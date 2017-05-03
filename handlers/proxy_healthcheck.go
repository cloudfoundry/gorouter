package handlers

import (
	"net/http"
	"sync/atomic"

	"code.cloudfoundry.org/gorouter/logger"
	"github.com/urfave/negroni"
)

type proxyHealthcheck struct {
	userAgent   string
	heartbeatOK *int32
	logger      logger.Logger
}

// NewHealthcheck creates a handler that responds to healthcheck requests.
// If userAgent is set to a non-empty string, it will use that user agent to
// differentiate between healthcheck requests and non-healthcheck requests.
// Otherwise, it will treat all requests as healthcheck requests.
func NewProxyHealthcheck(userAgent string, heartbeatOK *int32, logger logger.Logger) negroni.Handler {
	return &proxyHealthcheck{
		userAgent:   userAgent,
		heartbeatOK: heartbeatOK,
		logger:      logger,
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
