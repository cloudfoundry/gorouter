package handlers

import (
	"net/http"

	"code.cloudfoundry.org/gorouter/config"
	"code.cloudfoundry.org/gorouter/proxy/utils"
	"github.com/urfave/negroni/v3"
)

type httpRewriteHandler struct {
	responseHeaderRewriters []utils.HeaderRewriter
}

func headerNameValuesToHTTPHeader(headerNameValues []config.HeaderNameValue) http.Header {
	h := http.Header{}
	for _, hv := range headerNameValues {
		h.Add(hv.Name, hv.Value)
	}
	return h
}

func NewHTTPRewriteHandler(cfg config.HTTPRewrite, headersToAlwaysRemove []string) negroni.Handler {
	addHeadersIfNotPresent := headerNameValuesToHTTPHeader(
		cfg.Responses.AddHeadersIfNotPresent,
	)
	headers := cfg.Responses.RemoveHeaders

	for _, header := range headersToAlwaysRemove {
		headers = append(headers, config.HeaderNameValue{Name: header})
	}

	removeHeaders := headerNameValuesToHTTPHeader(
		headers,
	)
	return &httpRewriteHandler{
		responseHeaderRewriters: []utils.HeaderRewriter{
			&utils.RemoveHeaderRewriter{Header: removeHeaders},
			&utils.AddHeaderIfNotPresentRewriter{Header: addHeadersIfNotPresent},
		},
	}
}

func (p *httpRewriteHandler) ServeHTTP(rw http.ResponseWriter, r *http.Request, next http.HandlerFunc) {
	proxyWriter := rw.(utils.ProxyResponseWriter)
	for _, rewriter := range p.responseHeaderRewriters {
		proxyWriter.AddHeaderRewriter(rewriter)
	}
	next(rw, r)
}
