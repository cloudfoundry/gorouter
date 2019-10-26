package handlers

import (
	"net/http"
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

const (
	CfInstanceIdHeader = "X-CF-InstanceID"
	CfAppInstance      = "X-CF-APP-INSTANCE"
)

type lookupHandler struct {
	registry registry.Registry
	reporter metrics.ProxyReporter
	logger   logger.Logger
}

// NewLookup creates a handler responsible for looking up a route.
func NewLookup(registry registry.Registry, rep metrics.ProxyReporter, logger logger.Logger) negroni.Handler {
	return &lookupHandler{
		registry: registry,
		reporter: rep,
		logger:   logger,
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

	pool := l.lookup(r)
	if pool == nil {
		l.handleMissingRoute(rw, r)
		return
	}

	if pool.IsEmpty() {
		l.handleUnavailableRoute(rw, r)
		return
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

func (l *lookupHandler) handleMissingHost(rw http.ResponseWriter, r *http.Request) {
	l.reporter.CaptureBadRequest()

	AddRouterErrorHeader(rw, "empty_host")
	addInvalidResponseCacheControlHeader(rw)

	writeStatus(
		rw,
		http.StatusBadRequest,
		"Request had empty Host header",
		l.logger,
	)
}

func (l *lookupHandler) handleMissingRoute(rw http.ResponseWriter, r *http.Request) {
	l.reporter.CaptureBadRequest()

	AddRouterErrorHeader(rw, "unknown_route")
	addInvalidResponseCacheControlHeader(rw)

	writeStatus(
		rw,
		http.StatusNotFound,
		fmt.Sprintf("Requested route ('%s') does not exist.", r.Host),
		l.logger,
	)
}

func (l *lookupHandler) handleUnavailableRoute(rw http.ResponseWriter, r *http.Request) {
	AddRouterErrorHeader(rw, "no_endpoints")
	addInvalidResponseCacheControlHeader(rw)

	writeStatus(
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

	writeStatus(
		rw,
		http.StatusServiceUnavailable,
		fmt.Sprintf("Requested route ('%s') has reached the connection limit.", r.Host),
		l.logger,
	)
}

func (l *lookupHandler) lookup(r *http.Request) *route.EndpointPool {
	requestPath := r.URL.EscapedPath()

	uri := route.Uri(hostWithoutPort(r.Host) + requestPath)
	appInstanceHeader := r.Header.Get(router_http.CfAppInstance)

	if appInstanceHeader != "" {
		appID, appIndex, err := validateCfAppInstance(appInstanceHeader)

		if err != nil {
			l.logger.Error("invalid-app-instance-header", zap.Error(err))
			return nil
		}

		return l.registry.LookupWithInstance(uri, appID, appIndex)
	}

	return l.registry.Lookup(uri)
}

func validateCfAppInstance(appInstanceHeader string) (string, string, error) {
	appDetails := strings.Split(appInstanceHeader, ":")
	if len(appDetails) != 2 {
		return "", "", fmt.Errorf("Incorrect %s header : %s", CfAppInstance, appInstanceHeader)
	}

	if appDetails[0] == "" || appDetails[1] == "" {
		return "", "", fmt.Errorf("Incorrect %s header : %s", CfAppInstance, appInstanceHeader)
	}

	return appDetails[0], appDetails[1], nil
}
