package handlers

import (
	"fmt"
	"log/slog"
	"net/http"
	"regexp"
	"strings"

	"github.com/urfave/negroni/v3"

	router_http "code.cloudfoundry.org/gorouter/common/http"
	"code.cloudfoundry.org/gorouter/errorwriter"
	log "code.cloudfoundry.org/gorouter/logger"
	"code.cloudfoundry.org/gorouter/metrics"
	"code.cloudfoundry.org/gorouter/registry"
	"code.cloudfoundry.org/gorouter/route"
)

const CfAppInstance = "X-CF-APP-INSTANCE"

type InvalidAppInstanceHeaderError struct {
	headerValue string
}

func (err InvalidAppInstanceHeaderError) Error() string {
	return fmt.Sprintf("invalid-app-instance-header: %s", err.headerValue)
}

type InvalidProcessInstanceHeaderError struct {
	headerValue string
}

func (err InvalidProcessInstanceHeaderError) Error() string {
	return fmt.Sprintf("invalid-process-instance-header: %s", err.headerValue)
}

type lookupHandler struct {
	registry                 registry.Registry
	reporter                 metrics.ProxyReporter
	logger                   *slog.Logger
	errorWriter              errorwriter.ErrorWriter
	EmptyPoolResponseCode503 bool
}

// NewLookup creates a handler responsible for looking up a route.
func NewLookup(
	registry registry.Registry,
	rep metrics.ProxyReporter,
	logger *slog.Logger,
	ew errorwriter.ErrorWriter,
	emptyPoolResponseCode503 bool,
) negroni.Handler {
	return &lookupHandler{
		registry:                 registry,
		reporter:                 rep,
		logger:                   logger,
		errorWriter:              ew,
		EmptyPoolResponseCode503: emptyPoolResponseCode503,
	}
}

func (l *lookupHandler) ServeHTTP(rw http.ResponseWriter, r *http.Request, next http.HandlerFunc) {
	logger := LoggerWithTraceInfo(l.logger, r)
	// gorouter requires the Host header to know to which backend to proxy to.
	//
	// The Host header is optional in HTTP/1.0,
	// so we must explicitly check that the Host is set.
	//
	// Also, when some load balancers which are used to load balance gorouters
	// (e.g. AWS Classic ELB)
	// receive HTTP/1.0 requests without a host headers,
	// they optimistically add their IP address to the host header.
	//
	// Therefore to not expose the internal IP address of the load balancer,
	// we must check that the Host is not also the source of the request.
	//
	// It is not vali for the Host header to be an IP address,
	// because Gorouter should not have an IP address as a route.
	if r.Host == "" || hostWithoutPort(r.Host) == hostWithoutPort(r.RemoteAddr) {
		l.handleMissingHost(rw, r, logger)
		return
	}

	pool, err := l.lookup(r, logger)
	if _, ok := err.(InvalidAppInstanceHeaderError); ok {
		l.handleInvalidAppInstanceHeader(rw, r, logger)
		return
	}

	if _, ok := err.(InvalidProcessInstanceHeaderError); ok {
		l.handleInvalidProcessInstanceHeader(rw, r, logger)
		return
	}

	if pool == nil {
		l.handleMissingRoute(rw, r, logger)
		return
	}

	if pool.IsEmpty() {
		if l.EmptyPoolResponseCode503 {
			l.handleUnavailableRoute(rw, r, logger)
			return
		} else {
			l.handleMissingRoute(rw, r, logger)
			return
		}
	}

	if pool.IsOverloaded() {
		l.handleOverloadedRoute(rw, r, logger)
		return
	}

	requestInfo, err := ContextRequestInfo(r)
	if err != nil {
		log.Panic(logger, "request-info-err", log.ErrAttr(err))
		return
	}
	requestInfo.RoutePool = pool
	next(rw, r)
}

func (l *lookupHandler) handleInvalidAppInstanceHeader(rw http.ResponseWriter, r *http.Request, logger *slog.Logger) {
	l.reporter.CaptureBadRequest()

	AddRouterErrorHeader(rw, "invalid_cf_app_instance_header")
	addNoCacheControlHeader(rw)

	l.errorWriter.WriteError(
		rw,
		http.StatusBadRequest,
		"Invalid X-CF-App-Instance Header",
		logger,
	)
}

func (l *lookupHandler) handleInvalidProcessInstanceHeader(rw http.ResponseWriter, r *http.Request, logger *slog.Logger) {
	l.reporter.CaptureBadRequest()

	AddRouterErrorHeader(rw, "invalid_cf_process_instance_header")
	addNoCacheControlHeader(rw)

	l.errorWriter.WriteError(
		rw,
		http.StatusBadRequest,
		"Invalid X-CF-Process-Instance Header",
		logger,
	)
}

func (l *lookupHandler) handleMissingHost(rw http.ResponseWriter, r *http.Request, logger *slog.Logger) {
	l.reporter.CaptureBadRequest()

	AddRouterErrorHeader(rw, "empty_host")
	addInvalidResponseCacheControlHeader(rw)

	l.errorWriter.WriteError(
		rw,
		http.StatusBadRequest,
		"Request had empty Host header",
		logger,
	)
}

