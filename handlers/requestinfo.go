package handlers

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"code.cloudfoundry.org/gorouter/common/uuid"
	"code.cloudfoundry.org/gorouter/logger"
	"code.cloudfoundry.org/gorouter/proxy/utils"
	"code.cloudfoundry.org/gorouter/route"
	gouuid "github.com/nu7hatch/gouuid"
	"github.com/uber-go/zap"

	"github.com/openzipkin/zipkin-go/idgenerator"
	"github.com/openzipkin/zipkin-go/model"
	"github.com/urfave/negroni"
)

type key string

const RequestInfoCtxKey key = "RequestInfo"

type TraceInfo struct {
	TraceID string
	SpanID  string
	UUID    string
}

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
	// there was no successful attempt the RequestFailed flag will be set.
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

	// RoundTripSuccessful will be set once a request has successfully reached a backend instance.
	RoundTripSuccessful bool

	TraceInfo TraceInfo

	BackendReqHeaders http.Header
}

func (r *RequestInfo) ProvideTraceInfo() (TraceInfo, error) {
	if r.TraceInfo != (TraceInfo{}) {
		return r.TraceInfo, nil
	}

	// use UUID as TraceID so that it can be used in VCAP_REQUEST_ID per RFC 4122
	guid, err := uuid.GenerateUUID()
	if err != nil {
		return TraceInfo{}, err
	}

	traceID, spanID, err := generateTraceAndSpanIDFromGUID(guid)
	if err != nil {
		return TraceInfo{}, err
	}

	r.TraceInfo = TraceInfo{
		UUID:    guid,
		TraceID: traceID,
		SpanID:  spanID,
	}

	return r.TraceInfo, nil
}

func (r *RequestInfo) SetTraceInfo(traceID, spanID string) error {
	guid := traceID[0:8] + "-" + traceID[8:12] + "-" + traceID[12:16] + "-" + traceID[16:20] + "-" + traceID[20:]
	_, err := gouuid.ParseHex(guid)
	if err == nil {
		r.TraceInfo = TraceInfo{
			TraceID: traceID,
			SpanID:  spanID,
			UUID:    guid,
		}
		return nil
	}

	guid, err = uuid.GenerateUUID()
	if err != nil {
		return err
	}
	traceID, spanID, err = generateTraceAndSpanIDFromGUID(guid)
	if err != nil {
		return err
	}

	r.TraceInfo = TraceInfo{
		TraceID: traceID,
		SpanID:  spanID,
		UUID:    guid,
	}
	return nil
}

func generateTraceAndSpanIDFromGUID(guid string) (string, string, error) {
	traceHex := strings.Replace(guid, "-", "", -1)
	traceID, err := model.TraceIDFromHex(traceHex)
	if err != nil {
		return "", "", err
	}
	spanID := idgenerator.NewRandom128().SpanID(traceID)
	return traceID.String(), spanID.String(), nil
}

func LoggerWithTraceInfo(l logger.Logger, r *http.Request) logger.Logger {
	reqInfo, err := ContextRequestInfo(r)
	if err != nil {
		return l
	}
	if reqInfo.TraceInfo.TraceID == "" {
		return l
	}

	return l.With(zap.String("trace-id", reqInfo.TraceInfo.TraceID), zap.String("span-id", reqInfo.TraceInfo.SpanID))
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
