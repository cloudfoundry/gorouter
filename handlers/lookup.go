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
	registry           registry.Registry
	reporter           metrics.CombinedReporter
	logger             logger.Logger
	maxConnsPerBackend int64
}

// NewLookup creates a handler responsible for looking up a route.
func NewLookup(registry registry.Registry, rep metrics.CombinedReporter, logger logger.Logger, maxConnsPerBackend int64) negroni.Handler {
	return &lookupHandler{
		registry:           registry,
		reporter:           rep,
		logger:             logger,
		maxConnsPerBackend: maxConnsPerBackend,
	}
}

func (l *lookupHandler) ServeHTTP(rw http.ResponseWriter, r *http.Request, next http.HandlerFunc) {
	pool := l.lookup(r)
	if pool == nil {
		l.handleMissingRoute(rw, r)
		return
	}

	if l.maxConnsPerBackend > 0 && !pool.IsEmpty() {
		newPool := pool.FilteredPool(l.maxConnsPerBackend)
		if newPool.IsEmpty() {
			l.handleOverloadedRoute(rw, r)
			return
		}
		pool = newPool
	}

	requestInfo, err := ContextRequestInfo(r)
	if err != nil {
		l.logger.Fatal("request-info-err", zap.Error(err))
		return
	}
	requestInfo.RoutePool = pool
	next(rw, r)
}

func (l *lookupHandler) handleMissingRoute(rw http.ResponseWriter, r *http.Request) {
	l.reporter.CaptureBadRequest()
	l.logger.Info("unknown-route")

	rw.Header().Set("X-Cf-RouterError", "unknown_route")

	writeStatus(
		rw,
		http.StatusNotFound,
		fmt.Sprintf("Requested route ('%s') does not exist.", r.Host),
		l.logger,
	)
}

func (l *lookupHandler) handleOverloadedRoute(rw http.ResponseWriter, r *http.Request) {
	l.reporter.CaptureBackendExhaustedConns()
	l.logger.Info("connection-limit-reached")

	rw.Header().Set("X-Cf-RouterError", "Connection Limit Reached")

	writeStatus(
		rw,
		http.StatusServiceUnavailable,
		fmt.Sprintf("Requested route ('%s') has reached the connection limit.", r.Host),
		l.logger,
	)
}

func (l *lookupHandler) lookup(r *http.Request) *route.Pool {
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
