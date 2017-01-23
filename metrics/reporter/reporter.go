package reporter

import (
	"net/http"
	"time"

	"code.cloudfoundry.org/gorouter/route"
)

//go:generate counterfeiter -o fakes/fake_reporter.go . ProxyReporter
type ProxyReporter interface {
	CaptureBadRequest()
	CaptureBadGateway()
	CaptureRoutingRequest(b *route.Endpoint)
	CaptureRoutingResponse(b *route.Endpoint, res *http.Response, t time.Time, d time.Duration)
	CaptureRouteServiceResponse(res *http.Response)
}

type ComponentTagged interface {
	Component() string
}

//go:generate counterfeiter -o fakes/fake_registry_reporter.go . RouteRegistryReporter
type RouteRegistryReporter interface {
	CaptureRouteStats(totalRoutes int, msSinceLastUpdate uint64)
	CaptureLookupTime(t time.Duration)
	CaptureRegistryMessage(msg ComponentTagged)
}
