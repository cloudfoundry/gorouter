package proxy

import (
	"crypto/tls"
	"errors"
	"net"
	"net/http"
	"net/http/httputil"
	"strings"
	"time"

	"code.cloudfoundry.org/gorouter/common/health"

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
	"github.com/cloudfoundry/dropsonde"
	"github.com/uber-go/zap"
	"github.com/urfave/negroni"
)

const (
	VcapCookieId = "__VCAP_ID__"
)

var (
	headersToAlwaysRemove = []string{"X-CF-Proxy-Signature"}
)

type proxy struct {
	ip                       string
	traceKey                 string
	logger                   logger.Logger
	errorWriter              errorwriter.ErrorWriter
	reporter                 metrics.ProxyReporter
	accessLogger             accesslog.AccessLogger
	secureCookies            bool
	health                   *health.Health
	routeServiceConfig       *routeservice.RouteServiceConfig
	healthCheckUserAgent     string
	forceForwardedProtoHttps bool
	sanitizeForwardedProto   bool
	defaultLoadBalance       string
	endpointDialTimeout      time.Duration
	endpointTimeout          time.Duration
	bufferPool               httputil.BufferPool
	backendTLSConfig         *tls.Config
	routeServiceTLSConfig    *tls.Config
	disableXFFLogging        bool
	disableSourceIPLogging   bool
	stickySessionCookieNames config.StringSet
}

func NewProxy(
	logger logger.Logger,
	accessLogger accesslog.AccessLogger,
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
		accessLogger:             accessLogger,
		traceKey:                 cfg.TraceKey,
		ip:                       cfg.Ip,
		logger:                   logger,
		errorWriter:              errorWriter,
		reporter:                 reporter,
		secureCookies:            cfg.SecureCookies,
		health:                   health,
		routeServiceConfig:       routeServiceConfig,
		healthCheckUserAgent:     cfg.HealthCheckUserAgent,
		forceForwardedProtoHttps: cfg.ForceForwardedProtoHttps,
		sanitizeForwardedProto:   cfg.SanitizeForwardedProto,
		defaultLoadBalance:       cfg.LoadBalance,
		endpointDialTimeout:      cfg.EndpointDialTimeout,
		endpointTimeout:          cfg.EndpointTimeout,
		bufferPool:               NewBufferPool(),
		backendTLSConfig:         backendTLSConfig,
		routeServiceTLSConfig:    routeServiceTLSConfig,
		disableXFFLogging:        cfg.Logging.DisableLogForwardedFor,
		disableSourceIPLogging:   cfg.Logging.DisableLogSourceIP,
		stickySessionCookieNames: cfg.StickySessionCookieNames,
	}

	dialer := &net.Dialer{
		Timeout:   cfg.EndpointDialTimeout,
		KeepAlive: cfg.EndpointKeepAliveProbeInterval,
	}

	roundTripperFactory := &round_tripper.FactoryImpl{
		BackendTemplate: &http.Transport{
			Dial:                dialer.Dial,
			DisableKeepAlives:   cfg.DisableKeepAlives,
			MaxIdleConns:        cfg.MaxIdleConns,
			IdleConnTimeout:     90 * time.Second, // setting the value to golang default transport
			MaxIdleConnsPerHost: cfg.MaxIdleConnsPerHost,
			DisableCompression:  true,
			TLSClientConfig:     backendTLSConfig,
			TLSHandshakeTimeout: cfg.TLSHandshakeTimeout,
		},
		RouteServiceTemplate: &http.Transport{
			Dial:                dialer.Dial,
			DisableKeepAlives:   cfg.DisableKeepAlives,
			MaxIdleConns:        cfg.MaxIdleConns,
			IdleConnTimeout:     90 * time.Second, // setting the value to golang default transport
			MaxIdleConnsPerHost: cfg.MaxIdleConnsPerHost,
			DisableCompression:  true,
			TLSClientConfig:     routeServiceTLSConfig,
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
	n.Use(handlers.NewVcapRequestIdHeader(logger))
	if cfg.SendHttpStartStopServerEvent {
		n.Use(handlers.NewHTTPStartStop(dropsonde.DefaultEmitter, logger))
	}
	n.Use(handlers.NewAccessLog(accessLogger, headersToLog, logger))
	n.Use(handlers.NewReporter(reporter, logger))
	n.Use(handlers.NewHTTPRewriteHandler(cfg.HTTPRewrite, headersToAlwaysRemove))
	n.Use(handlers.NewProxyHealthcheck(cfg.HealthCheckUserAgent, p.health, logger))
	n.Use(zipkinHandler)
	n.Use(w3cHandler)
	n.Use(handlers.NewProtocolCheck(logger, errorWriter, cfg.EnableHTTP2))
	n.Use(handlers.NewLookup(registry, reporter, logger, errorWriter, cfg.EmptyPoolResponseCode503))
	n.Use(handlers.NewClientCert(
		SkipSanitize(routeServiceHandler.(*handlers.RouteService)),
		ForceDeleteXFCCHeader(routeServiceHandler.(*handlers.RouteService), cfg.ForwardedClientCert),
		cfg.ForwardedClientCert,
		logger,
		errorWriter,
	))
	n.Use(&handlers.XForwardedProto{
		SkipSanitization:         SkipSanitizeXFP(routeServiceHandler.(*handlers.RouteService)),
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
		p.errorWriter,
		p.endpointDialTimeout,
		p.backendTLSConfig,
		handler.DisableXFFLogging(p.disableXFFLogging),
		handler.DisableSourceIPLogging(p.disableSourceIPLogging),
	)

	if reqInfo.RoutePool == nil {
		p.logger.Fatal("request-info-err", zap.Error(errors.New("failed-to-access-RoutePool")))
	}

	stickyEndpointId := getStickySession(request, p.stickySessionCookieNames)
	endpointIterator := &wrappedIterator{
		nested: reqInfo.RoutePool.Endpoints(p.defaultLoadBalance, stickyEndpointId),

		afterNext: func(endpoint *route.Endpoint) {
			if endpoint != nil {
				reqInfo.RouteEndpoint = endpoint
				p.reporter.CaptureRoutingRequest(endpoint)
			}
		},
	}

	if handlers.IsWebSocketUpgrade(request) {
		handler.HandleWebSocketRequest(endpointIterator)
		return
	}

	reqInfo.AppRequestStartedAt = time.Now()
	next(responseWriter, request)
	reqInfo.AppRequestFinishedAt = time.Now()
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

func getStickySession(request *http.Request, stickySessionCookieNames config.StringSet) string {
	// Try choosing a backend using sticky session
	for stickyCookieName, _ := range stickySessionCookieNames {
		if _, err := request.Cookie(stickyCookieName); err == nil {
			if sticky, err := request.Cookie(VcapCookieId); err == nil {
				return sticky.Value
			}
		}
	}
	return ""
}
