package metrics_prometheus

import (
	"fmt"
	"log"
	"net/http"
	"time"

	mr "code.cloudfoundry.org/go-metric-registry"

	"code.cloudfoundry.org/gorouter/config"
	"code.cloudfoundry.org/gorouter/metrics"
	"code.cloudfoundry.org/gorouter/route"
)

// Metrics represents a prometheus metrics endpoint.
type Metrics struct {
	RouteRegistration           mr.CounterVec
	RouteUnregistration         mr.CounterVec
	RoutesPruned                mr.Counter
	TotalRoutes                 mr.Gauge
	TimeSinceLastRegistryUpdate mr.Gauge
	RouteLookupTime             mr.Histogram
	RouteRegistrationLatency    mr.Histogram
	RoutingRequest              mr.CounterVec
	BadRequest                  mr.Counter
	BadGateway                  mr.Counter
	EmptyContentLengthHeader    mr.Counter
	BackendInvalidID            mr.Counter
	BackendInvalidTLSCert       mr.Counter
	BackendTLSHandshakeFailed   mr.Counter
	BackendExhaustedConns       mr.Counter
	WebsocketUpgrades           mr.Counter
	WebsocketFailures           mr.Counter
	Responses                   mr.CounterVec
	RouteServicesResponses      mr.CounterVec
	RoutingResponseLatency      mr.HistogramVec
	FoundFileDescriptors        mr.Gauge
	NATSBufferedMessages        mr.Gauge
	NATSDroppedMessages         mr.Gauge
	HTTPLatency                 mr.HistogramVec
	perRequestMetricsReporting  bool
}

func NewMetricsRegistry(config config.PrometheusConfig) *mr.Registry {
	var metricsRegistry *mr.Registry
	if config.Port != 0 && config.CertPath != "" {
		metricsRegistry = mr.NewRegistry(log.Default(),
			// the server starts in background. Endpoint: 127.0.0.1:port/metrics
			mr.WithTLSServer(int(config.Port), config.CertPath, config.KeyPath, config.CAPath))
	} else { // port zero is used in test suites
		metricsRegistry = mr.NewRegistry(log.Default(),
			// the server starts in background. Endpoint: 127.0.0.1:port/metrics
			mr.WithServer(int(config.Port)))
	}
	return metricsRegistry
}

var _ metrics.MetricReporter = &Metrics{}

func NewMetrics(registry *mr.Registry, perRequestMetricsReporting bool, meterConfig config.MetersConfig) *Metrics {
	return &Metrics{
		RouteRegistration:           registry.NewCounterVec("registry_message", "number of route registration messages", []string{"component", "action"}),
		RouteUnregistration:         registry.NewCounterVec("unregistry_message", "number of unregister messages", []string{"component"}),
		RoutesPruned:                registry.NewCounter("routes_pruned", "number of pruned routes"),
		TotalRoutes:                 registry.NewGauge("total_routes", "number of total routes"),
		TimeSinceLastRegistryUpdate: registry.NewGauge("ms_since_last_registry_update", "time since last registry update in ms"),
		RouteLookupTime:             registry.NewHistogram("route_lookup_time", "route lookup time per request in ns", meterConfig.RouteLookupTimeHistogramBuckets),
		RouteRegistrationLatency:    registry.NewHistogram("route_registration_latency", "route registration latency in ms", meterConfig.RouteRegistrationLatencyHistogramBuckets),
		RoutingRequest:              registry.NewCounterVec("total_requests", "number of routing requests", []string{"component"}),
		BadRequest:                  registry.NewCounter("rejected_requests", "number of rejected requests"),
		BadGateway:                  registry.NewCounter("bad_gateways", "number of bad gateway errors received from backends"),
		EmptyContentLengthHeader:    registry.NewCounter("empty_content_length_header", "number of requests with the empty content length header"),
		BackendInvalidID:            registry.NewCounter("backend_invalid_id", "number of bad backend id errors received from backends"),
		BackendInvalidTLSCert:       registry.NewCounter("backend_invalid_tls_cert", "number of tls certificate errors received from backends"),
		BackendTLSHandshakeFailed:   registry.NewCounter("backend_tls_handshake_failed", "number of backend handshake errors"),
		BackendExhaustedConns:       registry.NewCounter("backend_exhausted_conns", "number of errors related to backend connection limit reached"),
		WebsocketUpgrades:           registry.NewCounter("websocket_upgrades", "websocket upgrade to websocket"),
		WebsocketFailures:           registry.NewCounter("websocket_failures", "websocket failure"),
		Responses:                   registry.NewCounterVec("responses", "number of responses", []string{"status_group"}),
		RouteServicesResponses:      registry.NewCounterVec("responses_route_services", "number of responses for route services", []string{"status_group"}),
		RoutingResponseLatency:      registry.NewHistogramVec("latency", "routing response latency in ms", []string{"component"}, meterConfig.RoutingResponseLatencyHistogramBuckets),
		FoundFileDescriptors:        registry.NewGauge("file_descriptors", "number of file descriptors found"),
		NATSBufferedMessages:        registry.NewGauge("buffered_messages", "number of buffered messages in NATS"),
		NATSDroppedMessages:         registry.NewGauge("total_dropped_messages", "number of total dropped messages in NATS"),
		HTTPLatency:                 registry.NewHistogramVec("http_latency_seconds", "the latency of http requests from gorouter and back in sec", []string{"source_id"}, meterConfig.HTTPLatencyHistogramBuckets),
		perRequestMetricsReporting:  perRequestMetricsReporting,
	}
}

