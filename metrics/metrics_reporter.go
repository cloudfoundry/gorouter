package metrics

import (
	"net/http"

	"code.cloudfoundry.org/gorouter/metrics/reporter"
	"code.cloudfoundry.org/gorouter/route"
	dropsondeMetrics "github.com/cloudfoundry/dropsonde/metrics"

	"fmt"
	"strings"
	"time"
)

type MetricsReporter struct {
}

func NewMetricsReporter() *MetricsReporter {
	return &MetricsReporter{}
}

func (m *MetricsReporter) CaptureBadRequest(req *http.Request) {
	dropsondeMetrics.BatchIncrementCounter("rejected_requests")
}

func (m *MetricsReporter) CaptureBadGateway(req *http.Request) {
	dropsondeMetrics.BatchIncrementCounter("bad_gateways")
}

func (m *MetricsReporter) CaptureRoutingRequest(b *route.Endpoint, req *http.Request) {
	dropsondeMetrics.BatchIncrementCounter("total_requests")

	componentName, ok := b.Tags["component"]
	if ok && len(componentName) > 0 {
		dropsondeMetrics.BatchIncrementCounter(fmt.Sprintf("requests.%s", componentName))
		if strings.HasPrefix(componentName, "dea-") {
			dropsondeMetrics.BatchIncrementCounter("routed_app_requests")
		}
	}
}

func (m *MetricsReporter) CaptureRoutingResponse(b *route.Endpoint, res *http.Response, t time.Time, d time.Duration) {
	responseCounterName := getResponseCounterName(res)

	dropsondeMetrics.BatchIncrementCounter(responseCounterName)
	dropsondeMetrics.BatchIncrementCounter("responses")

	latency := float64(d / time.Millisecond)
	unit := "ms"
	dropsondeMetrics.SendValue("latency", latency, unit)

	componentName, ok := b.Tags["component"]
	if ok && len(componentName) > 0 {
		dropsondeMetrics.BatchIncrementCounter(fmt.Sprintf("responses.%s", componentName))
		dropsondeMetrics.BatchIncrementCounter(fmt.Sprintf("%s.%s", responseCounterName, componentName))
		dropsondeMetrics.SendValue(fmt.Sprintf("latency.%s", componentName), latency, unit)
	}
}

func (c *MetricsReporter) CaptureLookupTime(t time.Duration) {
	unit := "ns"
	dropsondeMetrics.SendValue("route_lookup_time", float64(t.Nanoseconds()), unit)
}

func (c *MetricsReporter) CaptureRouteStats(totalRoutes int, msSinceLastUpdate uint64) {
	dropsondeMetrics.SendValue("total_routes", float64(totalRoutes), "")
	dropsondeMetrics.SendValue("ms_since_last_registry_update", float64(msSinceLastUpdate), "ms")
}

func (c *MetricsReporter) CaptureRegistryMessage(msg reporter.ComponentTagged) {
	dropsondeMetrics.IncrementCounter("registry_message." + msg.Component())
}

func getResponseCounterName(res *http.Response) string {
	var statusCode int

	if res != nil {
		statusCode = res.StatusCode / 100
	}
	if statusCode >= 2 && statusCode <= 5 {
		return fmt.Sprintf("responses.%dxx", statusCode)
	}
	return "responses.xxx"
}
