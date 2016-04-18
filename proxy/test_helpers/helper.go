package test_helpers

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/cloudfoundry/gorouter/metrics/reporter"
	"github.com/cloudfoundry/gorouter/route"
	"github.com/cloudfoundry/gorouter/stats"
)

type NullVarz struct{}

func (_ NullVarz) MarshalJSON() ([]byte, error)                                                     { return json.Marshal(nil) }
func (_ NullVarz) ActiveApps() *stats.ActiveApps                                                    { return stats.NewActiveApps() }
func (_ NullVarz) CaptureBadRequest(*http.Request)                                                  {}
func (_ NullVarz) CaptureBadGateway(*http.Request)                                                  {}
func (_ NullVarz) CaptureRoutingRequest(b *route.Endpoint, req *http.Request)                       {}
func (_ NullVarz) CaptureRoutingResponse(*route.Endpoint, *http.Response, time.Time, time.Duration) {}
func (_ NullVarz) CaptureRegistryMessage(msg reporter.ComponentTagged)                              {}
