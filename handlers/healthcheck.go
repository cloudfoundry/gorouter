package handlers

import (
	"code.cloudfoundry.org/gorouter/common/threading"
	"net/http"

	"code.cloudfoundry.org/gorouter/logger"
)

type healthcheck struct {
	heartbeatOK *threading.SharedBoolean
	logger      logger.Logger
}

func NewHealthcheck(heartbeatOK *threading.SharedBoolean, logger logger.Logger) http.Handler {
	return &healthcheck{
		heartbeatOK: heartbeatOK,
		logger:      logger,
	}
}

func (h *healthcheck) ServeHTTP(rw http.ResponseWriter, r *http.Request) {

	rw.Header().Set("Cache-Control", "private, max-age=0")
	rw.Header().Set("Expires", "0")

	if !h.heartbeatOK.Get() {
		rw.WriteHeader(http.StatusServiceUnavailable)
		r.Close = true
		return
	}

	rw.WriteHeader(http.StatusOK)
	rw.Write([]byte("ok\n"))
	r.Close = true
}
