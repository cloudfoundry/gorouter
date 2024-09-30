package handlers

import (
	"fmt"
	"log/slog"
	"net/http"
	"runtime"

	"github.com/urfave/negroni/v3"

	"code.cloudfoundry.org/gorouter/common/health"
	router_http "code.cloudfoundry.org/gorouter/common/http"
	log "code.cloudfoundry.org/gorouter/logger"
)

type panicCheck struct {
	health *health.Health
	logger *slog.Logger
}

// NewPanicCheck creates a handler responsible for checking for panics and setting the Healthcheck to fail.
func NewPanicCheck(health *health.Health, logger *slog.Logger) negroni.Handler {
	return &panicCheck{
		health: health,
		logger: logger,
	}
}

func (p *panicCheck) ServeHTTP(rw http.ResponseWriter, r *http.Request, next http.HandlerFunc) {
	defer func() {
		if rec := recover(); rec != nil {
			switch rec {
			case http.ErrAbortHandler:
				// The ErrAbortHandler panic occurs when a client goes away in the middle of a request
				// this is a panic we expect to see in normal operation and is safe to allow the panic
				// http.Server will handle it appropriately
				panic(http.ErrAbortHandler)
			default:
				err, ok := rec.(error)
				if !ok {
					err = fmt.Errorf("%v", rec)
				}
				logger := LoggerWithTraceInfo(p.logger, r)
				logger.Error("panic-check", slog.String("host", r.Host), log.ErrAttr(err), slog.Any("stacktrace", runtime.StartTrace()))

				rw.Header().Set(router_http.CfRouterError, "unknown_failure")
				rw.WriteHeader(http.StatusBadGateway)
				r.Close = true
			}
		}
	}()

	next(rw, r)
}
