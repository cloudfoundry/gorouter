package metrics

import (
	"net/http"
	"time"

	"code.cloudfoundry.org/gorouter/route"
)

// Deprecated: this interface is marked for removal. It should be removed upon
// removal of Varz
//
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
	CaptureBackendInvalidID()
	CaptureBackendInvalidTLSCert()
	CaptureBackendTLSHandshakeFailed()
	CaptureBadRequest()
	CaptureBadGateway()
	CaptureEmptyContentLengthHeader()
	CaptureRoutingRequest(b *route.Endpoint)
	CaptureRoutingResponse(statusCode int)
	CaptureRoutingResponseLatency(b *route.Endpoint, statusCode int, t time.Time, d time.Duration)
	CaptureRouteServiceResponse(res *http.Response)
	CaptureWebSocketUpdate()
	CaptureWebSocketFailure()
}

type ComponentTagged interface {
	Component() string
}

//go:generate counterfeiter -o fakes/fake_registry_reporter.go . RouteRegistryReporter
type RouteRegistryReporter interface {
	CaptureRouteStats(totalRoutes int, msSinceLastUpdate int64)
	CaptureRoutesPruned(prunedRoutes uint64)
	CaptureLookupTime(t time.Duration)
	CaptureRegistryMessage(msg ComponentTagged, action string)
	CaptureRouteRegistrationLatency(t time.Duration)
	CaptureUnregistryMessage(msg ComponentTagged)
	UnmuzzleRouteRegistrationLatency()
}

//go:generate counterfeiter -o fakes/fake_monitorreporter.go . MonitorReporter
type MonitorReporter interface {
	CaptureFoundFileDescriptors(files int)
	CaptureNATSBufferedMessages(messages int)
	CaptureNATSDroppedMessages(messages int)
}

type CompositeReporter struct {
	VarzReporter
	ProxyReporter
}

type MultiRouteRegistryReporter []RouteRegistryReporter

var _ RouteRegistryReporter = MultiRouteRegistryReporter{}

func (m MultiRouteRegistryReporter) CaptureLookupTime(t time.Duration) {
	for _, r := range m {
		r.CaptureLookupTime(t)
	}
}

func (m MultiRouteRegistryReporter) UnmuzzleRouteRegistrationLatency() {
	for _, r := range m {
		r.UnmuzzleRouteRegistrationLatency()
	}
}

func (m MultiRouteRegistryReporter) CaptureRouteRegistrationLatency(t time.Duration) {
	for _, r := range m {
		r.CaptureRouteRegistrationLatency(t)
	}
}

func (m MultiRouteRegistryReporter) CaptureRouteStats(totalRoutes int, msSinceLastUpdate int64) {
	for _, r := range m {
		r.CaptureRouteStats(totalRoutes, msSinceLastUpdate)
	}
}

func (m MultiRouteRegistryReporter) CaptureRoutesPruned(routesPruned uint64) {
	for _, r := range m {
		r.CaptureRoutesPruned(routesPruned)
	}
}

func (m MultiRouteRegistryReporter) CaptureRegistryMessage(msg ComponentTagged, action string) {
	for _, r := range m {
		r.CaptureRegistryMessage(msg, action)
	}
}

func (m MultiRouteRegistryReporter) CaptureUnregistryMessage(msg ComponentTagged) {
	for _, r := range m {
		r.CaptureUnregistryMessage(msg)
	}
}

type MultiProxyReporter []ProxyReporter

var _ ProxyReporter = MultiProxyReporter{}

func (m MultiProxyReporter) CaptureBackendExhaustedConns() {
	for _, r := range m {
		r.CaptureBackendExhaustedConns()
	}
}

func (m MultiProxyReporter) CaptureBackendTLSHandshakeFailed() {
	for _, r := range m {
		r.CaptureBackendTLSHandshakeFailed()
	}
}

func (m MultiProxyReporter) CaptureBackendInvalidID() {
	for _, r := range m {
		r.CaptureBackendInvalidID()
	}
}

func (m MultiProxyReporter) CaptureBackendInvalidTLSCert() {
	for _, r := range m {
		r.CaptureBackendInvalidTLSCert()
	}
}

func (m MultiProxyReporter) CaptureBadRequest() {
	for _, r := range m {
		r.CaptureBadRequest()
	}
}

func (m MultiProxyReporter) CaptureBadGateway() {
	for _, r := range m {
		r.CaptureBadGateway()
	}
}

func (m MultiProxyReporter) CaptureEmptyContentLengthHeader() {
	for _, r := range m {
		r.CaptureEmptyContentLengthHeader()
	}
}

func (m MultiProxyReporter) CaptureRoutingRequest(b *route.Endpoint) {
	for _, r := range m {
		r.CaptureRoutingRequest(b)
	}
}

func (m MultiProxyReporter) CaptureRouteServiceResponse(res *http.Response) {
	for _, r := range m {
		r.CaptureRouteServiceResponse(res)
	}
}

func (m MultiProxyReporter) CaptureRoutingResponse(statusCode int) {
	for _, r := range m {
		r.CaptureRoutingResponse(statusCode)
	}
}

func (m MultiProxyReporter) CaptureRoutingResponseLatency(b *route.Endpoint, statusCode int, t time.Time, d time.Duration) {
	for _, r := range m {
		r.CaptureRoutingResponseLatency(b, statusCode, t, d)
	}
}

func (m MultiProxyReporter) CaptureWebSocketUpdate() {
	for _, r := range m {
		r.CaptureWebSocketUpdate()
	}
}

func (m MultiProxyReporter) CaptureWebSocketFailure() {
	for _, r := range m {
		r.CaptureWebSocketFailure()
	}
}

type MultiMonitorReporter []MonitorReporter

var _ MonitorReporter = MultiMonitorReporter{}

func (m MultiMonitorReporter) CaptureFoundFileDescriptors(files int) {
	for _, r := range m {
		r.CaptureFoundFileDescriptors(files)
	}
}

func (m MultiMonitorReporter) CaptureNATSBufferedMessages(messages int) {
	for _, r := range m {
		r.CaptureNATSBufferedMessages(messages)
	}
}

func (m MultiMonitorReporter) CaptureNATSDroppedMessages(messages int) {
	for _, r := range m {
		r.CaptureNATSDroppedMessages(messages)
	}
}

func (c *CompositeReporter) CaptureBadRequest() {
	c.VarzReporter.CaptureBadRequest()
	c.ProxyReporter.CaptureBadRequest()
}

func (c *CompositeReporter) CaptureBadGateway() {
	c.VarzReporter.CaptureBadGateway()
	c.ProxyReporter.CaptureBadGateway()
}

func (c *CompositeReporter) CaptureEmptyContentLengthHeader() {
	c.ProxyReporter.CaptureEmptyContentLengthHeader()
}

func (c *CompositeReporter) CaptureRoutingRequest(b *route.Endpoint) {
	c.VarzReporter.CaptureRoutingRequest(b)
	c.ProxyReporter.CaptureRoutingRequest(b)
}

func (c *CompositeReporter) CaptureRoutingResponseLatency(b *route.Endpoint, statusCode int, t time.Time, d time.Duration) {
	c.VarzReporter.CaptureRoutingResponseLatency(b, statusCode, t, d)
	c.ProxyReporter.CaptureRoutingResponseLatency(b, 0, time.Time{}, d)
}
