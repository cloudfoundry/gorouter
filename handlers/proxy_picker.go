package handlers

import (
	"net/http"
	"net/http/httputil"
	"strings"

	"github.com/urfave/negroni/v3"
)

type proxyPicker struct {
	directorProxy           *httputil.ReverseProxy
	expect100ContinueRProxy *httputil.ReverseProxy
}

// Creates a per-request decision on which reverse proxy to use, based on whether
// a request contained an `Expect: 100-continue` header
func NewProxyPicker(directorProxy *httputil.ReverseProxy, expect100ContinueRProxy *httputil.ReverseProxy) negroni.Handler {
	return &proxyPicker{
		directorProxy:           directorProxy,
		expect100ContinueRProxy: expect100ContinueRProxy,
	}
}

func (pp *proxyPicker) ServeHTTP(rw http.ResponseWriter, r *http.Request, next http.HandlerFunc) {
	pickedProxy := pp.directorProxy
	if strings.ToLower(r.Header.Get("Expect")) == "100-continue" {
		pickedProxy = pp.expect100ContinueRProxy
	}

	pickedProxy.ServeHTTP(rw, r)
	next(rw, r)
}
