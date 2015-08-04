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
	RouteServiceEnabled bool
	RouteServiceTimeout time.Duration
	Crypto              secure.Crypto
	CryptoPrev          secure.Crypto
}

type RouteServiceConfig struct {
	routeServiceEnabled bool
	routeServiceTimeout time.Duration
	crypto              secure.Crypto
	cryptoPrev          secure.Crypto
	logger              *steno.Logger
}

type RouteServiceArgs struct {
	UrlString string
	ParsedUrl *url.URL
	Signature string
	Metadata  string
}

func NewRouteServiceConfig(enabled bool, timeout time.Duration, crypto secure.Crypto, cryptoPrev secure.Crypto) *RouteServiceConfig {
	return &RouteServiceConfig{
		routeServiceEnabled: enabled,
		routeServiceTimeout: timeout,
		crypto:              crypto,
		cryptoPrev:          cryptoPrev,
		logger:              steno.NewLogger("router.proxy.route-service"),
	}
}

func (rs *RouteServiceConfig) RouteServiceEnabled() bool {
	return rs.routeServiceEnabled
}

func (rs *RouteServiceConfig) GenerateSignatureAndMetadata() (string, string, error) {
	signatureHeader, metadataHeader, err := route_service.BuildSignatureAndMetadata(rs.crypto)
	if err != nil {
		return "", "", err
	}
	return signatureHeader, metadataHeader, nil
}

func (rs *RouteServiceConfig) SetupRouteServiceRequest(request *http.Request, args RouteServiceArgs) {
	rs.logger.Debug("proxy.route-service")
	request.Header.Set(RouteServiceSignature, args.Signature)
	request.Header.Set(RouteServiceMetadata, args.Metadata)

	clientRequestUrl := request.URL.Scheme + "://" + request.URL.Host + request.URL.Opaque

	request.Header.Set(RouteServiceForwardedUrl, clientRequestUrl)

	request.Host = args.ParsedUrl.Host
	request.URL = args.ParsedUrl
}

func (rs *RouteServiceConfig) ValidateSignature(headers *http.Header) error {
	metadataHeader := headers.Get(RouteServiceMetadata)
	signatureHeader := headers.Get(RouteServiceSignature)

	signature, err := route_service.SignatureFromHeaders(signatureHeader, metadataHeader, rs.crypto)
	if err != nil {
		rs.logger.Warnd(map[string]interface{}{"error": err.Error()}, "proxy.route-service.current_key")
		// Decrypt the head again trying to use the old key.
		if rs.cryptoPrev != nil {
			signature, err = route_service.SignatureFromHeaders(signatureHeader, metadataHeader, rs.cryptoPrev)

			if err != nil {
				rs.logger.Warnd(map[string]interface{}{"error": err.Error()}, "proxy.route-service.previous_key")
			}
		}
	}

	if err != nil {
		return err
	}

	if time.Since(signature.RequestedTime) > rs.routeServiceTimeout {
		rs.logger.Debug("proxy.route-service.timeout")
		return routeServiceExpired
	}

	return nil
}

type proxy struct {
	ip                 string
	traceKey           string
	logger             *steno.Logger
	registry           LookupRegistry
	reporter           ProxyReporter
	accessLogger       access_log.AccessLogger
	transport          *http.Transport
	secureCookies      bool
	routeServiceConfig *RouteServiceConfig
}

