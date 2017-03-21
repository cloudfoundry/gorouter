package handlers

import (
	"errors"
	"net/http"
	"time"

	"code.cloudfoundry.org/gorouter/access_log/schema"
	"code.cloudfoundry.org/gorouter/logger"
	"code.cloudfoundry.org/gorouter/metrics"
	"code.cloudfoundry.org/gorouter/proxy/utils"

	"github.com/uber-go/zap"
	"github.com/urfave/negroni"
)

type reporterHandler struct {
	reporter metrics.CombinedReporter
	logger   logger.Logger
}

// NewReporter creates a new handler that handles reporting backend
// responses to metrics
func NewReporter(reporter metrics.CombinedReporter, logger logger.Logger) negroni.Handler {
	return &reporterHandler{
		reporter: reporter,
		logger:   logger,
	}
}

// ServeHTTP handles reporting the response after the request has been completed
func (rh *reporterHandler) ServeHTTP(rw http.ResponseWriter, r *http.Request, next http.HandlerFunc) {
	next(rw, r)

	alr := r.Context().Value("AccessLogRecord")
	if alr == nil {
		rh.logger.Error("AccessLogRecord-not-set-on-context", zap.Error(errors.New("failed-to-access-log-record")))
		return
	}
	accessLog := alr.(*schema.AccessLogRecord)

	if accessLog.RouteEndpoint == nil {
		return
	}

	proxyWriter := rw.(utils.ProxyResponseWriter)
	rh.reporter.CaptureRoutingResponse(proxyWriter.Status())
	rh.reporter.CaptureRoutingResponseLatency(
		accessLog.RouteEndpoint, proxyWriter.Status(),
		accessLog.StartedAt, time.Since(accessLog.StartedAt),
	)
}
