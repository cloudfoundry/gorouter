package proxy

import (
	"crypto/tls"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/cloudfoundry/dropsonde"
	"github.com/urfave/negroni/v3"

	"code.cloudfoundry.org/gorouter/accesslog"
	"code.cloudfoundry.org/gorouter/common/health"
	router_http "code.cloudfoundry.org/gorouter/common/http"
	"code.cloudfoundry.org/gorouter/config"
	"code.cloudfoundry.org/gorouter/errorwriter"
	"code.cloudfoundry.org/gorouter/handlers"
	log "code.cloudfoundry.org/gorouter/logger"
	"code.cloudfoundry.org/gorouter/metrics"
	"code.cloudfoundry.org/gorouter/proxy/fails"
	"code.cloudfoundry.org/gorouter/proxy/round_tripper"
	"code.cloudfoundry.org/gorouter/proxy/utils"
	"code.cloudfoundry.org/gorouter/registry"
	"code.cloudfoundry.org/gorouter/routeservice"
)

var (
	headersToAlwaysRemove = []string{"X-CF-Proxy-Signature"}
)

type proxy struct {
	logger                *slog.Logger
	errorWriter           errorwriter.ErrorWriter
	reporter              metrics.ProxyReporter
	accessLogger          accesslog.AccessLogger
	health                *health.Health
	routeServiceConfig    *routeservice.RouteServiceConfig
	bufferPool            httputil.BufferPool
	backendTLSConfig      *tls.Config
	routeServiceTLSConfig *tls.Config
	config                *config.Config
}

