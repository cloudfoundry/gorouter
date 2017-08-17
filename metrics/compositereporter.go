package metrics

import (
	"net/http"
	"time"

	"code.cloudfoundry.org/gorouter/route"
)

// Deprecated: this interface is marked for removal. It should be removed upon
// removal of Varz
//go:generate counterfeiter -o fakes/fake_varzreporter.go . VarzReporter
type VarzReporter interface {
	CaptureBadRequest()
	CaptureBadGateway()
	CaptureRoutingRequest(b *route.Endpoint)
	CaptureRoutingResponseLatency(b *route.Endpoint, statusCode int, t time.Time, d time.Duration)
}

//go:generate counterfeiter -o fakes/fake_proxyreporter.go . ProxyReporter
type ProxyReporter interface {
	CaptureBackendExhaustedConns()
	CaptureBadRequest()
	CaptureBadGateway()
	CaptureRoutingRequest(b *route.Endpoint)
	CaptureRoutingResponse(statusCode int)
	CaptureRoutingResponseLatency(b *route.Endpoint, d time.Duration)
	CaptureRouteServiceResponse(res *http.Response)
	CaptureWebSocketUpdate()
	CaptureWebSocketFailure()
}

type ComponentTagged interface {
	Component() string
}

//go:generate counterfeiter -o fakes/fake_registry_reporter.go . RouteRegistryReporter
type RouteRegistryReporter interface {
	CaptureRouteStats(totalRoutes int, msSinceLastUpdate uint64)
	CaptureLookupTime(t time.Duration)
	CaptureRegistryMessage(msg ComponentTagged)
	CaptureUnregistryMessage(msg ComponentTagged)
}

//go:generate counterfeiter -o fakes/fake_combinedreporter.go . CombinedReporter
type CombinedReporter interface {
	CaptureBackendExhaustedConns()
	CaptureBadRequest()
	CaptureBadGateway()
	CaptureRoutingRequest(b *route.Endpoint)
	CaptureRoutingResponse(statusCode int)
	CaptureRoutingResponseLatency(b *route.Endpoint, statusCode int, t time.Time, d time.Duration)
	CaptureRouteServiceResponse(res *http.Response)
	CaptureWebSocketUpdate()
	CaptureWebSocketFailure()
}

type CompositeReporter struct {
	varzReporter  VarzReporter
	proxyReporter ProxyReporter
}

func NewCompositeReporter(varzReporter VarzReporter, proxyReporter ProxyReporter) CombinedReporter {
	return &CompositeReporter{
		varzReporter:  varzReporter,
		proxyReporter: proxyReporter,
	}
}

func (c *CompositeReporter) CaptureBackendExhaustedConns() {
	c.proxyReporter.CaptureBackendExhaustedConns()
}

func (c *CompositeReporter) CaptureBadRequest() {
	c.varzReporter.CaptureBadRequest()
	c.proxyReporter.CaptureBadRequest()
}

func (c *CompositeReporter) CaptureBadGateway() {
	c.varzReporter.CaptureBadGateway()
	c.proxyReporter.CaptureBadGateway()
}

func (c *CompositeReporter) CaptureRoutingRequest(b *route.Endpoint) {
	c.varzReporter.CaptureRoutingRequest(b)
	c.proxyReporter.CaptureRoutingRequest(b)
}

func (c *CompositeReporter) CaptureRouteServiceResponse(res *http.Response) {
	c.proxyReporter.CaptureRouteServiceResponse(res)
}

func (c *CompositeReporter) CaptureRoutingResponse(statusCode int) {
	c.proxyReporter.CaptureRoutingResponse(statusCode)
}

func (c *CompositeReporter) CaptureRoutingResponseLatency(b *route.Endpoint, statusCode int, t time.Time, d time.Duration) {
	c.varzReporter.CaptureRoutingResponseLatency(b, statusCode, t, d)
	c.proxyReporter.CaptureRoutingResponseLatency(b, d)
}

func (c *CompositeReporter) CaptureWebSocketUpdate() {
	c.proxyReporter.CaptureWebSocketUpdate()
}

func (c *CompositeReporter) CaptureWebSocketFailure() {
	c.proxyReporter.CaptureWebSocketFailure()
}
