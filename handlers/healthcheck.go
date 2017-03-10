package handlers

import (
	"net/http"
	"sync/atomic"

	"code.cloudfoundry.org/gorouter/logger"
)

type healthcheck struct {
	heartbeatOK *int32
	logger      logger.Logger
}

func NewHealthcheck(heartbeatOK *int32, logger logger.Logger) http.Handler {
	return &healthcheck{
		heartbeatOK: heartbeatOK,
		logger:      logger,
	}
}

func (h *healthcheck) ServeHTTP(rw http.ResponseWriter, r *http.Request) {

	rw.Header().Set("Cache-Control", "private, max-age=0")
	rw.Header().Set("Expires", "0")

	draining := atomic.LoadInt32(h.heartbeatOK) == 0
	if draining {
		rw.WriteHeader(http.StatusServiceUnavailable)
		r.Close = true
		return
	}

	rw.WriteHeader(http.StatusOK)
	rw.Write([]byte("ok\n"))
	r.Close = true
}
