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

func (_ NullVarz) MarshalJSON() ([]byte, error)            { return json.Marshal(nil) }
func (_ NullVarz) ActiveApps() *stats.ActiveApps           { return stats.NewActiveApps() }
func (_ NullVarz) CaptureBadRequest()                      {}
func (_ NullVarz) CaptureBadGateway()                      {}
func (_ NullVarz) CaptureRoutingRequest(b *route.Endpoint) {}
func (_ NullVarz) CaptureRoutingResponse(int)              {}
func (_ NullVarz) CaptureRoutingResponseLatency(*route.Endpoint, int, time.Time, time.Duration) {
}
func (_ NullVarz) CaptureRouteServiceResponse(*http.Response)         {}
func (_ NullVarz) CaptureRegistryMessage(msg metrics.ComponentTagged) {}
