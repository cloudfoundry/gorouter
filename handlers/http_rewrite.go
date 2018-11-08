package handlers

import (
	"net/http"

	"github.com/urfave/negroni"

	"code.cloudfoundry.org/gorouter/config"
	"code.cloudfoundry.org/gorouter/proxy/utils"
)

type httpRewriteHandler struct {
	responseHeaderRewriter utils.HeaderRewriter
}

func headerNameValuesToHTTPHeader(headerNameValues []config.HeaderNameValue) http.Header {
	h := http.Header{}
	for _, hv := range headerNameValues {
		h.Add(hv.Name, hv.Value)
	}
	return h
}

func NewHTTPRewriteHandler(cfg config.HTTPRewrite) negroni.Handler {
	addHeadersIfNotPresent := headerNameValuesToHTTPHeader(
		cfg.Responses.AddHeadersIfNotPresent,
	)
	return &httpRewriteHandler{
		responseHeaderRewriter: &utils.AddHeaderIfNotPresentRewriter{Header: addHeadersIfNotPresent},
	}
}

func (p *httpRewriteHandler) ServeHTTP(rw http.ResponseWriter, r *http.Request, next http.HandlerFunc) {
	proxyWriter := rw.(utils.ProxyResponseWriter)
	proxyWriter.AddHeaderRewriter(p.responseHeaderRewriter)
	next(rw, r)
}
