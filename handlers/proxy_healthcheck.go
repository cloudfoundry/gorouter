package handlers

import (
	"net/http"

	"github.com/urfave/negroni/v3"

	"code.cloudfoundry.org/gorouter/common/health"
)

type proxyHealthcheck struct {
	userAgent string
	health    *health.Health
}

// NewHealthcheck creates a handler that responds to healthcheck requests.
// If userAgent is set to a non-empty string, it will use that user agent to
// differentiate between healthcheck requests and non-healthcheck requests.
// Otherwise, it will treat all requests as healthcheck requests.
func NewProxyHealthcheck(userAgent string, health *health.Health) negroni.Handler {
	return &proxyHealthcheck{
		userAgent: userAgent,
		health:    health,
	}
}

func (h *proxyHealthcheck) ServeHTTP(rw http.ResponseWriter, r *http.Request, next http.HandlerFunc) {
	// If reqeust is not intended for healthcheck
	if r.Header.Get("User-Agent") != h.userAgent {
		next(rw, r)
		return
	}

	rw.Header().Set("Cache-Control", "private, max-age=0")
	rw.Header().Set("Expires", "0")

	if h.health.Health() != health.Healthy {
		rw.WriteHeader(http.StatusServiceUnavailable)
		r.Close = true
		return
	}

	rw.WriteHeader(http.StatusOK)
	// #nosec G104 - ignore errors when writing HTTP responses so we don't spam our logs during a DoS
	rw.Write([]byte("ok\n"))
	r.Close = true
}
