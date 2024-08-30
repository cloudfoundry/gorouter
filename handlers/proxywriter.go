package handlers

import (
	"log/slog"
	"net/http"

	log "code.cloudfoundry.org/gorouter/logger"
	"code.cloudfoundry.org/gorouter/proxy/utils"
	"github.com/urfave/negroni/v3"
)

type proxyWriterHandler struct {
	logger *slog.Logger
}

// NewProxyWriter creates a handler responsible for setting a proxy
// responseWriter on the request and response
func NewProxyWriter(logger *slog.Logger) negroni.Handler {
	return &proxyWriterHandler{
		logger: logger,
	}
}

// ServeHTTP wraps the responseWriter in a ProxyResponseWriter
func (p *proxyWriterHandler) ServeHTTP(rw http.ResponseWriter, r *http.Request, next http.HandlerFunc) {
	reqInfo, err := ContextRequestInfo(r)
	if err != nil {
		log.Panic(p.logger, "request-info-err", log.ErrAttr(err))
		return
	}
	proxyWriter := utils.NewProxyResponseWriter(rw)
	reqInfo.ProxyResponseWriter = proxyWriter
	next(proxyWriter, r)
}
