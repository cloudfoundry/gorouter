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

	"code.cloudfoundry.org/gorouter/common/health"

	"github.com/cloudfoundry/dropsonde"
	"github.com/uber-go/zap"
	"github.com/urfave/negroni"

	"code.cloudfoundry.org/gorouter/accesslog"
	router_http "code.cloudfoundry.org/gorouter/common/http"
	"code.cloudfoundry.org/gorouter/config"
	"code.cloudfoundry.org/gorouter/errorwriter"
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
)

var (
	headersToAlwaysRemove = []string{"X-CF-Proxy-Signature"}
)

type proxy struct {
	logger                logger.Logger
	errorWriter           errorwriter.ErrorWriter
	reporter              metrics.ProxyReporter
	accessLogger          accesslog.AccessLogger
	promRegistry          handlers.Registry
	health                *health.Health
	routeServiceConfig    *routeservice.RouteServiceConfig
	bufferPool            httputil.BufferPool
	backendTLSConfig      *tls.Config
	routeServiceTLSConfig *tls.Config
	config                *config.Config
}

func NewProxy(
	logger logger.Logger,
	accessLogger accesslog.AccessLogger,
	promRegistry handlers.Registry,
	errorWriter errorwriter.ErrorWriter,
	cfg *config.Config,
	registry registry.Registry,
	reporter metrics.ProxyReporter,
	routeServiceConfig *routeservice.RouteServiceConfig,
	backendTLSConfig *tls.Config,
	routeServiceTLSConfig *tls.Config,
	health *health.Health,
	routeServicesTransport http.RoundTripper,
) http.Handler {

	p := &proxy{
		accessLogger:          accessLogger,
		promRegistry:          promRegistry,
		logger:                logger,
		errorWriter:           errorWriter,
		reporter:              reporter,
		health:                health,
		routeServiceConfig:    routeServiceConfig,
		bufferPool:            NewBufferPool(),
		backendTLSConfig:      backendTLSConfig,
		routeServiceTLSConfig: routeServiceTLSConfig,
		config:                cfg,
	}

	dialer := &net.Dialer{
		Timeout:   cfg.EndpointDialTimeout,
		KeepAlive: cfg.EndpointKeepAliveProbeInterval,
	}

	roundTripperFactory := &round_tripper.FactoryImpl{
		BackendTemplate: &http.Transport{
			DialContext:           dialer.DialContext,
			DisableKeepAlives:     cfg.DisableKeepAlives,
			MaxIdleConns:          cfg.MaxIdleConns,
			IdleConnTimeout:       90 * time.Second, // setting the value to golang default transport
			MaxIdleConnsPerHost:   cfg.MaxIdleConnsPerHost,
			DisableCompression:    true,
			TLSClientConfig:       backendTLSConfig,
			TLSHandshakeTimeout:   cfg.TLSHandshakeTimeout,
			ExpectContinueTimeout: 1 * time.Second,
		},
		RouteServiceTemplate: &http.Transport{
			DialContext:           dialer.DialContext,
			DisableKeepAlives:     cfg.DisableKeepAlives,
			MaxIdleConns:          cfg.MaxIdleConns,
			IdleConnTimeout:       90 * time.Second, // setting the value to golang default transport
			MaxIdleConnsPerHost:   cfg.MaxIdleConnsPerHost,
			DisableCompression:    true,
			TLSClientConfig:       routeServiceTLSConfig,
			ExpectContinueTimeout: 1 * time.Second,
		},
		IsInstrumented: cfg.SendHttpStartStopClientEvent,
	}

	prt := round_tripper.NewProxyRoundTripper(
		roundTripperFactory,
		fails.RetriableClassifiers,
		logger,
		reporter,
		&round_tripper.ErrorHandler{
			MetricReporter: reporter,
			ErrorSpecs:     round_tripper.DefaultErrorSpecs,
		},
		routeServicesTransport,
		cfg,
	)

	rproxy := &httputil.ReverseProxy{
		Director:       p.setupProxyRequest,
		Transport:      prt,
		FlushInterval:  50 * time.Millisecond,
		BufferPool:     p.bufferPool,
		ModifyResponse: p.modifyResponse,
	}

	routeServiceHandler := handlers.NewRouteService(routeServiceConfig, registry, logger, errorWriter)

	zipkinHandler := handlers.NewZipkin(cfg.Tracing.EnableZipkin, logger)
	w3cHandler := handlers.NewW3C(cfg.Tracing.EnableW3C, cfg.Tracing.W3CTenantID, logger)

	headersToLog := utils.CollectHeadersToLog(
		cfg.ExtraHeadersToLog,
		zipkinHandler.HeadersToLog(),
		w3cHandler.HeadersToLog(),
	)

	n := negroni.New()
	n.Use(handlers.NewPanicCheck(p.health, logger))
	n.Use(handlers.NewRequestInfo())
	n.Use(handlers.NewProxyWriter(logger))
	n.Use(zipkinHandler)
	n.Use(w3cHandler)
	n.Use(handlers.NewVcapRequestIdHeader(logger))
	if cfg.SendHttpStartStopServerEvent {
		n.Use(handlers.NewHTTPStartStop(dropsonde.DefaultEmitter, logger))
	}
	if p.promRegistry != nil {
		if cfg.PerAppPrometheusHttpMetricsReporting {
			n.Use(handlers.NewHTTPLatencyPrometheus(p.promRegistry))
		}
	}
	n.Use(handlers.NewAccessLog(accessLogger, headersToLog, cfg.Logging.EnableAttemptsDetails, logger))
	n.Use(handlers.NewQueryParam(logger))
	n.Use(handlers.NewReporter(reporter, logger))
	n.Use(handlers.NewHTTPRewriteHandler(cfg.HTTPRewrite, headersToAlwaysRemove))
	n.Use(handlers.NewProxyHealthcheck(cfg.HealthCheckUserAgent, p.health))
	n.Use(handlers.NewProtocolCheck(logger, errorWriter, cfg.EnableHTTP2))
	n.Use(handlers.NewLookup(registry, reporter, logger, errorWriter, cfg.EmptyPoolResponseCode503))
	n.Use(handlers.NewMaxRequestSize(cfg, logger))
	n.Use(handlers.NewClientCert(
		SkipSanitize(routeServiceHandler.(*handlers.RouteService)),
		ForceDeleteXFCCHeader(routeServiceHandler.(*handlers.RouteService), cfg.ForwardedClientCert, logger),
		cfg.ForwardedClientCert,
		logger,
		errorWriter,
	))
	n.Use(&handlers.XForwardedProto{
		SkipSanitization:         SkipSanitizeXFP(routeServiceHandler.(*handlers.RouteService)),
		ForceForwardedProtoHttps: p.config.ForceForwardedProtoHttps,
		SanitizeForwardedProto:   p.config.SanitizeForwardedProto,
	})
	n.Use(routeServiceHandler)
	n.Use(p)
	n.UseHandler(rproxy)

	return n
}

