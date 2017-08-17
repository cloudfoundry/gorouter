package metrics

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"code.cloudfoundry.org/gorouter/route"
	"github.com/cloudfoundry/dropsonde/metrics"
)

type MetricsReporter struct {
	sender  metrics.MetricSender
	batcher metrics.MetricBatcher
}

func NewMetricsReporter(sender metrics.MetricSender, batcher metrics.MetricBatcher) *MetricsReporter {
	return &MetricsReporter{
		sender:  sender,
		batcher: batcher,
	}
}

func (m *MetricsReporter) CaptureBackendExhaustedConns() {
	m.batcher.BatchIncrementCounter("backend_exhausted_conns")
}

func (m *MetricsReporter) CaptureBadRequest() {
	m.batcher.BatchIncrementCounter("rejected_requests")
}

func (m *MetricsReporter) CaptureBadGateway() {
	m.batcher.BatchIncrementCounter("bad_gateways")
}

func (m *MetricsReporter) CaptureRoutingRequest(b *route.Endpoint) {
	m.batcher.BatchIncrementCounter("total_requests")

	componentName, ok := b.Tags["component"]
	if ok && len(componentName) > 0 {
		m.batcher.BatchIncrementCounter(fmt.Sprintf("requests.%s", componentName))
		if strings.HasPrefix(componentName, "dea-") {
			m.batcher.BatchIncrementCounter("routed_app_requests")
		}
	}
}

func (m *MetricsReporter) CaptureRouteServiceResponse(res *http.Response) {
	var statusCode int
	if res != nil {
		statusCode = res.StatusCode
	}
	m.batcher.BatchIncrementCounter(fmt.Sprintf("responses.route_services.%s", getResponseCounterName(statusCode)))
	m.batcher.BatchIncrementCounter("responses.route_services")
}

func (m *MetricsReporter) CaptureRoutingResponse(statusCode int) {
	m.batcher.BatchIncrementCounter(fmt.Sprintf("responses.%s", getResponseCounterName(statusCode)))
	m.batcher.BatchIncrementCounter("responses")
}

func (m *MetricsReporter) CaptureRoutingResponseLatency(b *route.Endpoint, d time.Duration) {
	latency := float64(d / time.Millisecond)
	unit := "ms"
	m.sender.SendValue("latency", latency, unit)

	componentName, ok := b.Tags["component"]
	if ok && len(componentName) > 0 {
		m.sender.SendValue(fmt.Sprintf("latency.%s", componentName), latency, unit)
	}
}

func (m *MetricsReporter) CaptureLookupTime(t time.Duration) {
	unit := "ns"
	m.sender.SendValue("route_lookup_time", float64(t.Nanoseconds()), unit)
}

func (m *MetricsReporter) CaptureRouteStats(totalRoutes int, msSinceLastUpdate uint64) {
	m.sender.SendValue("total_routes", float64(totalRoutes), "")
	m.sender.SendValue("ms_since_last_registry_update", float64(msSinceLastUpdate), "ms")
}

func (m *MetricsReporter) CaptureRegistryMessage(msg ComponentTagged) {
	var componentName string
	if msg.Component() == "" {
		componentName = "registry_message"
	} else {
		componentName = "registry_message." + msg.Component()
	}
	m.batcher.BatchIncrementCounter(componentName)
}

func (m *MetricsReporter) CaptureUnregistryMessage(msg ComponentTagged) {
	var componentName string
	if msg.Component() == "" {
		componentName = "unregistry_message"
	} else {
		componentName = "unregistry_message." + msg.Component()
	}
	m.sender.IncrementCounter(componentName)
}

func (m *MetricsReporter) CaptureWebSocketUpdate() {
	m.batcher.BatchIncrementCounter("websocket_upgrades")
}

func (m *MetricsReporter) CaptureWebSocketFailure() {
	m.batcher.BatchIncrementCounter("websocket_failures")
}

func getResponseCounterName(statusCode int) string {
	statusCode = statusCode / 100
	if statusCode >= 2 && statusCode <= 5 {
		return fmt.Sprintf("%dxx", statusCode)
	}
	return "xxx"
}
