package handlers

import (
	"net/http"

	"github.com/urfave/negroni"

	"code.cloudfoundry.org/gorouter/logger"

	router_http "code.cloudfoundry.org/gorouter/common/http"
)

type Zipkin struct {
	zipkinEnabled bool
	logger        logger.Logger
	headersToLog  []string // Shared state with proxy for access logs
}

var _ negroni.Handler = new(Zipkin)

func NewZipkin(enabled bool, headersToLog []string, logger logger.Logger) *Zipkin {
	return &Zipkin{
		zipkinEnabled: enabled,
		headersToLog:  headersToLog,
		logger:        logger,
	}
}

func (z *Zipkin) ServeHTTP(rw http.ResponseWriter, r *http.Request, next http.HandlerFunc) {
	defer next(rw, r)
	if !z.zipkinEnabled {
		return
	}
	router_http.SetB3Headers(r, z.logger)
}

func (z *Zipkin) HeadersToLog() []string {
	if !z.zipkinEnabled {
		return z.headersToLog
	}
	headersToLog := z.headersToLog
	if !contains(headersToLog, router_http.B3TraceIdHeader) {
		headersToLog = append(headersToLog, router_http.B3TraceIdHeader)
	}

	if !contains(headersToLog, router_http.B3SpanIdHeader) {
		headersToLog = append(headersToLog, router_http.B3SpanIdHeader)
	}

	if !contains(headersToLog, router_http.B3ParentSpanIdHeader) {
		headersToLog = append(headersToLog, router_http.B3ParentSpanIdHeader)
	}
	return headersToLog
}

func contains(s []string, e string) bool {
	for _, a := range s {
		if a == e {
			return true
		}
	}
	return false
}
