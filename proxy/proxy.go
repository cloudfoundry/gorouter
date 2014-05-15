package proxy

import (
	"net/http"
	"net/http/httputil"
	"strings"
	"time"

	steno "github.com/cloudfoundry/gosteno"

	"github.com/cloudfoundry/gorouter/access_log"
	router_http "github.com/cloudfoundry/gorouter/common/http"
	"github.com/cloudfoundry/gorouter/route"
)

const (
	VcapCookieId    = "__VCAP_ID__"
	StickyCookieKey = "JSESSIONID"
)

type LookupRegistry interface {
	Lookup(uri route.Uri) (*route.Endpoint, bool)
	LookupByPrivateInstanceId(uri route.Uri, p string) (*route.Endpoint, bool)
}

type ProxyReporter interface {
	CaptureBadRequest(req *http.Request)
	CaptureBadGateway(req *http.Request)
	CaptureRoutingRequest(b *route.Endpoint, req *http.Request)
	CaptureRoutingResponse(b *route.Endpoint, res *http.Response, t time.Time, d time.Duration)
}

type Proxy interface {
	ServeHTTP(responseWriter http.ResponseWriter, request *http.Request)
}

type ProxyArgs struct {
	EndpointTimeout time.Duration
	Ip              string
	TraceKey        string
	Registry        LookupRegistry
	Reporter        ProxyReporter
	AccessLogger    access_log.AccessLogger
}

type proxy struct {
	ip           string
	traceKey     string
	logger       *steno.Logger
	registry     LookupRegistry
	reporter     ProxyReporter
	accessLogger access_log.AccessLogger
	transport    *http.Transport
}

