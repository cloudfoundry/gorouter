package proxy

import (
	"crypto/tls"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"time"

	"github.com/cloudfoundry/dropsonde"
	"github.com/cloudfoundry/gorouter/access_log"
	router_http "github.com/cloudfoundry/gorouter/common/http"
	"github.com/cloudfoundry/gorouter/common/secure"
	"github.com/cloudfoundry/gorouter/route"
	"github.com/cloudfoundry/gorouter/route_service"
	steno "github.com/cloudfoundry/gosteno"
	"github.com/cloudfoundry/gorouter/metrics"
)

const (
	VcapCookieId    = "__VCAP_ID__"
	StickyCookieKey = "JSESSIONID"
	maxRetries      = 3
)

var noEndpointsAvailable = errors.New("No endpoints available")

type LookupRegistry interface {
	Lookup(uri route.Uri) *route.Pool
}

type AfterRoundTrip func(rsp *http.Response, endpoint *route.Endpoint, err error)

type Proxy interface {
	ServeHTTP(responseWriter http.ResponseWriter, request *http.Request)
}

type ProxyArgs struct {
	EndpointTimeout     time.Duration
	Ip                  string
	TraceKey            string
	Registry            LookupRegistry
	Reporter            metrics.ProxyReporter
	AccessLogger        access_log.AccessLogger
	SecureCookies       bool
	TLSConfig           *tls.Config
	RouteServiceEnabled bool
	RouteServiceTimeout time.Duration
	Crypto              secure.Crypto
	CryptoPrev          secure.Crypto
	ExtraHeadersToLog   []string
}

type proxy struct {
	ip                 string
	traceKey           string
	logger             *steno.Logger
	registry           LookupRegistry
	reporter           metrics.ProxyReporter
	accessLogger       access_log.AccessLogger
	transport          *http.Transport
	secureCookies      bool
	routeServiceConfig *route_service.RouteServiceConfig
	ExtraHeadersToLog  []string
}

func NewProxy(args ProxyArgs) Proxy {
	routeServiceConfig := route_service.NewRouteServiceConfig(args.RouteServiceEnabled, args.RouteServiceTimeout, args.Crypto, args.CryptoPrev)

	p := &proxy{
		accessLogger: args.AccessLogger,
		traceKey:     args.TraceKey,
		ip:           args.Ip,
		logger:       steno.NewLogger("router.proxy"),
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
		secureCookies:      args.SecureCookies,
		routeServiceConfig: routeServiceConfig,
		ExtraHeadersToLog:  args.ExtraHeadersToLog,
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
	uri := route.Uri(hostWithoutPort(request) + request.RequestURI)
	return p.registry.Lookup(uri)
}

func (p *proxy) ServeHTTP(responseWriter http.ResponseWriter, request *http.Request) {
	startedAt := time.Now()
	accessLog := access_log.AccessLogRecord{
		Request:           request,
		StartedAt:         startedAt,
		ExtraHeadersToLog: p.ExtraHeadersToLog,
	}

	requestBodyCounter := &countingReadCloser{delegate: request.Body}
	request.Body = requestBodyCounter

	proxyWriter := NewProxyResponseWriter(responseWriter)
	handler := NewRequestHandler(request, proxyWriter, p.reporter, &accessLog)

	defer func() {
		accessLog.RequestBytesReceived = requestBodyCounter.count
		p.accessLogger.Log(accessLog)
	}()

	if !isProtocolSupported(request) {
		handler.HandleUnsupportedProtocol()
		return
	}

	if isLoadBalancerHeartbeat(request) {
		handler.HandleHeartbeat()
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
				handler.Logger().Set("RouteEndpoint", endpoint.ToLogData())
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
		forwardedUrlRaw := "http" + "://" + request.Host + request.RequestURI
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
			handler.HandleBadGateway(err)
			return
		}

		if endpoint.PrivateInstanceId != "" {
			setupStickySession(responseWriter, rsp, endpoint, stickyEndpointId, p.secureCookies, routePool.ContextPath())
		}
	}

	roundTripper := NewProxyRoundTripper(backend,
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
	target.URL.Scheme = "http"
	target.URL.Host = source.Host
	target.URL.Opaque = source.RequestURI
	target.URL.RawQuery = ""

	setRequestXRequestStart(source)
	setRequestXVcapRequestId(source, nil)

	sig := target.Header.Get(route_service.RouteServiceSignature)
	if forwardingToRouteService(routeServiceArgs.UrlString, sig) {
		// An endpoint has a route service and this request did not come from the service
		routeServiceConfig.SetupRouteServiceRequest(target, routeServiceArgs)
	} else if hasBeenToRouteService(routeServiceArgs.UrlString, sig) {
		// Remove the headers since the backend should not see it
		target.Header.Del(route_service.RouteServiceSignature)
		target.Header.Del(route_service.RouteServiceMetadata)
	}
}

func newRouteServiceEndpoint() *route.Endpoint {
	return &route.Endpoint{
		Tags: map[string]string{},
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

	maxAge := 0

	// did the endpoint change?
	sticky := originalEndpointId != "" && originalEndpointId != endpoint.PrivateInstanceId

	for _, v := range response.Cookies() {
		if v.Name == StickyCookieKey {
			sticky = true
			if v.MaxAge < 0 {
				maxAge = v.MaxAge
			}
			break
		}
	}

	if sticky {
		cookie := &http.Cookie{
			Name:     VcapCookieId,
			Value:    endpoint.PrivateInstanceId,
			Path:     path,
			MaxAge:   maxAge,
			HttpOnly: true,
			Secure:   secureCookies,
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
	count    int
}

func (crc *countingReadCloser) Read(b []byte) (int, error) {
	n, err := crc.delegate.Read(b)
	crc.count += n
	return n, err
}

func (crc *countingReadCloser) Close() error {
	return crc.delegate.Close()
}
