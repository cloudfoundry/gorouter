package handlers

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"time"

	"code.cloudfoundry.org/gorouter/proxy/utils"
	"code.cloudfoundry.org/gorouter/route"

	"github.com/urfave/negroni"
)

type key string

const requestInfoCtxKey key = "RequestInfo"

// RequestInfo stores all metadata about the request and is used to pass
// informaton between handlers
type RequestInfo struct {
	StartedAt, StoppedAt   time.Time
	RoutePool              *route.Pool
	RouteEndpoint          *route.Endpoint
	ProxyResponseWriter    utils.ProxyResponseWriter
	RouteServiceURL        *url.URL
	IsInternalRouteService bool
}

// ContextRequestInfo gets the RequestInfo from the request Context
func ContextRequestInfo(req *http.Request) (*RequestInfo, error) {
	ri := req.Context().Value(requestInfoCtxKey)
	if ri == nil {
		return nil, errors.New("RequestInfo not set on context")
	}
	reqInfo, ok := ri.(*RequestInfo)
	if !ok {
		return nil, errors.New("RequestInfo is not the correct type")
	}
	return reqInfo, nil
}

// RequestInfoHandler adds a RequestInfo to the context of all requests that go
// through this handler
type RequestInfoHandler struct{}

// NewRequestInfo creates a RequestInfoHandler
func NewRequestInfo() negroni.Handler {
	return &RequestInfoHandler{}
}

func (r *RequestInfoHandler) ServeHTTP(w http.ResponseWriter, req *http.Request, next http.HandlerFunc) {
	reqInfo := new(RequestInfo)
	req = req.WithContext(context.WithValue(req.Context(), requestInfoCtxKey, reqInfo))
	reqInfo.StartedAt = time.Now()
	next(w, req)
}
