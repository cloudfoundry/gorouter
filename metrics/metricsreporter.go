package metrics

import (
	"fmt"
	"net/http"
	"strings"
	"sync/atomic"
	"time"

	"code.cloudfoundry.org/gorouter/route"

	"github.com/cloudfoundry/dropsonde/metrics"
)

type MetricsReporter struct {
	Sender                     metrics.MetricSender
	Batcher                    metrics.MetricBatcher
	PerRequestMetricsReporting bool
	unmuzzled                  uint64
}

func (m *MetricsReporter) CaptureBackendExhaustedConns() {
	m.Batcher.BatchIncrementCounter("backend_exhausted_conns")
}

func (m *MetricsReporter) CaptureBackendTLSHandshakeFailed() {
	m.Batcher.BatchIncrementCounter("backend_tls_handshake_failed")
}

func (m *MetricsReporter) CaptureBackendInvalidID() {
	m.Batcher.BatchIncrementCounter("backend_invalid_id")
}

func (m *MetricsReporter) CaptureBackendInvalidTLSCert() {
	m.Batcher.BatchIncrementCounter("backend_invalid_tls_cert")
}

func (m *MetricsReporter) CaptureBadRequest() {
	m.Batcher.BatchIncrementCounter("rejected_requests")
}

func (m *MetricsReporter) CaptureBadGateway() {
	m.Batcher.BatchIncrementCounter("bad_gateways")
}

func (m *MetricsReporter) CaptureRoutingRequest(b *route.Endpoint) {
	m.Batcher.BatchIncrementCounter("total_requests")

	componentName, ok := b.Tags["component"]
	if ok && len(componentName) > 0 {
		m.Batcher.BatchIncrementCounter(fmt.Sprintf("requests.%s", componentName))
		if strings.HasPrefix(componentName, "dea-") {
			m.Batcher.BatchIncrementCounter("routed_app_requests")
		}
	}
}

func (m *MetricsReporter) CaptureRouteServiceResponse(res *http.Response) {
	var statusCode int
	if res != nil {
		statusCode = res.StatusCode
	}
	m.Batcher.BatchIncrementCounter(fmt.Sprintf("responses.route_services.%s", getResponseCounterName(statusCode)))
	m.Batcher.BatchIncrementCounter("responses.route_services")
}

func (m *MetricsReporter) CaptureRoutingResponse(statusCode int) {
	m.Batcher.BatchIncrementCounter(fmt.Sprintf("responses.%s", getResponseCounterName(statusCode)))
	m.Batcher.BatchIncrementCounter("responses")
}

func (m *MetricsReporter) CaptureRoutingResponseLatency(b *route.Endpoint, _ int, _ time.Time, d time.Duration) {
	if m.PerRequestMetricsReporting {
		//this function has extra arguments to match varz reporter
		latency := float64(d / time.Millisecond)
		unit := "ms"
		m.Sender.SendValue("latency", latency, unit)

		componentName, ok := b.Tags["component"]
		if ok && len(componentName) > 0 {
			m.Sender.SendValue(fmt.Sprintf("latency.%s", componentName), latency, unit)
		}
	}
}

func (m *MetricsReporter) CaptureLookupTime(t time.Duration) {
	if m.PerRequestMetricsReporting {
		unit := "ns"
		m.Sender.SendValue("route_lookup_time", float64(t.Nanoseconds()), unit)
	}
}

func (m *MetricsReporter) UnmuzzleRouteRegistrationLatency() {
	atomic.StoreUint64(&m.unmuzzled, 1)
}

func (m *MetricsReporter) CaptureRouteRegistrationLatency(t time.Duration) {
	if atomic.LoadUint64(&m.unmuzzled) == 1 {
		m.Sender.SendValue("route_registration_latency", float64(t/time.Millisecond), "ms")
	}
}

func (m *MetricsReporter) CaptureRouteStats(totalRoutes int, msSinceLastUpdate int64) {
	m.Sender.SendValue("total_routes", float64(totalRoutes), "")
	m.Sender.SendValue("ms_since_last_registry_update", float64(msSinceLastUpdate), "ms")
}

func (m *MetricsReporter) CaptureRoutesPruned(routesPruned uint64) {
	m.Batcher.BatchAddCounter("routes_pruned", routesPruned)
}

func (m *MetricsReporter) CaptureRegistryMessage(msg ComponentTagged) {
	var componentName string
	if msg.Component() == "" {
		componentName = "registry_message"
	} else {
		componentName = "registry_message." + msg.Component()
	}
	m.Batcher.BatchIncrementCounter(componentName)
}

func (m *MetricsReporter) CaptureUnregistryMessage(msg ComponentTagged) {
	var componentName string
	if msg.Component() == "" {
		componentName = "unregistry_message"
	} else {
		componentName = "unregistry_message." + msg.Component()
	}
	m.Sender.IncrementCounter(componentName)
}

func (m *MetricsReporter) CaptureWebSocketUpdate() {
	m.Batcher.BatchIncrementCounter("websocket_upgrades")
}

func (m *MetricsReporter) CaptureWebSocketFailure() {
	m.Batcher.BatchIncrementCounter("websocket_failures")
}

func getResponseCounterName(statusCode int) string {
	statusCode = statusCode / 100
	if statusCode >= 2 && statusCode <= 5 {
		return fmt.Sprintf("%dxx", statusCode)
	}
	return "xxx"
}
