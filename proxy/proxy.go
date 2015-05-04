package proxy

import (
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httputil"
	"strings"
	"time"

	"github.com/cloudfoundry/dropsonde"
	"github.com/cloudfoundry/gorouter/access_log"
	router_http "github.com/cloudfoundry/gorouter/common/http"
	"github.com/cloudfoundry/gorouter/route"
	steno "github.com/cloudfoundry/gosteno"
)

const (
	VcapCookieId    = "__VCAP_ID__"
	StickyCookieKey = "JSESSIONID"
	retries         = 3
)

var noEndpointsAvailable = errors.New("No endpoints available")

type LookupRegistry interface {
	Lookup(uri route.Uri) *route.Pool
}

type AfterRoundTrip func(rsp *http.Response, endpoint *route.Endpoint, err error)

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
	SecureCookies   bool
}

type proxy struct {
	ip            string
	traceKey      string
	logger        *steno.Logger
	registry      LookupRegistry
	reporter      ProxyReporter
	accessLogger  access_log.AccessLogger
	transport     *http.Transport
	secureCookies bool
}

func NewProxy(args ProxyArgs) Proxy {
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
		},
		secureCookies: args.SecureCookies,
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

func ChopUpPath(str string) []string {
	// Remove :<port>
	ln := len(str)
	if str[ln-1:ln] == "/" {
		str = str[0 : ln-1]
	}
	ret := strings.Split(str, "/")

	for i := 1; i < len(ret); i++ {
		ret[i] = ret[i-1] + "/" + ret[i]
	}
	return ret
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
	chopt := ChopUpPath(request.RequestURI)
	for i := len(chopt) - 1; i >= 0; i-- {
		uri := route.Uri(hostWithoutPort(request) + chopt[i])
		ret := p.registry.Lookup(uri)
		if ret != nil && !ret.IsEmpty() {
			return ret
		}
	}
	return nil
}

func (p *proxy) ServeHTTP(responseWriter http.ResponseWriter, request *http.Request) {
	startedAt := time.Now()

	accessLog := access_log.AccessLogRecord{
		Request:   request,
		StartedAt: startedAt,
	}

	requestBodyCounter := &countingReadCloser{delegate: request.Body}
	request.Body = requestBodyCounter

	handler := NewRequestHandler(request, responseWriter, p.reporter, &accessLog)

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
				handler.logger.Set("RouteEndpoint", endpoint.ToLogData())
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

	proxyWriter := newProxyResponseWriter(responseWriter)
	roundTripper := &proxyRoundTripper{
		transport: dropsonde.InstrumentedRoundTripper(p.transport),
		iter:      iter,
		handler:   &handler,

		after: func(rsp *http.Response, endpoint *route.Endpoint, err error) {
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
				proxyWriter.Done()
				return
			}

			if endpoint.PrivateInstanceId != "" {
				setupStickySession(responseWriter, rsp, endpoint, stickyEndpointId, p.secureCookies)
			}
		},
	}

	p.newReverseProxy(roundTripper, request).ServeHTTP(proxyWriter, request)

	accessLog.FinishedAt = time.Now()
	accessLog.BodyBytesSent = proxyWriter.Size()
}

func (p *proxy) newReverseProxy(proxyTransport http.RoundTripper, req *http.Request) http.Handler {
	rproxy := &httputil.ReverseProxy{
		Director: func(request *http.Request) {
			request.URL.Scheme = "http"
			request.URL.Host = req.Host
			request.URL.Opaque = req.RequestURI
			request.URL.RawQuery = ""

			setRequestXRequestStart(req)
			setRequestXVcapRequestId(req, nil)
		},
		Transport:     proxyTransport,
		FlushInterval: 50 * time.Millisecond,
	}

	return rproxy
}

type proxyRoundTripper struct {
	transport http.RoundTripper
	after     AfterRoundTrip
	iter      route.EndpointIterator
	handler   *RequestHandler

	response *http.Response
	err      error
}

func (p *proxyRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	var err error
	var res *http.Response
	var endpoint *route.Endpoint
	retry := 0
	for {
		endpoint = p.iter.Next()

		if endpoint == nil {
			p.handler.reporter.CaptureBadGateway(request)
			err = noEndpointsAvailable
			p.handler.HandleBadGateway(err)
			return nil, err
		}

		request.URL.Host = endpoint.CanonicalAddr()
		request.Header.Set("X-CF-ApplicationID", endpoint.ApplicationId)
		setRequestXCfInstanceId(request, endpoint)

		res, err = p.transport.RoundTrip(request)
		if err == nil {
			break
		}

		if ne, netErr := err.(*net.OpError); !netErr || ne.Op != "dial" {
			break
		}

		p.iter.EndpointFailed()

		p.handler.Logger().Set("Error", err.Error())
		p.handler.Logger().Warnf("proxy.endpoint.failed")

		retry++
		if retry == retries {
			break
		}
	}

	if p.after != nil {
		p.after(res, endpoint, err)
	}

	p.response = res
	p.err = err

	return res, err
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

func setupStickySession(responseWriter http.ResponseWriter, response *http.Response,
	endpoint *route.Endpoint,
	originalEndpointId string,
	secureCookies bool) {

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
			Path:     "/",
			MaxAge:   maxAge,
			HttpOnly: true,
			Secure:   secureCookies,
		}

		http.SetCookie(responseWriter, cookie)
	}
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
