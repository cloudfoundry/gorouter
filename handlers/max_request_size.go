package handlers

import (
	"fmt"
	"net/http"

	router_http "code.cloudfoundry.org/gorouter/common/http"
	"code.cloudfoundry.org/gorouter/logger"
	"github.com/uber-go/zap"
)

type MaxRequestSize struct {
	MaxSize int
	logger  logger.Logger
}

const ONE_MB = 1024 * 1024 // bytes * kb

// NewAccessLog creates a new handler that handles logging requests to the
// access log
func NewMaxRequestSize(maxSize int, logger logger.Logger) *MaxRequestSize {
	if maxSize < 1 {
		maxSize = ONE_MB
	}

	if maxSize > ONE_MB {
		logger.Warn("innefectual-max-header-bytes-value", zap.String("error", fmt.Sprintf("Values over %d are limited by http.Server", maxSize)))
		maxSize = ONE_MB
	}

	return &MaxRequestSize{
		MaxSize: maxSize,
		logger:  logger,
	}
}

func (m *MaxRequestSize) ServeHTTP(rw http.ResponseWriter, r *http.Request, next http.HandlerFunc) {
	reqSize := len(r.Method) + len(r.URL.RequestURI()) + len(r.Proto) + 5 // add 5 bytes for space-separation of method, URI, protocol, and /r/n

	for k, v := range r.Header {
		reqSize += len(k) + len(v) + 4 // add two bytes for ": " delimiting, and 2 more for \r\n
	}
	reqSize += len(r.Host) + 4 // add two bytes for ": " delimiting, and 2 more for \r\n

	if reqSize >= m.MaxSize {
		rw.Header().Set(router_http.CfRouterError, "max-request-size-exceeded")
		rw.WriteHeader(http.StatusRequestHeaderFieldsTooLarge)
		r.Close = true
		return
	}
	next(rw, r)
}