func (l *lookupHandler) handleMissingRoute(rw http.ResponseWriter, r *http.Request, logger *slog.Logger) {
	l.reporter.CaptureBadRequest()

	AddRouterErrorHeader(rw, "unknown_route")
	addNoCacheControlHeader(rw)

	errorMsg := fmt.Sprintf("Requested route ('%s') does not exist.", r.Host)
	returnStatus := http.StatusNotFound

	if appInstanceHeader := r.Header.Get(router_http.CfAppInstance); appInstanceHeader != "" {
		guid, idx := splitInstanceHeader(appInstanceHeader)
		errorMsg = fmt.Sprintf("Requested instance ('%s') with guid ('%s') does not exist for route ('%s')", idx, guid, r.Host)
		returnStatus = http.StatusBadRequest
	}

	if processInstanceHeader := r.Header.Get(router_http.CfProcessInstance); processInstanceHeader != "" {
		guid, idx := splitInstanceHeader(processInstanceHeader)
		errorMsg = fmt.Sprintf("Requested instance ('%s') with process guid ('%s') does not exist for route ('%s')", idx, guid, r.Host)
		returnStatus = http.StatusBadRequest
	}

	l.errorWriter.WriteError(
		rw,
		returnStatus,
		errorMsg,
		logger,
	)
}

func (l *lookupHandler) handleUnavailableRoute(rw http.ResponseWriter, r *http.Request, logger *slog.Logger) {
	AddRouterErrorHeader(rw, "no_endpoints")
	addInvalidResponseCacheControlHeader(rw)

	l.errorWriter.WriteError(
		rw,
		http.StatusServiceUnavailable,
		fmt.Sprintf("Requested route ('%s') has no available endpoints.", r.Host),
		logger,
	)
}

func (l *lookupHandler) handleOverloadedRoute(rw http.ResponseWriter, r *http.Request, logger *slog.Logger) {
	l.reporter.CaptureBackendExhaustedConns()
	l.logger.Info("connection-limit-reached")

	AddRouterErrorHeader(rw, "Connection Limit Reached")

	l.errorWriter.WriteError(
		rw,
		http.StatusServiceUnavailable,
		fmt.Sprintf("Requested route ('%s') has reached the connection limit.", r.Host),
		logger,
	)
}

func (l *lookupHandler) lookup(r *http.Request, logger *slog.Logger) (*route.EndpointPool, error) {
	requestPath := r.URL.EscapedPath()

	uri := route.Uri(hostWithoutPort(r.Host) + requestPath)
	appInstanceHeader := r.Header.Get(router_http.CfAppInstance)

	if appInstanceHeader != "" {
		err := validateAppInstanceHeader(appInstanceHeader)
		if err != nil {
			logger.Error("invalid-app-instance-header", log.ErrAttr(err))
			return nil, InvalidAppInstanceHeaderError{headerValue: appInstanceHeader}
		}

		appID, appIndex := splitInstanceHeader(appInstanceHeader)
		return l.registry.LookupWithAppInstance(uri, appID, appIndex), nil
	}

	processInstanceHeader := r.Header.Get(router_http.CfProcessInstance)
	if processInstanceHeader != "" {
		err := validateProcessInstanceHeader(processInstanceHeader)
		if err != nil {
			logger.Error("invalid-process-instance-header", log.ErrAttr(err))
			return nil, InvalidProcessInstanceHeaderError{headerValue: processInstanceHeader}
		}

		processID, processIndex := splitInstanceHeader(processInstanceHeader)
		return l.registry.LookupWithProcessInstance(uri, processID, processIndex), nil
	}

	return l.registry.Lookup(uri), nil
}

// Regex to match format of `APP_GUID:INSTANCE_ID`
var appInstanceHeaderRegex = regexp.MustCompile(`^[\da-f]{8}-([\da-f]{4}-){3}[\da-f]{12}:\d+$`)

func validateAppInstanceHeader(appInstanceHeader string) error {
	if !appInstanceHeaderRegex.MatchString(appInstanceHeader) {
		return fmt.Errorf("Incorrect %s header : %s", CfAppInstance, appInstanceHeader)
	}
	return nil
}

// Regex to match format of `PROCESS_GUID:INSTANCE_ID` and `PROCESS_GUID`
var processInstanceHeaderRegex = regexp.MustCompile(`^[\da-f]{8}-([\da-f]{4}-){3}[\da-f]{12}(:\d+)?$`)

func validateProcessInstanceHeader(processInstanceHeader string) error {
	if !processInstanceHeaderRegex.MatchString(processInstanceHeader) {
		return fmt.Errorf("Incorrect %s header : %s", router_http.CfProcessInstance, processInstanceHeader)
	}
	return nil
}

func splitInstanceHeader(instanceHeader string) (string, string) {
	details := strings.Split(instanceHeader, ":")
	if len(details) == 1 {
		return details[0], ""
	}
	return details[0], details[1]
}
