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

const RequestInfoCtxKey key = "RequestInfo"

// RequestInfo stores all metadata about the request and is used to pass
// informaton between handlers
type RequestInfo struct {
	StartedAt, StoppedAt                      time.Time
	AppRequestStartedAt, AppRequestFinishedAt time.Time
	RoutePool                                 *route.EndpointPool
	RouteEndpoint                             *route.Endpoint
	ProxyResponseWriter                       utils.ProxyResponseWriter
	RouteServiceURL                           *url.URL
	ShouldRouteToInternalRouteService         bool

	BackendReqHeaders http.Header
}

// ContextRequestInfo gets the RequestInfo from the request Context
func ContextRequestInfo(req *http.Request) (*RequestInfo, error) {
	return getRequestInfo(req.Context())
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
	req = req.WithContext(context.WithValue(req.Context(), RequestInfoCtxKey, reqInfo))
	reqInfo.StartedAt = time.Now()
	next(w, req)
}

func GetEndpoint(ctx context.Context) (*route.Endpoint, error) {
	reqInfo, err := getRequestInfo(ctx)
	if err != nil {
		return nil, err
	}
	ep := reqInfo.RouteEndpoint
	if ep == nil {
		return nil, errors.New("route endpoint not set on request info")
	}
	return ep, nil
}

func getRequestInfo(ctx context.Context) (*RequestInfo, error) {
	ri := ctx.Value(RequestInfoCtxKey)
	if ri == nil {
		return nil, errors.New("RequestInfo not set on context")
	}
	reqInfo, ok := ri.(*RequestInfo)
	if !ok {
		return nil, errors.New("RequestInfo is not the correct type") // untested
	}
	return reqInfo, nil
}
