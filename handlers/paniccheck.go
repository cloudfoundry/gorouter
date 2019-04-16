package handlers

import (
	"fmt"
	"net/http"

	"code.cloudfoundry.org/gorouter/common/threading"

	"code.cloudfoundry.org/gorouter/logger"
	"github.com/uber-go/zap"
	"github.com/urfave/negroni"
)

type panicCheck struct {
	heartbeatOK *threading.SharedBoolean
	logger      logger.Logger
}

// NewPanicCheck creates a handler responsible for checking for panics and setting the Healthcheck to fail.
func NewPanicCheck(healthcheck *threading.SharedBoolean, logger logger.Logger) negroni.Handler {
	return &panicCheck{
		heartbeatOK: healthcheck,
		logger:      logger,
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
				p.logger.Error("panic-check", zap.Error(err))
				p.heartbeatOK.Set(false)
				rw.WriteHeader(http.StatusServiceUnavailable)
				r.Close = true
			}
		}
	}()

	next(rw, r)
}