func NewProxy(
	logger *slog.Logger,
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
		accessLogger:          accessLogger,
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
			DialContext:            dialer.DialContext,
			DisableKeepAlives:      cfg.DisableKeepAlives,
			MaxIdleConns:           cfg.MaxIdleConns,
			IdleConnTimeout:        90 * time.Second, // setting the value to golang default transport
			MaxIdleConnsPerHost:    cfg.MaxIdleConnsPerHost,
			DisableCompression:     true,
			TLSClientConfig:        backendTLSConfig,
			TLSHandshakeTimeout:    cfg.TLSHandshakeTimeout,
			ExpectContinueTimeout:  1 * time.Second,
			MaxResponseHeaderBytes: int64(cfg.MaxResponseHeaderBytes),
		},
		RouteServiceTemplate: &http.Transport{
			DialContext:            dialer.DialContext,
			DisableKeepAlives:      cfg.DisableKeepAlives,
			MaxIdleConns:           cfg.MaxIdleConns,
			IdleConnTimeout:        90 * time.Second, // setting the value to golang default transport
			MaxIdleConnsPerHost:    cfg.MaxIdleConnsPerHost,
			DisableCompression:     true,
			TLSClientConfig:        routeServiceTLSConfig,
			ExpectContinueTimeout:  1 * time.Second,
			MaxResponseHeaderBytes: int64(cfg.MaxResponseHeaderBytes),
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

	// by default, we should close 100-continue requests for safety
	// this requires a second proxy because Director and Rewrite cannot coexist
	// additionally, Director() is called before hop-by-hop headers are sanitized
	// whereas Rewrite is after, and this is where `Connection: close` can be added
	expect100ContinueRProxy := &httputil.ReverseProxy{
		Rewrite:        p.setupProxyRequestClose100Continue,
		Transport:      prt,
		FlushInterval:  50 * time.Millisecond,
		BufferPool:     p.bufferPool,
		ModifyResponse: p.modifyResponse,
	}

	// if we want to not force close 100-continue requests, use the normal rproxy
	if cfg.KeepAlive100ContinueRequests {
		expect100ContinueRProxy = rproxy
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
	if cfg.PerAppPrometheusHttpMetricsReporting {
		n.Use(handlers.NewHTTPLatencyPrometheus(p.reporter))
	}
	n.Use(handlers.NewAccessLog(accessLogger, headersToLog, cfg.Logging.ExtraAccessLogFields, logger))
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
	n.Use(handlers.NewHopByHop(cfg, logger))
	n.Use(&handlers.XForwardedProto{
		SkipSanitization:         SkipSanitizeXFP(routeServiceHandler.(*handlers.RouteService)),
		ForceForwardedProtoHttps: p.config.ForceForwardedProtoHttps,
		SanitizeForwardedProto:   p.config.SanitizeForwardedProto,
	})
	n.Use(routeServiceHandler)
	n.Use(p)
	n.Use(handlers.NewProxyPicker(rproxy, expect100ContinueRProxy))

	return n
}

type RouteServiceValidator interface {
	ArrivedViaRouteService(req *http.Request, logger *slog.Logger) (bool, error)
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

func ForceDeleteXFCCHeader(routeServiceValidator RouteServiceValidator, forwardedClientCert string, logger *slog.Logger) func(*http.Request) (bool, error) {
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

	if p.config.EnableHTTP1ConcurrentReadWrite && request.ProtoMajor == 1 {
		rc := http.NewResponseController(proxyWriter)

		err := rc.EnableFullDuplex()
		if err != nil {
			log.Panic(logger, "enable-full-duplex-err", log.ErrAttr(err))
		}
	}

	reqInfo, err := handlers.ContextRequestInfo(request)
	if err != nil {
		log.Panic(logger, "request-info-err", log.ErrAttr(err))
	}

	if reqInfo.RoutePool == nil {
		log.Panic(logger, "request-info-err", log.ErrAttr(errors.New("failed-to-access-RoutePool")))
	}

	reqInfo.AppRequestStartedAt = time.Now()
	next(responseWriter, request)
	reqInfo.AppRequestFinishedAt = time.Now()
}

func (p *proxy) setupProxyRequest(target *http.Request) {
	reqInfo, err := handlers.ContextRequestInfo(target)
	if err != nil {
		log.Panic(p.logger, "request-info-err", log.ErrAttr(err))
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

	setRequestXRequestStart(target)
	target.Header.Del(router_http.CfAppInstance)
}

func (p *proxy) setupProxyRequestClose100Continue(target *httputil.ProxyRequest) {
	reqInfo, err := handlers.ContextRequestInfo(target.In)
	if err != nil {
		log.Panic(p.logger, "request-info-err", log.ErrAttr(err))
		return
	}
	reqInfo.BackendReqHeaders = target.Out.Header

	target.Out.URL.Scheme = "http"
	target.Out.URL.Host = target.In.Host
	target.Out.URL.ForceQuery = false
	target.Out.URL.Opaque = target.In.RequestURI

	if strings.HasPrefix(target.In.RequestURI, "//") {
		path := escapePathAndPreserveSlashes(target.In.URL.Path)
		target.Out.URL.Opaque = "//" + target.In.Host + path

		if len(target.In.URL.Query()) > 0 {
			target.Out.URL.Opaque = target.Out.URL.Opaque + "?" + target.In.URL.Query().Encode()
		}
	}
	target.Out.URL.RawQuery = ""

	setRequestXRequestStart(target.Out)
	target.Out.Header.Del(router_http.CfAppInstance)

	// always set connection close on 100-continue requests
	target.Out.Header.Set("Connection", "close")

	target.Out.Header["X-Forwarded-For"] = target.In.Header["X-Forwarded-For"]
	target.Out.Header["Forwarded"] = target.In.Header["Forwarded"]
	// Takes care of setting the X-Forwarded-For header properly. Also sets the X-Forwarded-Proto
	// which we overwrite again.
	target.SetXForwarded()
	target.Out.Header["X-Forwarded-Proto"] = target.In.Header["X-Forwarded-Proto"]
	target.Out.Header["X-Forwarded-Host"] = target.In.Header["X-Forwarded-Host"]
}

func setRequestXRequestStart(request *http.Request) {
	if _, ok := request.Header[http.CanonicalHeaderKey("X-Request-Start")]; !ok {
		request.Header.Set("X-Request-Start", strconv.FormatInt(time.Now().UnixNano()/1e6, 10))
	}
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
