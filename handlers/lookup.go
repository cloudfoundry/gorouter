package handlers

import (
	"net/http"
	"regexp"
	"strings"

	"fmt"

	router_http "code.cloudfoundry.org/gorouter/common/http"
	"code.cloudfoundry.org/gorouter/logger"
	"code.cloudfoundry.org/gorouter/metrics"
	"code.cloudfoundry.org/gorouter/registry"
	"code.cloudfoundry.org/gorouter/route"
	"github.com/uber-go/zap"
	"github.com/urfave/negroni"
)

const CfAppInstance = "X-CF-APP-INSTANCE"

type InvalidInstanceHeaderError struct {
	headerValue string
}

func (err InvalidInstanceHeaderError) Error() string {
	return fmt.Sprintf("invalid-app-instance-header: %s", err.headerValue)
}

type lookupHandler struct {
	registry                 registry.Registry
	reporter                 metrics.ProxyReporter
	logger                   logger.Logger
	errorWriter              ErrorWriter
	EmptyPoolResponseCode503 bool
}

// NewLookup creates a handler responsible for looking up a route.
func NewLookup(
	registry registry.Registry,
	rep metrics.ProxyReporter,
	logger logger.Logger,
	ew ErrorWriter,
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
		l.handleMissingHost(rw, r)
		return
	}

	pool, err := l.lookup(r)
	if _, ok := err.(InvalidInstanceHeaderError); ok {
		l.handleInvalidInstanceHeader(rw, r)
		return
	}

	if pool == nil {
		l.handleMissingRoute(rw, r)
		return
	}

	if pool.IsEmpty() {
		if l.EmptyPoolResponseCode503 {
			l.handleUnavailableRoute(rw, r)
			return
		} else {
			l.handleMissingRoute(rw, r)
			return
		}
	}

	if pool.IsOverloaded() {
		l.handleOverloadedRoute(rw, r)
		return
	}

	requestInfo, err := ContextRequestInfo(r)
	if err != nil {
		l.logger.Fatal("request-info-err", zap.Error(err))
		return
	}
	requestInfo.RoutePool = pool
	next(rw, r)
}

func (l *lookupHandler) handleInvalidInstanceHeader(rw http.ResponseWriter, r *http.Request) {
	l.reporter.CaptureBadRequest()

	AddRouterErrorHeader(rw, "invalid_cf_app_instance_header")
	addNoCacheControlHeader(rw)

	l.errorWriter.WriteError(
		rw,
		http.StatusBadRequest,
		"Invalid X-CF-App-Instance Header",
		l.logger,
	)
}

func (l *lookupHandler) handleMissingHost(rw http.ResponseWriter, r *http.Request) {
	l.reporter.CaptureBadRequest()

	AddRouterErrorHeader(rw, "empty_host")
	addInvalidResponseCacheControlHeader(rw)

	l.errorWriter.WriteError(
		rw,
		http.StatusBadRequest,
		"Request had empty Host header",
		l.logger,
	)
}

func (l *lookupHandler) handleMissingRoute(rw http.ResponseWriter, r *http.Request) {
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

	l.errorWriter.WriteError(
		rw,
		returnStatus,
		errorMsg,
		l.logger,
	)
}

func (l *lookupHandler) handleUnavailableRoute(rw http.ResponseWriter, r *http.Request) {
	AddRouterErrorHeader(rw, "no_endpoints")
	addInvalidResponseCacheControlHeader(rw)

	l.errorWriter.WriteError(
		rw,
		http.StatusServiceUnavailable,
		fmt.Sprintf("Requested route ('%s') has no available endpoints.", r.Host),
		l.logger,
	)
}

func (l *lookupHandler) handleOverloadedRoute(rw http.ResponseWriter, r *http.Request) {
	l.reporter.CaptureBackendExhaustedConns()
	l.logger.Info("connection-limit-reached")

	AddRouterErrorHeader(rw, "Connection Limit Reached")

	l.errorWriter.WriteError(
		rw,
		http.StatusServiceUnavailable,
		fmt.Sprintf("Requested route ('%s') has reached the connection limit.", r.Host),
		l.logger,
	)
}

func (l *lookupHandler) lookup(r *http.Request) (*route.EndpointPool, error) {
	requestPath := r.URL.EscapedPath()

	uri := route.Uri(hostWithoutPort(r.Host) + requestPath)
	appInstanceHeader := r.Header.Get(router_http.CfAppInstance)

	if appInstanceHeader != "" {
		err := validateInstanceHeader(appInstanceHeader)
		if err != nil {
			l.logger.Error("invalid-app-instance-header", zap.Error(err))
			return nil, InvalidInstanceHeaderError{headerValue: appInstanceHeader}
		}

		appID, appIndex := splitInstanceHeader(appInstanceHeader)
		return l.registry.LookupWithInstance(uri, appID, appIndex), nil
	}

	return l.registry.Lookup(uri), nil
}

func validateInstanceHeader(appInstanceHeader string) error {
	// Regex to match format of `APP_GUID:INSTANCE_ID`
	r := regexp.MustCompile(`^[\da-f]{8}-([\da-f]{4}-){3}[\da-f]{12}:\d+$`)
	if !r.MatchString(appInstanceHeader) {
		return fmt.Errorf("Incorrect %s header : %s", CfAppInstance, appInstanceHeader)
	}
	return nil
}

func splitInstanceHeader(appInstanceHeader string) (string, string) {
	appDetails := strings.Split(appInstanceHeader, ":")
	return appDetails[0], appDetails[1]
}
