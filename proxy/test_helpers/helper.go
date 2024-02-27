package test_helpers

import (
	"encoding/json"
	"net/http"
	"time"

	"code.cloudfoundry.org/gorouter/metrics"
	"code.cloudfoundry.org/gorouter/route"
	"code.cloudfoundry.org/gorouter/stats"
)

type NullVarz struct{}

func (NullVarz) MarshalJSON() ([]byte, error)            { return json.Marshal(nil) }
func (NullVarz) ActiveApps() *stats.ActiveApps           { return stats.NewActiveApps() }
func (NullVarz) CaptureBadRequest()                      {}
func (NullVarz) CaptureBadGateway()                      {}
func (NullVarz) CaptureRoutingRequest(b *route.Endpoint) {}
func (NullVarz) CaptureRoutingResponse(int)              {}
func (NullVarz) CaptureRoutingResponseLatency(*route.Endpoint, int, time.Time, time.Duration) {
}
func (NullVarz) CaptureRouteServiceResponse(*http.Response)         {}
func (NullVarz) CaptureRegistryMessage(msg metrics.ComponentTagged) {}
