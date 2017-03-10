package handlers

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"code.cloudfoundry.org/gorouter/access_log/schema"
	router_http "code.cloudfoundry.org/gorouter/common/http"
	"code.cloudfoundry.org/gorouter/logger"
	"code.cloudfoundry.org/gorouter/metrics"
	"code.cloudfoundry.org/gorouter/registry"
	"code.cloudfoundry.org/gorouter/route"
	"github.com/uber-go/zap"
	"github.com/urfave/negroni"
)

type lookupHandler struct {
	registry registry.Registry
	reporter metrics.CombinedReporter
	logger   logger.Logger
}

// NewLookup creates a handler responsible for looking up a route.
func NewLookup(registry registry.Registry, rep metrics.CombinedReporter, logger logger.Logger) negroni.Handler {
	return &lookupHandler{
		registry: registry,
		reporter: rep,
		logger:   logger,
	}
}

func (l *lookupHandler) ServeHTTP(rw http.ResponseWriter, r *http.Request, next http.HandlerFunc) {
	pool := l.lookup(r)
	if pool == nil {
		l.handleMissingRoute(rw, r)
		return
	}
	r = r.WithContext(context.WithValue(r.Context(), "RoutePool", pool))
	next(rw, r)
}

func (l *lookupHandler) handleMissingRoute(rw http.ResponseWriter, r *http.Request) {
	l.reporter.CaptureBadRequest()
	l.logger.Info("unknown-route")

	rw.Header().Set("X-Cf-RouterError", "unknown_route")
	code := http.StatusNotFound
	body := fmt.Sprintf(
		"%d %s: Requested route ('%s') does not exist.",
		code,
		http.StatusText(code),
		r.Host,
	)
	l.logger.Info("status", zap.String("body", body))

	alr := r.Context().Value("AccessLogRecord")
	if accessLogRecord, ok := alr.(*schema.AccessLogRecord); ok {

		accessLogRecord.StatusCode = code
	} else {
		l.logger.Error("AccessLogRecord-not-set-on-context", zap.Error(errors.New("failed-to-access-log-record")))
	}
	http.Error(rw, body, code)
	rw.Header().Del("Connection")
}

func (l *lookupHandler) lookup(r *http.Request) *route.Pool {
	requestPath := r.URL.EscapedPath()

	uri := route.Uri(hostWithoutPort(r) + requestPath)
	appInstanceHeader := r.Header.Get(router_http.CfAppInstance)

	if appInstanceHeader != "" {
		appID, appIndex, err := router_http.ValidateCfAppInstance(appInstanceHeader)

		if err != nil {
			l.logger.Error("invalid-app-instance-header", zap.Error(err))
			return nil
		}

		return l.registry.LookupWithInstance(uri, appID, appIndex)
	}

	return l.registry.Lookup(uri)
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
