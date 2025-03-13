package handlers

import (
	"net/http"
	"time"

	"github.com/urfave/negroni/v3"

	"code.cloudfoundry.org/gorouter/metrics"
)

type httpLatencyPrometheusHandler struct {
	reporter metrics.ProxyReporter
}

// NewHTTPLatencyPrometheus creates a new handler that handles prometheus metrics for latency
func NewHTTPLatencyPrometheus(reporter metrics.ProxyReporter) negroni.Handler {
	return &httpLatencyPrometheusHandler{
		reporter: reporter,
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

	hl.reporter.CaptureHTTPLatency(latency, sourceId)
}
