package handlers

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"time"

	"code.cloudfoundry.org/gorouter/proxy/utils"
	"code.cloudfoundry.org/gorouter/route"

	"github.com/openzipkin/zipkin-go/idgenerator"
	"github.com/urfave/negroni"
)

type key string

const RequestInfoCtxKey key = "RequestInfo"

// RequestInfo stores all metadata about the request and is used to pass
// information between handlers. The timing information is ordered by time of
// occurrence.
type RequestInfo struct {
	// ReceivedAt records the time at which this request was received by
	// gorouter as recorded in the RequestInfo middleware.
	ReceivedAt time.Time
	// AppRequestStartedAt records the time at which gorouter starts sending
	// the request to the backend.
	AppRequestStartedAt time.Time
	// LastFailedAttemptFinishedAt is the end of the last failed request,
	// if any. If there was at least one failed attempt this will be set, if
	// there was no successful attempt this value will equal
	// AppRequestFinishedAt.
	LastFailedAttemptFinishedAt time.Time

	// These times document at which timestamps the individual phases of the
	// request started / finished if there was a successful attempt.
	DnsStartedAt           time.Time
	DnsFinishedAt          time.Time
	DialStartedAt          time.Time
	DialFinishedAt         time.Time
	TlsHandshakeStartedAt  time.Time
	TlsHandshakeFinishedAt time.Time

	// AppRequestFinishedAt records the time at which either a response was
	// received or the last performed attempt failed and no further attempts
	// could be made.
	AppRequestFinishedAt time.Time

	// FinishedAt is recorded once the access log middleware is executed after
	// performing the request, in contrast to the ReceivedAt value which is
	// recorded before the access log, but we need the value to be able to
	// produce the log.
	FinishedAt time.Time

	RoutePool                         *route.EndpointPool
	RouteEndpoint                     *route.Endpoint
	ProxyResponseWriter               utils.ProxyResponseWriter
	RouteServiceURL                   *url.URL
	ShouldRouteToInternalRouteService bool
	FailedAttempts                    int

	TraceID string
	SpanID  string

	BackendReqHeaders http.Header
}

func (r *RequestInfo) ProvideTraceInfo() (string, string) {
	if r.TraceID != "" && r.SpanID != "" {
		return r.TraceID, r.SpanID
	}
	trace := idgenerator.NewRandom128().TraceID()
	r.TraceID = trace.String()
	r.SpanID = idgenerator.NewRandom128().SpanID(trace).String()
	return r.TraceID, r.SpanID
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
	reqInfo.ReceivedAt = time.Now()
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
