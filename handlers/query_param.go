package handlers

import (
	"log/slog"
	"net/http"
	"strings"

	router_http "code.cloudfoundry.org/gorouter/common/http"
	"github.com/urfave/negroni/v3"
)

type queryParam struct {
	logger *slog.Logger
}

// NewQueryParam creates a new handler that emits warnings if requests came in with semicolons un-escaped
func NewQueryParam(logger *slog.Logger) negroni.Handler {
	return &queryParam{logger: logger}
}

func (q *queryParam) ServeHTTP(rw http.ResponseWriter, r *http.Request, next http.HandlerFunc) {
	logger := LoggerWithTraceInfo(q.logger, r)
	semicolonInParams := strings.Contains(r.RequestURI, ";")
	if semicolonInParams {
		logger.Warn("deprecated-semicolon-params", slog.String("vcap_request_id", r.Header.Get(VcapRequestIdHeader)))
	}

	next(rw, r)

	if semicolonInParams {
		rw.Header().Add(router_http.CfRouterError, "deprecated-semicolon-params")
	}
}
