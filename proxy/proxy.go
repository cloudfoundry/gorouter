package proxy

import (
	"crypto/tls"
	"io"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"sync/atomic"
	"time"

	"github.com/cloudfoundry/dropsonde"
	"github.com/cloudfoundry/gorouter/access_log"
	"github.com/cloudfoundry/gorouter/access_log/schema"
	router_http "github.com/cloudfoundry/gorouter/common/http"
	"github.com/cloudfoundry/gorouter/common/secure"
	"github.com/cloudfoundry/gorouter/metrics/reporter"
	"github.com/cloudfoundry/gorouter/proxy/handler"
	"github.com/cloudfoundry/gorouter/proxy/round_tripper"
	"github.com/cloudfoundry/gorouter/proxy/utils"
	"github.com/cloudfoundry/gorouter/route"
	"github.com/cloudfoundry/gorouter/route_service"
	"github.com/pivotal-golang/lager"
)

const (
	VcapCookieId    = "__VCAP_ID__"
	StickyCookieKey = "JSESSIONID"
)

type LookupRegistry interface {
	Lookup(uri route.Uri) *route.Pool
}

type Proxy interface {
	ServeHTTP(responseWriter http.ResponseWriter, request *http.Request)
	// Drain signals Proxy that the gorouter is about to shutdown
	Drain()
}

type ProxyArgs struct {
	EndpointTimeout            time.Duration
	Ip                         string
	TraceKey                   string
	Registry                   LookupRegistry
	Reporter                   reporter.ProxyReporter
	AccessLogger               access_log.AccessLogger
	SecureCookies              bool
	TLSConfig                  *tls.Config
	RouteServiceEnabled        bool
	RouteServiceTimeout        time.Duration
	RouteServiceRecommendHttps bool
	Crypto                     secure.Crypto
	CryptoPrev                 secure.Crypto
	ExtraHeadersToLog          []string
	Logger                     lager.Logger
}

type proxy struct {
	ip                         string
	traceKey                   string
	logger                     lager.Logger
	registry                   LookupRegistry
	reporter                   reporter.ProxyReporter
	accessLogger               access_log.AccessLogger
	transport                  *http.Transport
	secureCookies              bool
	heartbeatOK                int32
	routeServiceConfig         *route_service.RouteServiceConfig
	extraHeadersToLog          []string
	routeServiceRecommendHttps bool
}

func NewProxy(args ProxyArgs) Proxy {
	routeServiceConfig := route_service.NewRouteServiceConfig(args.Logger, args.RouteServiceEnabled, args.RouteServiceTimeout, args.Crypto, args.CryptoPrev, args.RouteServiceRecommendHttps)

	p := &proxy{
		accessLogger: args.AccessLogger,
		traceKey:     args.TraceKey,
		ip:           args.Ip,
		logger:       args.Logger,
		registry:     args.Registry,
		reporter:     args.Reporter,
		transport: &http.Transport{
			Dial: func(network, addr string) (net.Conn, error) {
				conn, err := net.DialTimeout(network, addr, 5*time.Second)
				if err != nil {
					return conn, err
				}
				if args.EndpointTimeout > 0 {
					err = conn.SetDeadline(time.Now().Add(args.EndpointTimeout))
				}
				return conn, err
			},
			DisableKeepAlives:  true,
			DisableCompression: true,
			TLSClientConfig:    args.TLSConfig,
		},
		secureCookies:              args.SecureCookies,
		heartbeatOK:                1, // 1->true, 0->false
		routeServiceConfig:         routeServiceConfig,
		extraHeadersToLog:          args.ExtraHeadersToLog,
		routeServiceRecommendHttps: args.RouteServiceRecommendHttps,
	}

	return p
}

func hostWithoutPort(req *http.Request) string {
	host := req.Host

	// Remove :<port>
	pos := strings.Index(host, ":")
	if pos >= 0 {
		host = host[0:pos]
	}

	return host
}