func (metrics *Metrics) CaptureRouteStats(totalRoutes int, msSinceLastUpdate int64) {
	metrics.TotalRoutes.Set(float64(totalRoutes))
	metrics.TimeSinceLastRegistryUpdate.Set(float64(msSinceLastUpdate))
}

func (metrics *Metrics) CaptureRegistryMessage(msg metrics.ComponentTagged, action string) {
	metrics.RouteRegistration.Add(1, []string{msg.Component(), action})
}

func (metrics *Metrics) CaptureUnregistryMessage(msg metrics.ComponentTagged) {
	metrics.RouteUnregistration.Add(1, []string{msg.Component()})
}

func (metrics *Metrics) CaptureRoutesPruned(routesPruned uint64) {
	metrics.RoutesPruned.Add(float64(routesPruned))
}

func (metrics *Metrics) CaptureTotalRoutes(totalRoutes int) {
	metrics.TotalRoutes.Set(float64(totalRoutes))
}

func (metrics *Metrics) CaptureTimeSinceLastRegistryUpdate(msSinceLastUpdate int64) {
	metrics.TimeSinceLastRegistryUpdate.Set(float64(msSinceLastUpdate))
}

func (metrics *Metrics) CaptureLookupTime(t time.Duration) {
	if metrics.perRequestMetricsReporting {
		metrics.RouteLookupTime.Observe(float64(t.Nanoseconds()))
	}
}

func (metrics *Metrics) CaptureRouteRegistrationLatency(t time.Duration) {
	metrics.RouteRegistrationLatency.Observe(float64(t) / float64(time.Millisecond))
}

// UnmuzzleRouteRegistrationLatency should set a flag which suppresses metric data.
// That makes sense for Envelope V1 where we send it to collector any time we got new value
// but is unnecessary for Prometheus where data is buffered and sent to collector on constant frequency base.
// We still need this method though to fulfil the interface.
func (metrics *Metrics) UnmuzzleRouteRegistrationLatency() {}

func (metrics *Metrics) CaptureBackendExhaustedConns() {
	metrics.BackendExhaustedConns.Add(1)
}

func (metrics *Metrics) CaptureBadGateway() {
	metrics.BadGateway.Add(1)
}

func (metrics *Metrics) CaptureBackendInvalidID() {
	metrics.BackendInvalidID.Add(1)
}

func (metrics *Metrics) CaptureBackendInvalidTLSCert() {
	metrics.BackendInvalidTLSCert.Add(1)
}

func (metrics *Metrics) CaptureBackendTLSHandshakeFailed() {
	metrics.BackendTLSHandshakeFailed.Add(1)
}

func (metrics *Metrics) CaptureBadRequest() {
	metrics.BadRequest.Add(1)
}

func (metrics *Metrics) CaptureEmptyContentLengthHeader() {
	metrics.EmptyContentLengthHeader.Add(1)
}

// CaptureRoutingRequest used to capture backend round trips
func (metrics *Metrics) CaptureRoutingRequest(b *route.Endpoint) {
	metrics.RoutingRequest.Add(1, []string{b.Component()})
}

func (metrics *Metrics) CaptureRoutingResponse(statusCode int) {
	metrics.Responses.Add(1, []string{statusGroupName(statusCode)})
}

// CaptureRoutingResponseLatency has extra arguments to match varz reporter
func (metrics *Metrics) CaptureRoutingResponseLatency(b *route.Endpoint, _ int, _ time.Time, d time.Duration) {
	if metrics.perRequestMetricsReporting {
		metrics.RoutingResponseLatency.Observe(float64(d)/float64(time.Millisecond), []string{b.Component()})
	}
}

func (metrics *Metrics) CaptureRouteServiceResponse(res *http.Response) {
	var statusCode int
	if res != nil {
		statusCode = res.StatusCode
	}
	metrics.RouteServicesResponses.Add(1, []string{statusGroupName(statusCode)})
}

func (metrics *Metrics) CaptureWebSocketUpdate() {
	metrics.WebsocketUpgrades.Add(1)
}

func (metrics *Metrics) CaptureWebSocketFailure() {
	metrics.WebsocketFailures.Add(1)
}

func (metrics *Metrics) CaptureFoundFileDescriptors(files int) {
	metrics.FoundFileDescriptors.Set(float64(files))
}

func (metrics *Metrics) CaptureNATSBufferedMessages(messages int) {
	metrics.NATSBufferedMessages.Set(float64(messages))
}

func (metrics *Metrics) CaptureNATSDroppedMessages(messages int) {
	metrics.NATSDroppedMessages.Set(float64(messages))
}

func (metrics *Metrics) CaptureHTTPLatency(d time.Duration, sourceID string) {
	metrics.HTTPLatency.Observe(float64(d)/float64(time.Second), []string{sourceID})
}

func statusGroupName(statusCode int) string {
	statusGroupNum := statusCode / 100
	if statusGroupNum >= 2 && statusGroupNum <= 5 {
		return fmt.Sprintf("%dxx", statusGroupNum)
	}
	return "xxx"
}
