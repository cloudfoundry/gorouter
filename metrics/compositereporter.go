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

//go:generate counterfeiter -o fakes/fake_metricreporter.go . MetricReporter
type MetricReporter interface {
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
	CaptureGorouterTime(gorouterTime float64)
	CaptureRouteServiceResponse(res *http.Response)
	CaptureWebSocketUpdate()
	CaptureWebSocketFailure()
	CaptureHTTPLatency(d time.Duration, sourceID string)
	CaptureRouteStats(totalRoutes int, msSinceLastUpdate int64)
	CaptureRoutesPruned(prunedRoutes uint64)
	CaptureLookupTime(t time.Duration)
	CaptureRegistryMessage(msg ComponentTagged, action string)
	CaptureRouteRegistrationLatency(t time.Duration)
	CaptureUnregistryMessage(msg ComponentTagged)
	CaptureFoundFileDescriptors(files int)
	CaptureNATSBufferedMessages(messages int)
	CaptureNATSDroppedMessages(messages int)
	UnmuzzleRouteRegistrationLatency()
}

type ComponentTagged interface {
	Component() string
}

type CompositeReporter struct {
	VarzReporter
	MetricReporter
}

type MultiMetricReporter []MetricReporter

var _ MetricReporter = MultiMetricReporter{}

func NewMultiMetricReporter(reporters ...MetricReporter) MultiMetricReporter {
	multiReporter := MultiMetricReporter{}
	multiReporter = append(multiReporter, reporters...)
	return multiReporter
}

func (m MultiMetricReporter) CaptureBackendExhaustedConns() {
	for _, r := range m {
		r.CaptureBackendExhaustedConns()
	}
}

func (m MultiMetricReporter) CaptureBackendTLSHandshakeFailed() {
	for _, r := range m {
		r.CaptureBackendTLSHandshakeFailed()
	}
}

func (m MultiMetricReporter) CaptureBackendInvalidID() {
	for _, r := range m {
		r.CaptureBackendInvalidID()
	}
}

func (m MultiMetricReporter) CaptureBackendInvalidTLSCert() {
	for _, r := range m {
		r.CaptureBackendInvalidTLSCert()
	}
}

func (m MultiMetricReporter) CaptureBadRequest() {
	for _, r := range m {
		r.CaptureBadRequest()
	}
}

func (m MultiMetricReporter) CaptureBadGateway() {
	for _, r := range m {
		r.CaptureBadGateway()
	}
}

func (m MultiMetricReporter) CaptureEmptyContentLengthHeader() {
	for _, r := range m {
		r.CaptureEmptyContentLengthHeader()
	}
}

func (m MultiMetricReporter) CaptureRoutingRequest(b *route.Endpoint) {
	for _, r := range m {
		r.CaptureRoutingRequest(b)
	}
}

func (m MultiMetricReporter) CaptureRouteServiceResponse(res *http.Response) {
	for _, r := range m {
		r.CaptureRouteServiceResponse(res)
	}
}

func (m MultiMetricReporter) CaptureRoutingResponse(statusCode int) {
	for _, r := range m {
		r.CaptureRoutingResponse(statusCode)
	}
}

func (m MultiMetricReporter) CaptureRoutingResponseLatency(b *route.Endpoint, statusCode int, t time.Time, d time.Duration) {
	for _, r := range m {
		r.CaptureRoutingResponseLatency(b, statusCode, t, d)
	}
}

func (m MultiMetricReporter) CaptureGorouterTime(gorouterTime float64) {
	for _, r := range m {
		r.CaptureGorouterTime(gorouterTime)
	}
}

func (m MultiMetricReporter) CaptureWebSocketUpdate() {
	for _, r := range m {
		r.CaptureWebSocketUpdate()
	}
}

func (m MultiMetricReporter) CaptureWebSocketFailure() {
	for _, r := range m {
		r.CaptureWebSocketFailure()
	}
}

func (m MultiMetricReporter) CaptureHTTPLatency(d time.Duration, sourceID string) {
	for _, r := range m {
		r.CaptureHTTPLatency(d, sourceID)
	}
}

func (m MultiMetricReporter) CaptureLookupTime(t time.Duration) {
	for _, r := range m {
		r.CaptureLookupTime(t)
	}
}

func (m MultiMetricReporter) UnmuzzleRouteRegistrationLatency() {
	for _, r := range m {
		r.UnmuzzleRouteRegistrationLatency()
	}
}

func (m MultiMetricReporter) CaptureRouteRegistrationLatency(t time.Duration) {
	for _, r := range m {
		r.CaptureRouteRegistrationLatency(t)
	}
}

func (m MultiMetricReporter) CaptureRouteStats(totalRoutes int, msSinceLastUpdate int64) {
	for _, r := range m {
		r.CaptureRouteStats(totalRoutes, msSinceLastUpdate)
	}
}

func (m MultiMetricReporter) CaptureRoutesPruned(routesPruned uint64) {
	for _, r := range m {
		r.CaptureRoutesPruned(routesPruned)
	}
}

func (m MultiMetricReporter) CaptureRegistryMessage(msg ComponentTagged, action string) {
	for _, r := range m {
		r.CaptureRegistryMessage(msg, action)
	}
}

func (m MultiMetricReporter) CaptureUnregistryMessage(msg ComponentTagged) {
	for _, r := range m {
		r.CaptureUnregistryMessage(msg)
	}
}

func (m MultiMetricReporter) CaptureFoundFileDescriptors(files int) {
	for _, r := range m {
		r.CaptureFoundFileDescriptors(files)
	}
}

func (m MultiMetricReporter) CaptureNATSBufferedMessages(messages int) {
	for _, r := range m {
		r.CaptureNATSBufferedMessages(messages)
	}
}

func (m MultiMetricReporter) CaptureNATSDroppedMessages(messages int) {
	for _, r := range m {
		r.CaptureNATSDroppedMessages(messages)
	}
}

func (c *CompositeReporter) CaptureBadRequest() {
	c.VarzReporter.CaptureBadRequest()
	c.MetricReporter.CaptureBadRequest()
}

func (c *CompositeReporter) CaptureBadGateway() {
	c.VarzReporter.CaptureBadGateway()
	c.MetricReporter.CaptureBadGateway()
}

func (c *CompositeReporter) CaptureEmptyContentLengthHeader() {
	c.MetricReporter.CaptureEmptyContentLengthHeader()
}

func (c *CompositeReporter) CaptureRoutingRequest(b *route.Endpoint) {
	c.VarzReporter.CaptureRoutingRequest(b)
	c.MetricReporter.CaptureRoutingRequest(b)
}

func (c *CompositeReporter) CaptureRoutingResponseLatency(b *route.Endpoint, statusCode int, t time.Time, d time.Duration) {
	c.VarzReporter.CaptureRoutingResponseLatency(b, statusCode, t, d)
	c.MetricReporter.CaptureRoutingResponseLatency(b, 0, time.Time{}, d)
}

func (c *CompositeReporter) CaptureHTTPLatency(d time.Duration, sourceID string) {
	c.MetricReporter.CaptureHTTPLatency(d, sourceID)
}
