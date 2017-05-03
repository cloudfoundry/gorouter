package handlers

import (
	"net/http"

	"code.cloudfoundry.org/gorouter/logger"
	"github.com/uber-go/zap"
	"github.com/urfave/negroni"

	"code.cloudfoundry.org/gorouter/proxy/utils"
)

type proxyWriterHandler struct {
	logger logger.Logger
}

// NewProxyWriter creates a handler responsible for setting a proxy
// responseWriter on the request and response
func NewProxyWriter(logger logger.Logger) negroni.Handler {
	return &proxyWriterHandler{
		logger: logger,
	}
}

// ServeHTTP wraps the responseWriter in a ProxyResponseWriter
func (p *proxyWriterHandler) ServeHTTP(rw http.ResponseWriter, r *http.Request, next http.HandlerFunc) {
	reqInfo, err := ContextRequestInfo(r)
	if err != nil {
		p.logger.Fatal("request-info-err", zap.Error(err))
		return
	}
	proxyWriter := utils.NewProxyResponseWriter(rw)
	reqInfo.ProxyResponseWriter = proxyWriter
	next(proxyWriter, r)
}
