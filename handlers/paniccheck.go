package handlers

import (
	"net/http"
	"sync/atomic"

	"code.cloudfoundry.org/gorouter/logger"
	"github.com/uber-go/zap"
	"github.com/urfave/negroni"
)

type panicCheck struct {
	heartbeatOK *int32
	logger      logger.Logger
}

// NewPanicCheck creates a handler responsible for checking for panics and setting the Healthcheck to fail.
func NewPanicCheck(healthcheck *int32, logger logger.Logger) negroni.Handler {
	return &panicCheck{
		heartbeatOK: healthcheck,
		logger:      logger,
	}
}

func (p *panicCheck) ServeHTTP(rw http.ResponseWriter, r *http.Request, next http.HandlerFunc) {
	defer func() {
		if err := recover(); err != nil {
			switch err {
			case http.ErrAbortHandler:
				// The ErrAbortHandler panic occurs when a client goes away in the middle of a request
				// this is a panic we expect to see in normal operation and is safe to ignore
			default:
				p.logger.Error("panic-check", zap.Object("panic", err))
				atomic.StoreInt32(p.heartbeatOK, 0)
				rw.WriteHeader(http.StatusServiceUnavailable)
				r.Close = true
			}
		}
	}()

	next(rw, r)
}
