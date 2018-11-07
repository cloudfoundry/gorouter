package handlers

import (
	"net/http"

	"github.com/urfave/negroni"

	"code.cloudfoundry.org/gorouter/config"
	"code.cloudfoundry.org/gorouter/proxy/utils"
)

type httpRewriteHandler struct {
	headerRewriter utils.HeaderRewriter
}

func NewHTTPRewriteHandler(cfg config.HTTPRewrite) negroni.Handler {
	headersToInject := http.Header{}
	for _, hv := range cfg.InjectResponseHeaders {
		headersToInject.Add(hv.Name, hv.Value)
	}
	return &httpRewriteHandler{
		headerRewriter: &utils.InjectHeaderRewriter{Header: headersToInject},
	}
}

func (p *httpRewriteHandler) ServeHTTP(rw http.ResponseWriter, r *http.Request, next http.HandlerFunc) {
	proxyWriter := rw.(utils.ProxyResponseWriter)
	proxyWriter.AddHeaderRewriter(p.headerRewriter)
	next(rw, r)
}
