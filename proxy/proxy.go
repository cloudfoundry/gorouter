package proxy

import (
	"net/http"
	"net/url"
	"strings"
	"time"

	steno "github.com/cloudfoundry/gosteno"

	"github.com/cloudfoundry/gorouter/access_log"
	"github.com/cloudfoundry/gorouter/route"
)

const (
	VcapTraceHeader = "X-Vcap-Trace"
	VcapCookieId    = "__VCAP_ID__"
	StickyCookieKey = "JSESSIONID"
)

type LookupRegistry interface {
	Lookup(uri route.Uri) (*route.Endpoint, bool)
	LookupByPrivateInstanceId(uri route.Uri, p string) (*route.Endpoint, bool)
}

type Reporter interface {
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
	Reporter        Reporter
	Logger          access_log.AccessLogger
}

type proxy struct {
	ip           string
	traceKey     string
	logger       *steno.Logger
	registry     LookupRegistry
	reporter     Reporter
	accessLogger access_log.AccessLogger
	transport    *http.Transport
}

func NewProxy(args ProxyArgs) Proxy {
	return &proxy{
		accessLogger: args.Logger,
		traceKey:     args.TraceKey,
		ip:           args.Ip,
		logger:       steno.NewLogger("router.proxy"),
		registry:     args.Registry,
		reporter:     args.Reporter,
		transport:    &http.Transport{ResponseHeaderTimeout: args.EndpointTimeout},
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
	originalURL := request.URL
	request.URL = &url.URL{Host: originalURL.Host, Opaque: request.RequestURI}
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

	endpointResponse, err := handler.HandleHttpRequest(p.transport, routeEndpoint)

	latency := time.Since(startedAt)

	p.reporter.CaptureRoutingResponse(routeEndpoint, endpointResponse, startedAt, latency)

	if err != nil {
		p.reporter.CaptureBadGateway(request)
		handler.HandleBadGateway(err)
		return
	}

	accessLog.FirstByteAt = time.Now()
	accessLog.Response = endpointResponse

	if p.traceKey != "" && request.Header.Get(VcapTraceHeader) == p.traceKey {
		handler.SetTraceHeaders(p.ip, routeEndpoint.CanonicalAddr())
	}

	bytesSent := handler.WriteResponse(endpointResponse)

	accessLog.FinishedAt = time.Now()
	accessLog.BodyBytesSent = bytesSent
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