func NewProxy(args ProxyArgs) Proxy {
	return &proxy{
		accessLogger: args.AccessLogger,
		traceKey:     args.TraceKey,
		ip:           args.Ip,
		logger:       steno.NewLogger("router.proxy"),
		registry:     args.Registry,
		reporter:     args.Reporter,
		transport: &http.Transport{
			DisableKeepAlives:     true,
			ResponseHeaderTimeout: args.EndpointTimeout,
		},
	}
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

func (p *proxy) lookup(request *http.Request) (*route.Endpoint, bool) {
	uri := route.Uri(hostWithoutPort(request))

	// Try choosing a backend using sticky session
	if _, err := request.Cookie(StickyCookieKey); err == nil {
		if sticky, err := request.Cookie(VcapCookieId); err == nil {
			routeEndpoint, ok := p.registry.LookupByPrivateInstanceId(uri, sticky.Value)
			if ok {
				return routeEndpoint, ok
			}
		}
	}

	// Choose backend using host alone
	return p.registry.Lookup(uri)
}

func (p *proxy) ServeHTTP(responseWriter http.ResponseWriter, request *http.Request) {
	startedAt := time.Now()
	handler := NewRequestHandler(request, responseWriter)

	accessLog := access_log.AccessLogRecord{
		Request:   request,
		StartedAt: startedAt,
	}

	defer func() {
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

	routeEndpoint, found := p.lookup(request)
	if !found {
		p.reporter.CaptureBadRequest(request)
		handler.HandleMissingRoute()
		return
	}

	handler.logger.Set("RouteEndpoint", routeEndpoint.ToLogData())

	accessLog.RouteEndpoint = routeEndpoint

	p.reporter.CaptureRoutingRequest(routeEndpoint, handler.request)

	if isTcpUpgrade(request) {
		handler.HandleTcpRequest(routeEndpoint)
		return
	}

	if isWebSocketUpgrade(request) {
		handler.HandleWebSocketRequest(routeEndpoint)
		return
	}

	proxyTransport := &proxyRoundTripper{
		transport: p.transport,
		after: func(rsp *http.Response, err error) {
			accessLog.FirstByteAt = time.Now()
			accessLog.Response = rsp

			if p.traceKey != "" && request.Header.Get(router_http.VcapTraceHeader) == p.traceKey {
				setTraceHeaders(responseWriter, p.ip, routeEndpoint.CanonicalAddr())
			}

			latency := time.Since(startedAt)

			p.reporter.CaptureRoutingResponse(routeEndpoint, rsp, startedAt, latency)

			if err != nil {
				p.reporter.CaptureBadGateway(request)
				handler.HandleBadGateway(err)
				return
			}

			if routeEndpoint.PrivateInstanceId != "" {
				setupStickySession(responseWriter, rsp, routeEndpoint)
			}
		},
	}

	proxyWriter := newProxyResponseWriter(responseWriter)
	p.newReverseProxy(proxyTransport, routeEndpoint, request).ServeHTTP(proxyWriter, request)

	accessLog.FinishedAt = time.Now()
	accessLog.BodyBytesSent = int64(proxyWriter.Size())
}

func (p *proxy) newReverseProxy(proxyTransport http.RoundTripper, endpoint *route.Endpoint, req *http.Request) http.Handler {
	rproxy := &httputil.ReverseProxy{
		Director: func(request *http.Request) {
			request.URL.Scheme = "http"
			request.URL.Host = endpoint.CanonicalAddr()
			request.URL.Opaque = req.URL.Opaque
			request.URL.RawQuery = req.URL.RawQuery

			setRequestXRequestStart(req)
			setRequestXVcapRequestId(req, nil)
		},
	}

	rproxy.Transport = proxyTransport
	rproxy.FlushInterval = 50 * time.Millisecond

	return rproxy
}

func setupStickySession(responseWriter http.ResponseWriter, response *http.Response, endpoint *route.Endpoint) {
	for _, v := range response.Cookies() {
		if v.Name == StickyCookieKey {
			cookie := &http.Cookie{
				Name:  VcapCookieId,
				Value: endpoint.PrivateInstanceId,
				Path:  "/",
			}

			http.SetCookie(responseWriter, cookie)
			return
		}
	}
}

type proxyRoundTripper struct {
	transport http.RoundTripper
	after     func(response *http.Response, err error)
	response  *http.Response
	err       error
}

func (p *proxyRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	p.response, p.err = p.transport.RoundTrip(request)
	if p.after != nil {
		p.after(p.response, p.err)
	}

	return p.response, p.err
}

type proxyResponseWriter struct {
	w      http.ResponseWriter
	status int
	size   int

	flusher http.Flusher
}

func newProxyResponseWriter(w http.ResponseWriter) *proxyResponseWriter {
	proxyWriter := &proxyResponseWriter{
		w:       w,
		flusher: w.(http.Flusher),
	}

	return proxyWriter
}

func (p *proxyResponseWriter) Header() http.Header {
	return p.w.Header()
}

func (p *proxyResponseWriter) Write(b []byte) (int, error) {
	if p.status == 0 {
		p.WriteHeader(http.StatusOK)
	}
	size, err := p.w.Write(b)
	p.size += size
	return size, err
}

func (p *proxyResponseWriter) WriteHeader(s int) {
	p.w.WriteHeader(s)

	if p.status == 0 {
		p.status = s
	}
}
func (p *proxyResponseWriter) Flush() {
	if p.flusher != nil {
		p.flusher.Flush()
	}
}

func (p *proxyResponseWriter) Status() int {
	return p.status
}

func (p *proxyResponseWriter) Size() int {
	return p.size
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
	// upgrade should be case insensitive per RFC6455 4.2.1
	if strings.ToLower(request.Header.Get("Connection")) == "upgrade" {
		return request.Header.Get("Upgrade")
	} else {
		return ""
	}
}

func setTraceHeaders(responseWriter http.ResponseWriter, routerIp, addr string) {
	responseWriter.Header().Set(router_http.VcapRouterHeader, routerIp)
	responseWriter.Header().Set(router_http.VcapBackendHeader, addr)
	responseWriter.Header().Set(router_http.CfRouteEndpointHeader, addr)
}
