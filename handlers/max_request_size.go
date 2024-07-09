package handlers

import (
	"fmt"
	"net/http"

	router_http "code.cloudfoundry.org/gorouter/common/http"
	"code.cloudfoundry.org/gorouter/config"
	"code.cloudfoundry.org/gorouter/logger"
	"github.com/uber-go/zap"
)

type MaxRequestSize struct {
	cfg     *config.Config
	MaxSize int
	logger  logger.Logger
}

const ONE_MB = 1024 * 1024 // bytes * kb

// NewAccessLog creates a new handler that handles logging requests to the
// access log
func NewMaxRequestSize(cfg *config.Config, logger logger.Logger) *MaxRequestSize {
	maxSize := cfg.MaxHeaderBytes

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
		cfg:     cfg,
	}
}

func (m *MaxRequestSize) ServeHTTP(rw http.ResponseWriter, r *http.Request, next http.HandlerFunc) {
	logger := LoggerWithTraceInfo(m.logger, r)
	reqSize := len(r.Method) + len(r.URL.RequestURI()) + len(r.Proto) + 5 // add 5 bytes for space-separation of method, URI, protocol, and /r/n

	for k, v := range r.Header {
		reqSize += len(k) + len(v) + 4 // add two bytes for ": " delimiting, and 2 more for \r\n
	}
	reqSize += len(r.Host) + 4 // add two bytes for ": " delimiting, and 2 more for \r\n

	if reqSize >= m.MaxSize {
		reqInfo, err := ContextRequestInfo(r)
		if err != nil {
			logger.Error("request-info-err", zap.Error(err))
		} else {
			endpointIterator, err := EndpointIteratorForRequest(logger, r, m.cfg.StickySessionCookieNames, m.cfg.StickySessionsForAuthNegotiate, m.cfg.LoadBalanceAZPreference, m.cfg.Zone)
			if err != nil {
				logger.Error("failed-to-find-endpoint-for-req-during-431-short-circuit", zap.Error(err))
			} else {
				reqInfo.RouteEndpoint = endpointIterator.Next(0)
			}
		}
		rw.Header().Set(router_http.CfRouterError, "max-request-size-exceeded")
		rw.WriteHeader(http.StatusRequestHeaderFieldsTooLarge)
		r.Close = true
		return
	}
	next(rw, r)
}