func NewProxy(args ProxyArgs) Proxy {
	routeServiceConfig := NewRouteServiceConfig(args.RouteServiceEnabled, args.RouteServiceTimeout, args.Crypto, args.CryptoPrev)

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
		return
	}

	if isWebSocketUpgrade(request) {
		handler.HandleWebSocketRequest(iter)
		return
	}

	routeServiceUrl := routePool.RouteServiceUrl()
	// Attempted to use a route service when it is not supported
	if routeServiceUrl != "" && !p.routeServiceConfig.RouteServiceEnabled() {
		handler.HandleUnsupportedRouteService()
		return
	}

	var routeServiceArgs RouteServiceArgs
	if hasBeenToRouteService(routeServiceUrl, request.Header.Get(RouteServiceSignature)) {
		// A request from a route service destined for a backend instances
		routeServiceArgs.UrlString = routeServiceUrl
		err := p.routeServiceConfig.ValidateSignature(&request.Header)
		if err != nil {
			handler.HandleBadSignature(err)
			return
		}
	} else if routeServiceUrl != "" {
		// Generate signature, metadata, and parse Url for any errors
		var err error
		routeServiceArgs, err = validateRouteServiceRequest(p.routeServiceConfig, routeServiceUrl)
		if err != nil {
			handler.HandleRouteServiceFailure(err)
			return
		}
	}

	roundTripper := &ProxyRoundTripper{
		Transport:      dropsonde.InstrumentedRoundTripper(p.transport),
		Iter:           iter,
		Handler:        &handler,
		ServingBackend: request.Header.Get(RouteServiceSignature) != "" || routeServiceUrl == "",

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
				return
			}

			if endpoint.PrivateInstanceId != "" {
				setupStickySession(responseWriter, rsp, endpoint, stickyEndpointId, p.secureCookies, routePool.ContextPath())
			}
		},
	}

	newReverseProxy(roundTripper, request, routeServiceArgs, p.routeServiceConfig).ServeHTTP(proxyWriter, request)

	accessLog.FinishedAt = time.Now()
	accessLog.BodyBytesSent = proxyWriter.Size()
}

func newReverseProxy(proxyTransport http.RoundTripper, req *http.Request, routeServiceArgs RouteServiceArgs, routeServiceConfig *RouteServiceConfig) http.Handler {
	rproxy := &httputil.ReverseProxy{
		Director: func(request *http.Request) {
			request.URL.Scheme = "http"
			request.URL.Host = req.Host
			request.URL.Opaque = req.RequestURI
			request.URL.RawQuery = ""

			setRequestXRequestStart(req)
			setRequestXVcapRequestId(req, nil)

			sig := request.Header.Get(RouteServiceSignature)
			if forwardingToRouteService(routeServiceArgs.UrlString, sig) {
				// An endpoint has a route service and this request did not come from the service
				routeServiceConfig.SetupRouteServiceRequest(request, routeServiceArgs)
			} else if hasBeenToRouteService(routeServiceArgs.UrlString, sig) {
				// Remove the header since the backend should not see it
				request.Header.Del(RouteServiceSignature)
			}
		},
		Transport:     proxyTransport,
		FlushInterval: 50 * time.Millisecond,
	}

	return rproxy
}

type ProxyRoundTripper struct {
	Transport      http.RoundTripper
	after          AfterRoundTrip
	Iter           route.EndpointIterator
	Handler        *RequestHandler
	ServingBackend bool
}

func (p *ProxyRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	var err error
	var res *http.Response
	var endpoint *route.Endpoint
	retry := 0
	for {
		endpoint = p.Iter.Next()

		if endpoint == nil {
			p.Handler.reporter.CaptureBadGateway(request)
			err = noEndpointsAvailable
			p.Handler.HandleBadGateway(err)
			return nil, err
		}

		if p.ServingBackend {
			p.processBackend(request, endpoint)
		}

		res, err = p.Transport.RoundTrip(request)
		if err == nil {
			break
		}
		if ne, netErr := err.(*net.OpError); !netErr || ne.Op != "dial" {
			break
		}

		p.Iter.EndpointFailed()

		p.Handler.Logger().Set("Error", err.Error())
		p.Handler.Logger().Warnf("proxy.endpoint.failed")

		retry++
		if retry == retries {
			break
		}
	}

	if p.after != nil {
		p.after(res, endpoint, err)
	}

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

func validateRouteServiceRequest(routeServiceConfig *RouteServiceConfig, routeServiceUrl string) (RouteServiceArgs, error) {
	var routeServiceArgs RouteServiceArgs
	sig, metadata, err := routeServiceConfig.GenerateSignatureAndMetadata()
	if err != nil {
		return routeServiceArgs, err
	}

	routeServiceArgs.UrlString = routeServiceUrl
	routeServiceArgs.Signature = sig
	routeServiceArgs.Metadata = metadata

	rsURL, err := url.Parse(routeServiceUrl)
	if err != nil {
		return routeServiceArgs, err
	}
	routeServiceArgs.ParsedUrl = rsURL

	return routeServiceArgs, nil
}

func (p *ProxyRoundTripper) processBackend(request *http.Request, endpoint *route.Endpoint) {
	p.Handler.Logger().Debug("proxy.backend")

	request.URL.Host = endpoint.CanonicalAddr()
	request.Header.Set("X-CF-ApplicationID", endpoint.ApplicationId)
	setRequestXCfInstanceId(request, endpoint)
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
