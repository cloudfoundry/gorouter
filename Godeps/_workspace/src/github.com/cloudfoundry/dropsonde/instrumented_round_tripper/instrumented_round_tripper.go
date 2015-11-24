package instrumented_round_tripper

import (
	"log"
	"net/http"
	"time"

	"github.com/cloudfoundry/dropsonde/emitter"
	"github.com/cloudfoundry/dropsonde/factories"
	"github.com/cloudfoundry/sonde-go/events"
	"github.com/gogo/protobuf/proto"
	uuid "github.com/nu7hatch/gouuid"
)

type instrumentedRoundTripper struct {
	roundTripper http.RoundTripper
	emitter      emitter.EventEmitter
}

type instrumentedCancelableRoundTripper struct {
	instrumentedRoundTripper *instrumentedRoundTripper
}

/*
Helper for creating an InstrumentedRoundTripper which will delegate to the given RoundTripper
*/
func InstrumentedRoundTripper(roundTripper http.RoundTripper, emitter emitter.EventEmitter) http.RoundTripper {
	irt := &instrumentedRoundTripper{roundTripper, emitter}

	_, ok := roundTripper.(canceler)
	if ok {
		return &instrumentedCancelableRoundTripper{
			instrumentedRoundTripper: irt,
		}
	}

	return irt
}

/*
Wraps the RoundTrip function of the given RoundTripper.
Will provide accounting metrics for the http.Request / http.Response life-cycle
Callers of RoundTrip are responsible for setting the ‘X-CF-RequestID’ field in the request header if they have one.
Callers are also responsible for setting the ‘X-CF-ApplicationID’ and ‘X-CF-InstanceIndex’ fields in the request header if they are known.
*/
func (irt *instrumentedRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	requestId, err := GenerateUuid()
	if err != nil {
		log.Printf("failed to generated request ID: %v\n", err)
		requestId = &uuid.UUID{}
	}

	startTime := time.Now()
	parentRequestId := req.Header.Get("X-CF-RequestID")
	req.Header.Set("X-CF-RequestID", requestId.String())

	resp, roundTripErr := irt.roundTripper.RoundTrip(req)

	var statusCode int
	var contentLength int64
	if roundTripErr == nil {
		statusCode = resp.StatusCode
		contentLength = resp.ContentLength
	}

	httpStartStop := factories.NewHttpStartStop(req, statusCode, contentLength, events.PeerType_Client, requestId)
	if parentRequestId != "" {
		if id, err := uuid.ParseHex(parentRequestId); err == nil {
			httpStartStop.ParentRequestId = factories.NewUUID(id)
		}
	}
	httpStartStop.StartTimestamp = proto.Int64(startTime.UnixNano())

	err = irt.emitter.Emit(httpStartStop)
	if err != nil {
		log.Printf("failed to emit startstop event: %v\n", err)
	}

	return resp, roundTripErr
}

func (icrt *instrumentedCancelableRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	return icrt.instrumentedRoundTripper.RoundTrip(req)
}

func (icrt *instrumentedCancelableRoundTripper) CancelRequest(req *http.Request) {
	cancelableTransport := icrt.instrumentedRoundTripper.roundTripper.(canceler)
	cancelableTransport.CancelRequest(req)
}

var GenerateUuid = uuid.NewV4

type canceler interface {
	CancelRequest(*http.Request)
}
