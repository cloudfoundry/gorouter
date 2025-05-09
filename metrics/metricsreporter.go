package metrics

import (
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync/atomic"
	"time"

	"code.cloudfoundry.org/gorouter/route"

	log "code.cloudfoundry.org/gorouter/logger"

	"github.com/cloudfoundry/dropsonde/metrics"
)

type Metrics struct {
	Sender                     metrics.MetricSender
	Batcher                    metrics.MetricBatcher
	PerRequestMetricsReporting bool
	Logger                     *slog.Logger
	unmuzzled                  uint64
}

func (m *Metrics) CaptureBackendExhaustedConns() {
	m.Batcher.BatchIncrementCounter("backend_exhausted_conns")
}

func (m *Metrics) CaptureBackendTLSHandshakeFailed() {
	m.Batcher.BatchIncrementCounter("backend_tls_handshake_failed")
}

func (m *Metrics) CaptureBackendInvalidID() {
	m.Batcher.BatchIncrementCounter("backend_invalid_id")
}

func (m *Metrics) CaptureBackendInvalidTLSCert() {
	m.Batcher.BatchIncrementCounter("backend_invalid_tls_cert")
}

func (m *Metrics) CaptureBadRequest() {
	m.Batcher.BatchIncrementCounter("rejected_requests")
}

func (m *Metrics) CaptureBadGateway() {
	m.Batcher.BatchIncrementCounter("bad_gateways")
}

func (m *Metrics) CaptureEmptyContentLengthHeader() {
	m.Batcher.BatchIncrementCounter("empty_content_length_header")
}

func (m *Metrics) CaptureRoutingRequest(b *route.Endpoint) {
	m.Batcher.BatchIncrementCounter("total_requests")

	componentName, ok := b.Tags["component"]
	if ok && len(componentName) > 0 {
		m.Batcher.BatchIncrementCounter(fmt.Sprintf("requests.%s", componentName))
		if strings.HasPrefix(componentName, "dea-") {
			m.Batcher.BatchIncrementCounter("routed_app_requests")
		}
	}
}

func (m *Metrics) CaptureRouteServiceResponse(res *http.Response) {
	var statusCode int
	if res != nil {
		statusCode = res.StatusCode
	}
	m.Batcher.BatchIncrementCounter(fmt.Sprintf("responses.route_services.%s", getResponseCounterName(statusCode)))
	m.Batcher.BatchIncrementCounter("responses.route_services")
}

func (m *Metrics) CaptureRoutingResponse(statusCode int) {
	m.Batcher.BatchIncrementCounter(fmt.Sprintf("responses.%s", getResponseCounterName(statusCode)))
	m.Batcher.BatchIncrementCounter("responses")
}

func (m *Metrics) CaptureGorouterTime(gorouterTime float64) {
	if m.PerRequestMetricsReporting {
		unit := "ms"
		err := m.Sender.SendValue("gorouter_time", gorouterTime*1000, unit)
		if err != nil {
			m.Logger.Debug("failed-sending-metric", log.ErrAttr(err), slog.String("metric", "gorouter_time"))
		}
	}
}

func (m *Metrics) CaptureRoutingResponseLatency(b *route.Endpoint, _ int, _ time.Time, d time.Duration) {
	if m.PerRequestMetricsReporting {
		//this function has extra arguments to match varz reporter
		latency := float64(d / time.Millisecond)
		unit := "ms"
		err := m.Sender.SendValue("latency", latency, unit)
		if err != nil {
			m.Logger.Debug("failed-sending-metric", log.ErrAttr(err), slog.String("metric", "latency"))
		}

		componentName, ok := b.Tags["component"]
		if ok && len(componentName) > 0 {
			err := m.Sender.SendValue(fmt.Sprintf("latency.%s", componentName), latency, unit)
			if err != nil {
				m.Logger.Debug("failed-sending-metric", log.ErrAttr(err), slog.String("metric", fmt.Sprintf("latency.%s", componentName)))
			}
		}
	}
}

func (m *Metrics) CaptureLookupTime(t time.Duration) {
	if m.PerRequestMetricsReporting {
		unit := "ns"
		err := m.Sender.SendValue("route_lookup_time", float64(t.Nanoseconds()), unit)
		if err != nil {
			m.Logger.Debug("failed-sending-metric", log.ErrAttr(err), slog.String("metric", "route_lookup_time"))
		}
	}
}

func (m *Metrics) UnmuzzleRouteRegistrationLatency() {
	atomic.StoreUint64(&m.unmuzzled, 1)
}

func (m *Metrics) CaptureRouteRegistrationLatency(t time.Duration) {
	if atomic.LoadUint64(&m.unmuzzled) == 1 {
		err := m.Sender.SendValue("route_registration_latency", float64(t/time.Millisecond), "ms")
		if err != nil {
			m.Logger.Debug("failed-sending-metric", log.ErrAttr(err), slog.String("metric", "route_registration_latency"))
		}
	}
}

func (m *Metrics) CaptureRouteStats(totalRoutes int, msSinceLastUpdate int64) {
	err := m.Sender.SendValue("total_routes", float64(totalRoutes), "")
	if err != nil {
		m.Logger.Debug("failed-sending-metric", log.ErrAttr(err), slog.String("metric", "total_routes"))
	}
	err = m.Sender.SendValue("ms_since_last_registry_update", float64(msSinceLastUpdate), "ms")
	if err != nil {
		m.Logger.Debug("failed-sending-metric", log.ErrAttr(err), slog.String("metric", "ms_since_last_registry_update"))
	}
}

func (m *Metrics) CaptureRoutesPruned(routesPruned uint64) {
	m.Batcher.BatchAddCounter("routes_pruned", routesPruned)
}

func (m *Metrics) CaptureRegistryMessage(msg ComponentTagged, _ string) {
	var componentName string
	if msg.Component() == "" {
		componentName = "registry_message"
	} else {
		componentName = "registry_message." + msg.Component()
	}
	m.Batcher.BatchIncrementCounter(componentName)
}

func (m *Metrics) CaptureUnregistryMessage(msg ComponentTagged) {
	var componentName string
	if msg.Component() == "" {
		componentName = "unregistry_message"
	} else {
		componentName = "unregistry_message." + msg.Component()
	}
	err := m.Sender.IncrementCounter(componentName)
	if err != nil {
		m.Logger.Debug("failed-sending-metric", log.ErrAttr(err), slog.String("metric", componentName))
	}
}

func (m *Metrics) CaptureWebSocketUpdate() {
	m.Batcher.BatchIncrementCounter("websocket_upgrades")
}

func (m *Metrics) CaptureWebSocketFailure() {
	m.Batcher.BatchIncrementCounter("websocket_failures")
}

func (m *Metrics) CaptureFoundFileDescriptors(files int) {
	m.Sender.SendValue("file_descriptors", float64(files), "file")
}

func (m *Metrics) CaptureNATSBufferedMessages(messages int) {
	m.Sender.SendValue("buffered_messages", float64(messages), "message")
}

func (m *Metrics) CaptureNATSDroppedMessages(messages int) {
	m.Sender.SendValue("total_dropped_messages", float64(messages), "message")
}

// CaptureHTTPLatency observes histogram of HTTP latency metric
// Empty implementation here is to fulfil interface
func (m *Metrics) CaptureHTTPLatency(_ time.Duration, _ string) {
}

func getResponseCounterName(statusCode int) string {
	statusCode = statusCode / 100
	if statusCode >= 2 && statusCode <= 5 {
		return fmt.Sprintf("%dxx", statusCode)
	}
	return "xxx"
}
