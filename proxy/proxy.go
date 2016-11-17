package proxy

import (
	"crypto/tls"
	"errors"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"time"

	"code.cloudfoundry.org/gorouter/access_log"
	"code.cloudfoundry.org/gorouter/access_log/schema"
	router_http "code.cloudfoundry.org/gorouter/common/http"
	"code.cloudfoundry.org/gorouter/common/secure"
	"code.cloudfoundry.org/gorouter/handlers"
	"code.cloudfoundry.org/gorouter/metrics/reporter"
	"code.cloudfoundry.org/gorouter/proxy/handler"
	"code.cloudfoundry.org/gorouter/proxy/round_tripper"
	"code.cloudfoundry.org/gorouter/proxy/utils"
	"code.cloudfoundry.org/gorouter/route"
	"code.cloudfoundry.org/gorouter/route_service"
	"code.cloudfoundry.org/lager"
	"github.com/cloudfoundry/dropsonde"
	"github.com/urfave/negroni"
)

const (
	VcapCookieId    = "__VCAP_ID__"
	StickyCookieKey = "JSESSIONID"
)

type LookupRegistry interface {
	Lookup(uri route.Uri) *route.Pool
	LookupWithInstance(uri route.Uri, appId string, appIndex string) *route.Pool
}

type Proxy interface {
	ServeHTTP(responseWriter http.ResponseWriter, request *http.Request)
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
	ExtraHeadersToLog          *[]string
	Logger                     lager.Logger
	HealthCheckUserAgent       string
	HeartbeatOK                *int32
	EnableZipkin               bool
	ForceForwardedProtoHttps   bool
	DefaultLoadBalance         string
}

type proxyHandler struct {
	handlers *negroni.Negroni
	proxy    *proxy
}

func (p *proxyHandler) ServeHTTP(responseWriter http.ResponseWriter, request *http.Request) {
	p.handlers.ServeHTTP(responseWriter, request)
}

type proxyWriterHandler struct{}

// ServeHTTP wraps the responseWriter in a ProxyResponseWriter
func (p *proxyWriterHandler) ServeHTTP(responseWriter http.ResponseWriter, request *http.Request, next http.HandlerFunc) {
	proxyWriter := utils.NewProxyResponseWriter(responseWriter)
	next(proxyWriter, request)
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
	heartbeatOK                *int32
	routeServiceConfig         *route_service.RouteServiceConfig
	extraHeadersToLog          *[]string
	routeServiceRecommendHttps bool
	healthCheckUserAgent       string
	forceForwardedProtoHttps   bool
	defaultLoadBalance         string
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
		heartbeatOK:                args.HeartbeatOK, // 1->true, 0->false
		routeServiceConfig:         routeServiceConfig,
		extraHeadersToLog:          args.ExtraHeadersToLog,
		routeServiceRecommendHttps: args.RouteServiceRecommendHttps,
		healthCheckUserAgent:       args.HealthCheckUserAgent,
		forceForwardedProtoHttps:   args.ForceForwardedProtoHttps,
		defaultLoadBalance:         args.DefaultLoadBalance,
	}

	n := negroni.New()
	n.Use(&proxyWriterHandler{})
	n.Use(handlers.NewAccessLog(args.AccessLogger, args.ExtraHeadersToLog))
	n.Use(handlers.NewHealthcheck(args.HealthCheckUserAgent, p.heartbeatOK, args.Logger))
	n.Use(handlers.NewZipkin(args.EnableZipkin, args.ExtraHeadersToLog, args.Logger))

	n.UseHandler(p)
	handlers := &proxyHandler{
		handlers: n,
		proxy:    p,
	}

	return handlers
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
	appInstanceHeader := request.Header.Get(router_http.CfAppInstance)
	if appInstanceHeader != "" {
		appId, appIndex, err := router_http.ValidateCfAppInstance(appInstanceHeader)

		if err != nil {
			p.logger.Error("invalid-app-instance-header", err)
			return nil
		} else {
			return p.registry.LookupWithInstance(uri, appId, appIndex)
		}
	}

	return p.registry.Lookup(uri)
}

