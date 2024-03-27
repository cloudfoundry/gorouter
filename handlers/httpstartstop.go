package handlers

import (
	"net/http"
	"time"

	"code.cloudfoundry.org/gorouter/logger"
	"code.cloudfoundry.org/gorouter/proxy/utils"
	"github.com/cloudfoundry/dropsonde"
	"github.com/cloudfoundry/dropsonde/emitter"
	"github.com/cloudfoundry/dropsonde/factories"
	"github.com/cloudfoundry/sonde-go/events"
	uuid "github.com/nu7hatch/gouuid"
	"github.com/uber-go/zap"
	"github.com/urfave/negroni/v3"
	"google.golang.org/protobuf/proto"
)

type httpStartStopHandler struct {
	emitter dropsonde.EventEmitter
	logger  logger.Logger
}

// NewHTTPStartStop creates a new handler that handles emitting frontend
// HTTP StartStop events
func NewHTTPStartStop(emitter dropsonde.EventEmitter, logger logger.Logger) negroni.Handler {
	return &httpStartStopHandler{
		emitter: emitter,
		logger:  logger,
	}
}

// ServeHTTP handles emitting a StartStop event after the request has been completed
func (hh *httpStartStopHandler) ServeHTTP(rw http.ResponseWriter, r *http.Request, next http.HandlerFunc) {
	logger := LoggerWithTraceInfo(hh.logger, r)

	requestID, err := uuid.ParseHex(r.Header.Get(VcapRequestIdHeader))
	if err != nil {
		logger.Panic("start-stop-handler-err", zap.String("error", "X-Vcap-Request-Id not found"))
		return
	}
	prw, ok := rw.(utils.ProxyResponseWriter)
	if !ok {
		logger.Panic("request-info-err", zap.String("error", "ProxyResponseWriter not found"))
		return
	}

	// Remove these headers if pre-set so they aren't logged in the event.
	// ProxyRoundTripper will set them to correct values later
	r.Header.Del("X-CF-ApplicationID")
	r.Header.Del("X-CF-InstanceIndex")
	r.Header.Del("X-CF-InstanceID")

	startTime := time.Now()

	next(rw, r)

	startStopEvent := factories.NewHttpStartStop(r, int32(prw.Status()), int64(prw.Size()), events.PeerType_Server, requestID)
	startStopEvent.StartTimestamp = proto.Int64(startTime.UnixNano())

	envelope, err := emitter.Wrap(startStopEvent, hh.emitter.Origin())
	if err != nil {
		logger.Info("failed-to-create-startstop-envelope", zap.Error(err))
		return
	}

	endpoint, _ := GetEndpoint(r.Context())
	if endpoint != nil {
		envelope.Tags = endpoint.Tags
	}

	err = hh.emitter.EmitEnvelope(envelope)
	if err != nil {
		logger.Info("failed-to-emit-startstop-event", zap.Error(err))
	}
}
