package proxy

import (
	"crypto/tls"
	"errors"
	"net"
	"net/http"
	"net/http/httputil"
	"strings"
	"time"

	"code.cloudfoundry.org/gorouter/accesslog"
	router_http "code.cloudfoundry.org/gorouter/common/http"
	"code.cloudfoundry.org/gorouter/config"
	"code.cloudfoundry.org/gorouter/handlers"
	"code.cloudfoundry.org/gorouter/logger"
	"code.cloudfoundry.org/gorouter/metrics"
	"code.cloudfoundry.org/gorouter/proxy/fails"
	"code.cloudfoundry.org/gorouter/proxy/handler"
	"code.cloudfoundry.org/gorouter/proxy/round_tripper"
	"code.cloudfoundry.org/gorouter/proxy/utils"
	"code.cloudfoundry.org/gorouter/registry"
	"code.cloudfoundry.org/gorouter/route"
	"code.cloudfoundry.org/gorouter/routeservice"
	"github.com/cloudfoundry/dropsonde"
	"github.com/uber-go/zap"
	"github.com/urfave/negroni"
)

const (
	VcapCookieId    = "__VCAP_ID__"
	StickyCookieKey = "JSESSIONID"
)

type proxy struct {
	ip                       string
	traceKey                 string
	logger                   logger.Logger
	reporter                 metrics.ProxyReporter
	accessLogger             accesslog.AccessLogger
	secureCookies            bool
	heartbeatOK              *int32
	routeServiceConfig       *routeservice.RouteServiceConfig
	healthCheckUserAgent     string
	forceForwardedProtoHttps bool
	sanitizeForwardedProto   bool
	defaultLoadBalance       string
	endpointDialTimeout      time.Duration
	endpointTimeout          time.Duration
	bufferPool               httputil.BufferPool
	backendTLSConfig         *tls.Config
	skipSanitization         func(req *http.Request) bool
	disableXFFLogging        bool
	disableSourceIPLogging   bool
}

func NewProxy(
	logger logger.Logger,
	accessLogger accesslog.AccessLogger,
	cfg *config.Config,
	registry registry.Registry,
	reporter metrics.ProxyReporter,
	routeServiceConfig *routeservice.RouteServiceConfig,
	tlsConfig *tls.Config,
	heartbeatOK *int32,
	routeServicesTransport http.RoundTripper,
	skipSanitization func(req *http.Request) bool,
) http.Handler {

	p := &proxy{
		accessLogger:             accessLogger,
		traceKey:                 cfg.TraceKey,
		ip:                       cfg.Ip,
		logger:                   logger,
		reporter:                 reporter,
		secureCookies:            cfg.SecureCookies,
		heartbeatOK:              heartbeatOK, // 1->true, 0->false
		routeServiceConfig:       routeServiceConfig,
		healthCheckUserAgent:     cfg.HealthCheckUserAgent,
		forceForwardedProtoHttps: cfg.ForceForwardedProtoHttps,
		sanitizeForwardedProto:   cfg.SanitizeForwardedProto,
		defaultLoadBalance:       cfg.LoadBalance,
		endpointDialTimeout:      cfg.EndpointDialTimeout,
		endpointTimeout:          cfg.EndpointTimeout,
		bufferPool:               NewBufferPool(),
		backendTLSConfig:         tlsConfig,
		skipSanitization:         skipSanitization,
		disableXFFLogging:        cfg.Logging.DisableLogForwardedFor,
		disableSourceIPLogging:   cfg.Logging.DisableLogSourceIP,
	}

	roundTripperFactory := &round_tripper.FactoryImpl{
		Template: &http.Transport{
			Dial:                (&net.Dialer{Timeout: cfg.EndpointDialTimeout}).Dial,
			DisableKeepAlives:   cfg.DisableKeepAlives,
			MaxIdleConns:        cfg.MaxIdleConns,
			IdleConnTimeout:     90 * time.Second, // setting the value to golang default transport
			MaxIdleConnsPerHost: cfg.MaxIdleConnsPerHost,
			DisableCompression:  true,
			TLSClientConfig:     tlsConfig,
		},
	}

	prt := round_tripper.NewProxyRoundTripper(
		roundTripperFactory, fails.ErrorTypes, p.logger,
		p.defaultLoadBalance, p.reporter, p.secureCookies,
		&round_tripper.ErrorHandler{
			MetricReporter: p.reporter,
			ErrorSpecs:     round_tripper.DefaultErrorSpecs,
		},
		routeServicesTransport,
		p.endpointTimeout,
	)

	rproxy := &httputil.ReverseProxy{
		Director:       p.setupProxyRequest,
		Transport:      prt,
		FlushInterval:  50 * time.Millisecond,
		BufferPool:     p.bufferPool,
		ModifyResponse: p.modifyResponse,
	}

	routeServiceHandler := handlers.NewRouteService(routeServiceConfig, registry, logger)
	zipkinHandler := handlers.NewZipkin(cfg.Tracing.EnableZipkin, cfg.ExtraHeadersToLog, logger)
	n := negroni.New()
	n.Use(handlers.NewRequestInfo())
	n.Use(handlers.NewProxyWriter(logger))
	n.Use(handlers.NewVcapRequestIdHeader(logger))
	n.Use(handlers.NewHTTPStartStop(dropsonde.DefaultEmitter, logger))
	n.Use(handlers.NewAccessLog(accessLogger, zipkinHandler.HeadersToLog(), logger))
	n.Use(handlers.NewReporter(reporter, logger))
	n.Use(handlers.NewProxyHealthcheck(cfg.HealthCheckUserAgent, p.heartbeatOK, logger))
	n.Use(zipkinHandler)
	n.Use(handlers.NewProtocolCheck(logger))
	n.Use(handlers.NewLookup(registry, reporter, logger))
	n.Use(handlers.NewClientCert(
		SkipSanitize(p.skipSanitization, routeServiceHandler.(*handlers.RouteService)),
		ForceDeleteXFCCHeader(routeServiceHandler.(*handlers.RouteService), cfg.ForwardedClientCert),
		cfg.ForwardedClientCert,
		logger,
	))
	n.Use(&handlers.XForwardedProto{
		SkipSanitization:         SkipSanitizeXFP(p.skipSanitization, routeServiceHandler.(*handlers.RouteService)),
		ForceForwardedProtoHttps: p.forceForwardedProtoHttps,
		SanitizeForwardedProto:   p.sanitizeForwardedProto,
		Logger:                   logger,
	})
	n.Use(routeServiceHandler)
	n.Use(p)
	n.UseHandler(rproxy)

	return n
}

