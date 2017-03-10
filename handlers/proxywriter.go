package handlers

import (
	"context"
	"net/http"

	"github.com/urfave/negroni"

	"code.cloudfoundry.org/gorouter/proxy/utils"
)

type proxyWriterHandler struct{}

// NewProxyWriter creates a handler responsible for setting a proxy
// responseWriter on the request and response
func NewProxyWriter() negroni.Handler {
	return &proxyWriterHandler{}
}

// ServeHTTP wraps the responseWriter in a ProxyResponseWriter
func (p *proxyWriterHandler) ServeHTTP(rw http.ResponseWriter, r *http.Request, next http.HandlerFunc) {
	proxyWriter := utils.NewProxyResponseWriter(rw)
	r = r.WithContext(context.WithValue(r.Context(), ProxyResponseWriterCtxKey, proxyWriter))
	next(proxyWriter, r)
}
