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
	headersToLog  map[string]struct{} // Shared state with proxy for access logs
}

func NewZipkin(enabled bool, headersToLog map[string]struct{}, logger lager.Logger) negroni.Handler {
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

	z.headersToLog[router_http.B3TraceIdHeader] = struct{}{}
	z.headersToLog[router_http.B3SpanIdHeader] = struct{}{}
}
