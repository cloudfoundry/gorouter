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
)

const (
	VcapCookieId             = "__VCAP_ID__"
	StickyCookieKey          = "JSESSIONID"
	retries                  = 3
	RouteServiceSignature    = "X-CF-Proxy-Signature"
	RouteServiceForwardedUrl = "X-CF-Forwarded-Url"
	RouteServiceMetadata     = "X-CF-Proxy-Metadata"
)

var noEndpointsAvailable = errors.New("No endpoints available")
var invalidRouteServiceSignature = errors.New("Invalid route service header signature")
var routeServiceExpired = errors.New("Route service request expired")

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
	EndpointTimeout     time.Duration
	Ip                  string
	TraceKey            string
	Registry            LookupRegistry
	Reporter            ProxyReporter
	AccessLogger        access_log.AccessLogger
	SecureCookies       bool
	TLSConfig           *tls.Config
	RouteServiceTimeout time.Duration
	Crypto              secure.Crypto
}

type proxy struct {
	ip                  string
	traceKey            string
	logger              *steno.Logger
	registry            LookupRegistry
	reporter            ProxyReporter
	accessLogger        access_log.AccessLogger
	transport           *http.Transport
	secureCookies       bool
	routeServiceTimeout time.Duration
	crypto              secure.Crypto
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
			TLSClientConfig:    args.TLSConfig,
		},
		secureCookies:       args.SecureCookies,
		routeServiceTimeout: args.RouteServiceTimeout,
		crypto:              args.Crypto,
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
		transport:           dropsonde.InstrumentedRoundTripper(p.transport),
		iter:                iter,
		handler:             &handler,
		routeServiceTimeout: p.routeServiceTimeout,
		crypto:              p.crypto,

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

				setupStickySession(responseWriter, rsp, endpoint, stickyEndpointId, p.secureCookies, routePool.ContextPath())
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
	transport           http.RoundTripper
	after               AfterRoundTrip
	iter                route.EndpointIterator
	handler             *RequestHandler
	routeServiceTimeout time.Duration
	crypto              secure.Crypto

	response *http.Response
	err      error
}

func (p *proxyRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	var err error
	var sig string
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

		// determine where we need to route to (backend or route service)
		sig, err = p.processIncomingRequest(request, endpoint)
		if err != nil {
			return nil, err
		}

		res, err = p.transport.RoundTrip(request)
		if err == nil {
			break
		}

		if ne, netErr := err.(*net.OpError); !netErr || ne.Op != "dial" {
			break
		}

		// post processing of request in case of error / retry
		postProcess(request, sig)

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

func postProcess(request *http.Request, sig string) {
	// if we have a request which comes from a route service
	// we will want to restore the signed header so we do not
	// route to the route service on a retry
	if sig != "" {
		request.Header.Set(RouteServiceSignature, sig)
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

// Do not modify header object
func (p *proxyRoundTripper) validateSignature(header *http.Header) error {
	metadataHeader := header.Get(RouteServiceMetadata)
	signatureHeader := header.Get(RouteServiceSignature)

	signature, err := route_service.SignatureFromHeaders(signatureHeader, metadataHeader, p.crypto)
	if err != nil {
		return err
	}

	if time.Since(signature.RequestedTime) > p.routeServiceTimeout {
		return routeServiceExpired
	}

	return nil
}

func (p *proxyRoundTripper) processRoutingService(request *http.Request, endpoint *route.Endpoint) error {
	p.handler.Logger().Debug("proxy.route-service")

	signatureHeader, metadataHeader, err := route_service.BuildSignatureAndMetadata(p.crypto)
	if err != nil {
		return err
	}

	request.Header.Set(RouteServiceSignature, signatureHeader)
	request.Header.Set(RouteServiceMetadata, metadataHeader)

	clientRequestUrl := request.URL.Scheme + "://" + request.URL.Host + request.URL.Opaque

	request.Header.Set(RouteServiceForwardedUrl, clientRequestUrl)

	rsURL, err := url.Parse(endpoint.RouteServiceUrl)
	if err != nil {
		return err
	}
	request.Host = rsURL.Host
	request.URL = rsURL

	return nil
}

func (p *proxyRoundTripper) processBackend(request *http.Request, endpoint *route.Endpoint) {
	p.handler.Logger().Debug("proxy.backend")

	request.URL.Host = endpoint.CanonicalAddr()
	request.Header.Set("X-CF-ApplicationID", endpoint.ApplicationId)
	setRequestXCfInstanceId(request, endpoint)
}

func (p *proxyRoundTripper) processIncomingRequest(request *http.Request, endpoint *route.Endpoint) (string, error) {
	var err error
	var sig string

	if endpoint.RouteServiceUrl != "" {
		if request.Header.Get(RouteServiceSignature) == "" {
			return "", p.processRoutingService(request, endpoint)
		}
		err = p.validateSignature(&request.Header)
		if err != nil {
			p.handler.HandleBadSignature(err)
			return "", err
		}
		sig = request.Header.Get(RouteServiceSignature)
		request.Header.Del(RouteServiceSignature)
	}

	p.processBackend(request, endpoint)
	return sig, err
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
