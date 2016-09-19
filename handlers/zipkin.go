package handlers

import (
	"net/http"

	"github.com/urfave/negroni"

	router_http "code.cloudfoundry.org/gorouter/common/http"
	"code.cloudfoundry.org/lager"
)

type zipkin struct {
	zipkinEnabled bool
	logger        lager.Logger
	headersToLog  *[]string // Shared state with proxy for access logs
}

func NewZipkin(enabled bool, headersToLog *[]string, logger lager.Logger) negroni.Handler {
	return &zipkin{
		zipkinEnabled: enabled,
		headersToLog:  headersToLog,
		logger:        logger,
	}
}

func (z *zipkin) ServeHTTP(rw http.ResponseWriter, r *http.Request, next http.HandlerFunc) {
	defer next(rw, r)
	if !z.zipkinEnabled {
		return
	}
	router_http.SetB3Headers(r, z.logger)

	if !contains(*z.headersToLog, router_http.B3TraceIdHeader) {
		*z.headersToLog = append(*z.headersToLog, router_http.B3TraceIdHeader)
	}

	if !contains(*z.headersToLog, router_http.B3SpanIdHeader) {
		*z.headersToLog = append(*z.headersToLog, router_http.B3SpanIdHeader)
	}

	if !contains(*z.headersToLog, router_http.B3ParentSpanIdHeader) {
		*z.headersToLog = append(*z.headersToLog, router_http.B3ParentSpanIdHeader)
	}
}

func contains(s []string, e string) bool {
	for _, a := range s {
		if a == e {
			return true
		}
	}
	return false
}
