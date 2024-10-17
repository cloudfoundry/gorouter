package handlers

import (
	"fmt"
	"log/slog"
	"net/http"

	router_http "code.cloudfoundry.org/gorouter/common/http"
	"code.cloudfoundry.org/gorouter/config"
	log "code.cloudfoundry.org/gorouter/logger"
)

type MaxRequestSize struct {
	cfg     *config.Config
	MaxSize int
	logger  *slog.Logger
}

const ONE_MB = 1024 * 1024 // bytes * kb

// NewAccessLog creates a new handler that handles logging requests to the
// access log
func NewMaxRequestSize(cfg *config.Config, logger *slog.Logger) *MaxRequestSize {
	maxSize := cfg.MaxHeaderBytes

	if maxSize < 1 {
		maxSize = ONE_MB
	}

	if maxSize > ONE_MB {
		logger.Warn("innefectual-max-header-bytes-value", slog.String("error", fmt.Sprintf("Values over %d are limited by http.Server", maxSize)))
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
		valueLen := 0
		for _, value := range r.Header.Values(k) {
			valueLen += len(value)
		}
		reqSize += len(k) + valueLen + 4 + len(v) - 1 // add padding for ': ' and newlines and comma delimiting of multiple values
	}

	reqSize += len(r.Host) + 8 // add padding for "Host: " and newlines

	if reqSize >= m.MaxSize {
		reqInfo, err := ContextRequestInfo(r)
		if err != nil {
			logger.Error("request-info-err", log.ErrAttr(err))
		} else {
			endpointIterator, err := EndpointIteratorForRequest(logger, r, m.cfg.StickySessionCookieNames, m.cfg.StickySessionsForAuthNegotiate, m.cfg.LoadBalanceAZPreference, m.cfg.Zone)
			if err != nil {
				logger.Error("failed-to-find-endpoint-for-req-during-431-short-circuit", log.ErrAttr(err))
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
