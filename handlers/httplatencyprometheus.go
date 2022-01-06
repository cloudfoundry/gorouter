package handlers

import (
	"net/http"
	"time"

	metrics "code.cloudfoundry.org/go-metric-registry"
	"github.com/urfave/negroni"
)

type Registry interface {
	NewHistogram(name, helpText string, buckets []float64, opts ...metrics.MetricOption) metrics.Histogram
}

type httpLatencyPrometheusHandler struct {
	registry Registry
}

// NewHTTPStartStop creates a new handler that handles emitting frontent
// HTTP StartStop events
func NewHTTPLatencyPrometheus(r Registry) negroni.Handler {
	return &httpLatencyPrometheusHandler{
		registry: r,
	}
}

// ServeHTTP handles emitting a StartStop event after the request has been completed
func (hl *httpLatencyPrometheusHandler) ServeHTTP(rw http.ResponseWriter, r *http.Request, next http.HandlerFunc) {
	start := time.Now()
	next(rw, r)
	stop := time.Now()

	latency := stop.Sub(start) / time.Second

	sourceId := "gorouter"
	endpoint, err := GetEndpoint(r.Context())
	if err == nil {
		if endpoint.Tags["source_id"] != "" {
			sourceId = endpoint.Tags["source_id"]
		}
	}

	h := hl.registry.NewHistogram("http_latency_seconds", "the latency of http requests from gorouter and back",
		[]float64{0.1, 0.2, 0.4, 0.8, 1.6, 3.2, 6.4, 12.8, 25.6},
		metrics.WithMetricLabels(map[string]string{"source_id": sourceId}))
	h.Observe(float64(latency))
}