func (p *proxy) getStickySession(request *http.Request) string {
	// Try choosing a backend using sticky session
	if _, err := request.Cookie(StickyCookieKey); err == nil {
		if sticky, err := request.Cookie(VcapCookieId); err == nil {
			return sticky.Value
		}
	}
	return ""
}

func (p *proxy) lookup(request *http.Request) *route.Pool {
	requestPath := request.URL.EscapedPath()

	uri := route.Uri(hostWithoutPort(request) + requestPath)
	return p.registry.Lookup(uri)
}

// Drain stops sending successful heartbeats back to the loadbalancer
func (p *proxy) Drain() {
	atomic.StoreInt32(&(p.heartbeatOK), 0)
}

func (p *proxy) ServeHTTP(responseWriter http.ResponseWriter, request *http.Request) {
	startedAt := time.Now()
	accessLog := schema.AccessLogRecord{
		Request:           request,
		StartedAt:         startedAt,
		ExtraHeadersToLog: p.extraHeadersToLog,
	}

	requestBodyCounter := &countingReadCloser{delegate: request.Body}
	request.Body = requestBodyCounter

	proxyWriter := utils.NewProxyResponseWriter(responseWriter)
	handler := handler.NewRequestHandler(request, proxyWriter, p.reporter, &accessLog, p.logger)

	defer func() {
		accessLog.RequestBytesReceived = requestBodyCounter.GetCount()
		p.accessLogger.Log(accessLog)
	}()

	if !isProtocolSupported(request) {
		handler.HandleUnsupportedProtocol()
		return
	}

	if isLoadBalancerHeartbeat(request) {
		handler.HandleHeartbeat(atomic.LoadInt32(&p.heartbeatOK) != 0)
		return
	}

	routePool := p.lookup(request)
	if routePool == nil {
		p.reporter.CaptureBadRequest(request)
		handler.HandleMissingRoute()
		return
	}

	stickyEndpointId := p.getStickySession(request)
	iter := &wrappedIterator{
		nested: routePool.Endpoints(stickyEndpointId),

		afterNext: func(endpoint *route.Endpoint) {
			if endpoint != nil {
				handler.AddLoggingData(lager.Data{"route-endpoint": endpoint.ToLogData()})
				accessLog.RouteEndpoint = endpoint
				p.reporter.CaptureRoutingRequest(endpoint, request)
			}
		},
	}

	if isTcpUpgrade(request) {
		handler.HandleTcpRequest(iter)
		accessLog.FinishedAt = time.Now()
		return
	}

	if isWebSocketUpgrade(request) {
		handler.HandleWebSocketRequest(iter)
		accessLog.FinishedAt = time.Now()
		return
	}

	backend := true

	routeServiceUrl := routePool.RouteServiceUrl()
	// Attempted to use a route service when it is not supported
	if routeServiceUrl != "" && !p.routeServiceConfig.RouteServiceEnabled() {
		handler.HandleUnsupportedRouteService()
		return
	}

	var routeServiceArgs route_service.RouteServiceArgs
	if routeServiceUrl != "" {
		rsSignature := request.Header.Get(route_service.RouteServiceSignature)

		var recommendedScheme string

		if p.routeServiceRecommendHttps {
			recommendedScheme = "https"
		} else {
			recommendedScheme = "http"
		}

		forwardedUrlRaw := recommendedScheme + "://" + request.Host + request.RequestURI
		if hasBeenToRouteService(routeServiceUrl, rsSignature) {
			// A request from a route service destined for a backend instances
			routeServiceArgs.UrlString = routeServiceUrl
			err := p.routeServiceConfig.ValidateSignature(&request.Header, forwardedUrlRaw)
			if err != nil {
				handler.HandleBadSignature(err)
				return
			}
		} else {
			var err error
			// should not hardcode http, will be addressed by #100982038
			routeServiceArgs, err = buildRouteServiceArgs(p.routeServiceConfig, routeServiceUrl, forwardedUrlRaw)
			backend = false
			if err != nil {
				handler.HandleRouteServiceFailure(err)
				return
			}
		}
	}

	after := func(rsp *http.Response, endpoint *route.Endpoint, err error) {
		accessLog.FirstByteAt = time.Now()
		if rsp != nil {
			accessLog.StatusCode = rsp.StatusCode
		}

		if p.traceKey != "" && request.Header.Get(router_http.VcapTraceHeader) == p.traceKey {
			setTraceHeaders(responseWriter, p.ip, endpoint.CanonicalAddr())
		}

		latency := time.Since(startedAt)

		p.reporter.CaptureRoutingResponse(endpoint, rsp, startedAt, latency)

		if err != nil {
			p.reporter.CaptureBadGateway(request)
			handler.HandleBadGateway(err, request)
			return
		}

		if endpoint.PrivateInstanceId != "" {
			setupStickySession(responseWriter, rsp, endpoint, stickyEndpointId, p.secureCookies, routePool.ContextPath())
		}

		// if Content-Type not in response, nil out to suppress Go's auto-detect
		if _, ok := rsp.Header["Content-Type"]; !ok {
			responseWriter.Header()["Content-Type"] = nil
		}

	}

	roundTripper := round_tripper.NewProxyRoundTripper(backend,
		dropsonde.InstrumentedRoundTripper(p.transport), iter, handler, after)

	newReverseProxy(roundTripper, request, routeServiceArgs, p.routeServiceConfig).ServeHTTP(proxyWriter, request)

	accessLog.FinishedAt = time.Now()
	accessLog.BodyBytesSent = proxyWriter.Size()
}