type RouteServiceValidator interface {
	ArrivedViaRouteService(req *http.Request) (bool, error)
}

func SkipSanitizeXFP(arrivedViaRouteServicesServer func(*http.Request) bool, routeServiceValidator RouteServiceValidator) func(*http.Request) (bool, error) {
	return func(req *http.Request) (bool, error) {
		valid, err := routeServiceValidator.ArrivedViaRouteService(req)
		if err != nil {
			return false, err
		}
		return valid || arrivedViaRouteServicesServer(req), nil
	}
}

func SkipSanitize(arrivedViaRouteServicesServer func(*http.Request) bool, routeServiceValidator RouteServiceValidator) func(*http.Request) (bool, error) {
	return func(req *http.Request) (bool, error) {
		valid, err := routeServiceValidator.ArrivedViaRouteService(req)
		if err != nil {
			return false, err
		}
		return arrivedViaRouteServicesServer(req) || (valid && req.TLS != nil), nil
	}
}

func ForceDeleteXFCCHeader(routeServiceValidator RouteServiceValidator, forwardedClientCert string) func(*http.Request) (bool, error) {
	return func(req *http.Request) (bool, error) {
		valid, err := routeServiceValidator.ArrivedViaRouteService(req)
		if err != nil {
			return false, err
		}
		return valid && forwardedClientCert != config.SANITIZE_SET, nil
	}
}

func (p *proxy) ServeHTTP(responseWriter http.ResponseWriter, request *http.Request, next http.HandlerFunc) {
	proxyWriter := responseWriter.(utils.ProxyResponseWriter)

	reqInfo, err := handlers.ContextRequestInfo(request)
	if err != nil {
		p.logger.Fatal("request-info-err", zap.Error(err))
	}
	handler := handler.NewRequestHandler(
		request,
		proxyWriter,
		p.reporter,
		p.logger,
		p.endpointDialTimeout,
		p.backendTLSConfig,
		handler.DisableXFFLogging(p.disableXFFLogging),
		handler.DisableSourceIPLogging(p.disableSourceIPLogging),
	)

	if reqInfo.RoutePool == nil {
		p.logger.Fatal("request-info-err", zap.Error(errors.New("failed-to-access-RoutePool")))
	}

	stickyEndpointId := getStickySession(request)
	iter := &wrappedIterator{
		nested: reqInfo.RoutePool.Endpoints(p.defaultLoadBalance, stickyEndpointId),

		afterNext: func(endpoint *route.Endpoint) {
			if endpoint != nil {
				reqInfo.RouteEndpoint = endpoint
				p.reporter.CaptureRoutingRequest(endpoint)
			}
		},
	}

	if handlers.IsTcpUpgrade(request) {
		handler.HandleTcpRequest(iter)
		return
	}

	if handlers.IsWebSocketUpgrade(request) {
		handler.HandleWebSocketRequest(iter)
		return
	}

	next(responseWriter, request)
}

func (p *proxy) setupProxyRequest(target *http.Request) {
	reqInfo, err := handlers.ContextRequestInfo(target)
	if err != nil {
		p.logger.Fatal("request-info-err", zap.Error(err))
		return
	}
	reqInfo.BackendReqHeaders = target.Header

	target.URL.Scheme = "http"
	target.URL.Host = target.Host
	target.URL.ForceQuery = false
	target.URL.Opaque = target.RequestURI

	if strings.HasPrefix(target.RequestURI, "//") {
		target.URL.Opaque = "//" + target.Host + target.URL.Path + target.URL.Query().Encode()
	}
	target.URL.RawQuery = ""

	handler.SetRequestXRequestStart(target)
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

func (i *wrappedIterator) EndpointFailed(err error) {
	i.nested.EndpointFailed(err)
}
func (i *wrappedIterator) PreRequest(e *route.Endpoint) {
	i.nested.PreRequest(e)
}
func (i *wrappedIterator) PostRequest(e *route.Endpoint) {
	i.nested.PostRequest(e)
}

func getStickySession(request *http.Request) string {
	// Try choosing a backend using sticky session
	if _, err := request.Cookie(StickyCookieKey); err == nil {
		if sticky, err := request.Cookie(VcapCookieId); err == nil {
			return sticky.Value
		}
	}
	return ""
}