func (p *proxy) ServeHTTP(responseWriter http.ResponseWriter, request *http.Request) {
	proxyWriter := responseWriter.(utils.ProxyResponseWriter)

	alr := proxyWriter.Context().Value("AccessLogRecord")
	if alr == nil {
		p.logger.Error("AccessLogRecord not set on context", errors.New("failed-to-access-LogRecord"))
	}
	accessLog := alr.(*schema.AccessLogRecord)

	handler := handler.NewRequestHandler(request, proxyWriter, p.reporter, accessLog, p.logger)

	if !isProtocolSupported(request) {
		handler.HandleUnsupportedProtocol()
		return
	}

	routePool := p.lookup(request)
	if routePool == nil {
		handler.HandleMissingRoute()
		return
	}

	stickyEndpointId := p.getStickySession(request)
	iter := &wrappedIterator{
		nested: routePool.Endpoints(p.defaultLoadBalance, stickyEndpointId),

		afterNext: func(endpoint *route.Endpoint) {
			if endpoint != nil {
				accessLog.RouteEndpoint = endpoint
				p.reporter.CaptureRoutingRequest(endpoint, request)
			}
		},
	}

	if isTcpUpgrade(request) {
		handler.HandleTcpRequest(iter)
		return
	}

	if isWebSocketUpgrade(request) {
		handler.HandleWebSocketRequest(iter)
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

		forwardedUrlRaw := recommendedScheme + "://" + hostWithoutPort(request) + request.RequestURI
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
		if endpoint == nil {
			handler.HandleBadGateway(err, request)
			return
		}

		accessLog.FirstByteAt = time.Now()
		if rsp != nil {
			accessLog.StatusCode = rsp.StatusCode
		}

		if p.traceKey != "" && endpoint != nil && request.Header.Get(router_http.VcapTraceHeader) == p.traceKey {
			router_http.SetTraceHeaders(responseWriter, p.ip, endpoint.CanonicalAddr())
		}

		latency := time.Since(accessLog.StartedAt)

		p.reporter.CaptureRoutingResponse(endpoint, rsp, accessLog.StartedAt, latency)

		if err != nil {
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
		dropsonde.InstrumentedRoundTripper(p.transport), iter, handler.Logger(), after)

	newReverseProxy(roundTripper, request, routeServiceArgs, p.routeServiceConfig, p.forceForwardedProtoHttps).ServeHTTP(proxyWriter, request)
}

func newReverseProxy(proxyTransport http.RoundTripper, req *http.Request,
	routeServiceArgs route_service.RouteServiceArgs,
	routeServiceConfig *route_service.RouteServiceConfig,
	forceForwardedProtoHttps bool) http.Handler {
	rproxy := &httputil.ReverseProxy{
		Director: func(request *http.Request) {
			setupProxyRequest(req, request, forceForwardedProtoHttps)
			handleRouteServiceIntegration(request, routeServiceArgs, routeServiceConfig)
		},
		Transport:     proxyTransport,
		FlushInterval: 50 * time.Millisecond,
	}

	return rproxy
}

func handleRouteServiceIntegration(
	target *http.Request,
	routeServiceArgs route_service.RouteServiceArgs,
	routeServiceConfig *route_service.RouteServiceConfig,
) {
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

func setupProxyRequest(source *http.Request, target *http.Request, forceForwardedProtoHttps bool) {
	if forceForwardedProtoHttps {
		target.Header.Set("X-Forwarded-Proto", "https")
	} else if source.Header.Get("X-Forwarded-Proto") == "" {
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
	target.Header.Del(router_http.CfAppInstance)
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
func (i *wrappedIterator) PreRequest(e *route.Endpoint) {
	i.nested.PreRequest(e)
}
func (i *wrappedIterator) PostRequest(e *route.Endpoint) {
	i.nested.PostRequest(e)
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