func newReverseProxy(proxyTransport http.RoundTripper, req *http.Request,
	routeServiceArgs route_service.RouteServiceArgs,
	routeServiceConfig *route_service.RouteServiceConfig) http.Handler {
	rproxy := &httputil.ReverseProxy{
		Director: func(request *http.Request) {
			SetupProxyRequest(req, request, routeServiceArgs, routeServiceConfig)
		},
		Transport:     proxyTransport,
		FlushInterval: 50 * time.Millisecond,
	}

	return rproxy
}

func SetupProxyRequest(source *http.Request, target *http.Request,
	routeServiceArgs route_service.RouteServiceArgs,
	routeServiceConfig *route_service.RouteServiceConfig) {
	if source.Header.Get("X-Forwarded-Proto") == "" {
		scheme := "http"
		if source.TLS != nil {
			scheme = "https"
		}
		target.Header.Set("X-Forwarded-Proto", scheme)
	}

	target.URL.Scheme = "http"
	target.URL.Host = source.Host
	target.URL.Opaque = source.RequestURI
	target.URL.RawQuery = ""

	handler.SetRequestXRequestStart(source)

	sig := target.Header.Get(route_service.RouteServiceSignature)
	if forwardingToRouteService(routeServiceArgs.UrlString, sig) {
		// An endpoint has a route service and this request did not come from the service
		routeServiceConfig.SetupRouteServiceRequest(target, routeServiceArgs)
	} else if hasBeenToRouteService(routeServiceArgs.UrlString, sig) {
		// Remove the headers since the backend should not see it
		target.Header.Del(route_service.RouteServiceSignature)
		target.Header.Del(route_service.RouteServiceMetadata)
		target.Header.Del(route_service.RouteServiceForwardedUrl)
	}
}

type wrappedIterator struct {
	nested    route.EndpointIterator
	afterNext func(*route.Endpoint)
}

func (i *wrappedIterator) Next() *route.Endpoint {
	e := i.nested.Next()
	if i.afterNext != nil {
		i.afterNext(e)
	}
	return e
}

func (i *wrappedIterator) EndpointFailed() {
	i.nested.EndpointFailed()
}

