package handlers

import (
	"net/http"

	"github.com/urfave/negroni"

	"code.cloudfoundry.org/gorouter/proxy/utils"
)

type injectHeaderHandler struct {
	headerName  string
	headerValue string
}

func NewInjectHeaderHandler(headerName, headerValue string) negroni.Handler {
	return &injectHeaderHandler{
		headerName:  headerName,
		headerValue: headerValue,
	}
}

func (p *injectHeaderHandler) ServeHTTP(rw http.ResponseWriter, r *http.Request, next http.HandlerFunc) {
	proxyWriter := rw.(utils.ProxyResponseWriter)
	proxyWriter.InjectHeader(p.headerName, p.headerValue)
	next(rw, r)
}
