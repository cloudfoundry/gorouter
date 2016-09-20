package handlers

import (
	"net/http"
	"sync/atomic"

	"github.com/urfave/negroni"

	"code.cloudfoundry.org/gorouter/access_log/schema"
	"code.cloudfoundry.org/gorouter/proxy/utils"
	"code.cloudfoundry.org/lager"
)

type healthcheck struct {
	userAgent   string
	heartbeatOK *int32
	logger      lager.Logger
}

// NewHealthcheck creates a handler that responds to healthcheck requests.
// If userAgent is set to a non-empty string, it will use that user agent to
// differentiate between healthcheck requests and non-healthcheck requests.
// Otherwise, it will treat all requests as healthcheck requests.
func NewHealthcheck(userAgent string, heartbeatOK *int32, logger lager.Logger) negroni.Handler {
	return &healthcheck{
		userAgent:   userAgent,
		heartbeatOK: heartbeatOK,
		logger:      logger,
	}
}

func (h *healthcheck) ServeHTTP(rw http.ResponseWriter, r *http.Request, next http.HandlerFunc) {
	proxyWriter := rw.(utils.ProxyResponseWriter)
	alr := proxyWriter.Context().Value("AccessLogRecord")
	if alr == nil {
		panic("AccessLogRecord not set on context")
	}
	accessLogRecord := alr.(*schema.AccessLogRecord)
	if h.userAgent == "" || r.Header.Get("User-Agent") == h.userAgent {
		rw.Header().Set("Cache-Control", "private, max-age=0")
		rw.Header().Set("Expires", "0")

		ok := atomic.LoadInt32(h.heartbeatOK) != 0
		if ok {
			rw.WriteHeader(http.StatusOK)
			rw.Write([]byte("ok\n"))
			r.Close = true
			accessLogRecord.StatusCode = http.StatusOK
		} else {
			rw.WriteHeader(http.StatusServiceUnavailable)
			r.Close = true
			accessLogRecord.StatusCode = http.StatusServiceUnavailable
		}
		return
	}
	next(rw, r)
}