func buildRouteServiceArgs(routeServiceConfig *route_service.RouteServiceConfig, routeServiceUrl, forwardedUrlRaw string) (route_service.RouteServiceArgs, error) {
	var routeServiceArgs route_service.RouteServiceArgs
	sig, metadata, err := routeServiceConfig.GenerateSignatureAndMetadata(forwardedUrlRaw)
	if err != nil {
		return routeServiceArgs, err
	}

	routeServiceArgs.UrlString = routeServiceUrl
	routeServiceArgs.Signature = sig
	routeServiceArgs.Metadata = metadata
	routeServiceArgs.ForwardedUrlRaw = forwardedUrlRaw

	rsURL, err := url.Parse(routeServiceUrl)
	if err != nil {
		return routeServiceArgs, err
	}
	routeServiceArgs.ParsedUrl = rsURL

	return routeServiceArgs, nil
}

func setupStickySession(responseWriter http.ResponseWriter, response *http.Response,
	endpoint *route.Endpoint,
	originalEndpointId string,
	secureCookies bool,
	path string) {
	secure := false
	maxAge := 0

	// did the endpoint change?
	sticky := originalEndpointId != "" && originalEndpointId != endpoint.PrivateInstanceId

	for _, v := range response.Cookies() {
		if v.Name == StickyCookieKey {
			sticky = true
			if v.MaxAge < 0 {
				maxAge = v.MaxAge
			}
			secure = v.Secure
			break
		}
	}

	if sticky {
		// right now secure attribute would as equal to the JSESSION ID cookie (if present),
		// but override if set to true in config
		if secureCookies {
			secure = true
		}

		cookie := &http.Cookie{
			Name:     VcapCookieId,
			Value:    endpoint.PrivateInstanceId,
			Path:     path,
			MaxAge:   maxAge,
			HttpOnly: true,
			Secure:   secure,
		}

		http.SetCookie(responseWriter, cookie)
	}
}

func forwardingToRouteService(rsUrl, sigHeader string) bool {
	return sigHeader == "" && rsUrl != ""
}

func hasBeenToRouteService(rsUrl, sigHeader string) bool {
	return sigHeader != "" && rsUrl != ""
}

func isProtocolSupported(request *http.Request) bool {
	return request.ProtoMajor == 1 && (request.ProtoMinor == 0 || request.ProtoMinor == 1)
}

func isLoadBalancerHeartbeat(request *http.Request) bool {
	return request.UserAgent() == "HTTP-Monitor/1.1"
}

func isWebSocketUpgrade(request *http.Request) bool {
	// websocket should be case insensitive per RFC6455 4.2.1
	return strings.ToLower(upgradeHeader(request)) == "websocket"
}

func isTcpUpgrade(request *http.Request) bool {
	return upgradeHeader(request) == "tcp"
}

func upgradeHeader(request *http.Request) string {
	// handle multiple Connection field-values, either in a comma-separated string or multiple field-headers
	for _, v := range request.Header[http.CanonicalHeaderKey("Connection")] {
		// upgrade should be case insensitive per RFC6455 4.2.1
		if strings.Contains(strings.ToLower(v), "upgrade") {
			return request.Header.Get("Upgrade")
		}
	}

	return ""
}

func setTraceHeaders(responseWriter http.ResponseWriter, routerIp, addr string) {
	responseWriter.Header().Set(router_http.VcapRouterHeader, routerIp)
	responseWriter.Header().Set(router_http.VcapBackendHeader, addr)
	responseWriter.Header().Set(router_http.CfRouteEndpointHeader, addr)
}

type countingReadCloser struct {
	delegate io.ReadCloser
	count    uint32
}

func (crc *countingReadCloser) Read(b []byte) (int, error) {
	n, err := crc.delegate.Read(b)
	atomic.AddUint32(&crc.count, uint32(n))
	return n, err
}

func (crc *countingReadCloser) GetCount() int {
	return int(atomic.LoadUint32(&crc.count))
}

func (crc *countingReadCloser) Close() error {
	return crc.delegate.Close()
}