type RouteServiceValidator interface {
	ArrivedViaRouteService(req *http.Request, logger logger.Logger) (bool, error)
	IsRouteServiceTraffic(req *http.Request) bool
}

func SkipSanitizeXFP(routeServiceValidator RouteServiceValidator) func(*http.Request) bool {
	return func(req *http.Request) bool {
		return routeServiceValidator.IsRouteServiceTraffic(req)
	}
}

func SkipSanitize(routeServiceValidator RouteServiceValidator) func(*http.Request) bool {
	return func(req *http.Request) bool {
		return routeServiceValidator.IsRouteServiceTraffic(req) && (req.TLS != nil)
	}
}

func ForceDeleteXFCCHeader(routeServiceValidator RouteServiceValidator, forwardedClientCert string, logger logger.Logger) func(*http.Request) (bool, error) {
	return func(req *http.Request) (bool, error) {
		valid, err := routeServiceValidator.ArrivedViaRouteService(req, logger)
		if err != nil {
			return false, err
		}
		return valid && forwardedClientCert != config.SANITIZE_SET && forwardedClientCert != config.ALWAYS_FORWARD, nil
	}
}

func (p *proxy) ServeHTTP(responseWriter http.ResponseWriter, request *http.Request, next http.HandlerFunc) {
	logger := handlers.LoggerWithTraceInfo(p.logger, request)
	proxyWriter := responseWriter.(utils.ProxyResponseWriter)

	reqInfo, err := handlers.ContextRequestInfo(request)
	if err != nil {
		logger.Panic("request-info-err", zap.Error(err))
	}
	handler := handler.NewRequestHandler(
		request,
		proxyWriter,
		p.reporter,
		p.logger,
		p.errorWriter,
		p.config.EndpointDialTimeout,
		p.config.WebsocketDialTimeout,
		p.config.Backends.MaxAttempts,
		p.backendTLSConfig,
		p.config.HopByHopHeadersToFilter,
		handler.DisableXFFLogging(p.config.Logging.DisableLogForwardedFor),
		handler.DisableSourceIPLogging(p.config.Logging.DisableLogSourceIP),
	)

	if reqInfo.RoutePool == nil {
		logger.Panic("request-info-err", zap.Error(errors.New("failed-to-access-RoutePool")))
	}

	nestedIterator, err := handlers.EndpointIteratorForRequest(logger, request, p.config.LoadBalance, p.config.StickySessionCookieNames, p.config.StickySessionsForAuthNegotiate, p.config.LoadBalanceAZPreference, p.config.Zone)
	if err != nil {
		logger.Panic("request-info-err", zap.Error(err))
	}

	endpointIterator := &wrappedIterator{
		nested: nestedIterator,

		afterNext: func(endpoint *route.Endpoint) {
			if endpoint != nil {
				reqInfo.RouteEndpoint = endpoint
				p.reporter.CaptureRoutingRequest(endpoint)
			}
		},
	}

	handler.SanitizeRequestConnection()
	if handlers.IsWebSocketUpgrade(request) {
		reqInfo.AppRequestStartedAt = time.Now()
		handler.HandleWebSocketRequest(endpointIterator)
		reqInfo.AppRequestFinishedAt = time.Now()
		return
	}

	reqInfo.AppRequestStartedAt = time.Now()
	next(responseWriter, request)
	reqInfo.AppRequestFinishedAt = time.Now()
}

