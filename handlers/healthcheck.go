package handlers

import (
	"log/slog"
	"net/http"

	"code.cloudfoundry.org/gorouter/common/health"
)

type healthcheck struct {
	health *health.Health
	logger *slog.Logger
}

func NewHealthcheck(health *health.Health, logger *slog.Logger) http.Handler {
	return &healthcheck{
		health: health,
		logger: logger,
	}
}

func (h *healthcheck) ServeHTTP(rw http.ResponseWriter, r *http.Request) {

	rw.Header().Set("Cache-Control", "private, max-age=0")
	rw.Header().Set("Expires", "0")

	if h.health.Health() != health.Healthy {
		rw.WriteHeader(http.StatusServiceUnavailable)
		r.Close = true
		return
	}

	rw.WriteHeader(http.StatusOK)
	rw.Write([]byte("ok\n"))
	r.Close = true
}