func (p *proxy) setupProxyRequest(target *http.Request) {
	reqInfo, err := handlers.ContextRequestInfo(target)
	if err != nil {
		p.logger.Panic("request-info-err", zap.Error(err))
		return
	}
	reqInfo.BackendReqHeaders = target.Header

	target.URL.Scheme = "http"
	target.URL.Host = target.Host
	target.URL.ForceQuery = false
	target.URL.Opaque = target.RequestURI

	if strings.HasPrefix(target.RequestURI, "//") {
		path := escapePathAndPreserveSlashes(target.URL.Path)
		target.URL.Opaque = "//" + target.Host + path

		if len(target.URL.Query()) > 0 {
			target.URL.Opaque = target.URL.Opaque + "?" + target.URL.Query().Encode()
		}
	}
	target.URL.RawQuery = ""

	handler.SetRequestXRequestStart(target)
	target.Header.Del(router_http.CfAppInstance)
}

func escapePathAndPreserveSlashes(unescaped string) string {
	parts := strings.Split(unescaped, "/")
	escapedPath := ""
	for _, part := range parts {
		escapedPart := url.PathEscape(part)
		escapedPath = escapedPath + escapedPart + "/"
	}
	escapedPath = strings.TrimSuffix(escapedPath, "/")

	return escapedPath
}

type wrappedIterator struct {
	nested    route.EndpointIterator
	afterNext func(*route.Endpoint)
}

func (i *wrappedIterator) Next(attempt int) *route.Endpoint {
	e := i.nested.Next(attempt)
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
