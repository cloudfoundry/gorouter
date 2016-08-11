package metrics_test

import (
	"code.cloudfoundry.org/gorouter/metrics"
	"code.cloudfoundry.org/routing-api/models"
	"github.com/cloudfoundry/dropsonde/metric_sender/fake"
	dropsondeMetrics "github.com/cloudfoundry/dropsonde/metrics"

	"net/http"
	"time"

	"code.cloudfoundry.org/gorouter/route"
	"github.com/cloudfoundry/dropsonde/metricbatcher"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("MetricsReporter", func() {
	var metricsReporter *metrics.MetricsReporter
	var req *http.Request
	var endpoint *route.Endpoint
	var sender *fake.FakeMetricSender

	BeforeEach(func() {
		metricsReporter = metrics.NewMetricsReporter()
		req, _ = http.NewRequest("GET", "https://example.com", nil)
		endpoint = route.NewEndpoint("someId", "host", 2222, "privateId", "2", map[string]string{}, 30, "", models.ModificationTag{})
		sender = fake.NewFakeMetricSender()
		batcher := metricbatcher.New(sender, time.Millisecond)
		dropsondeMetrics.Initialize(sender, batcher)
	})

	It("increments the bad_requests metric", func() {
		metricsReporter.CaptureBadRequest(req)
		Eventually(func() uint64 { return sender.GetCounter("rejected_requests") }).Should(BeEquivalentTo(1))

		metricsReporter.CaptureBadRequest(req)
		Eventually(func() uint64 { return sender.GetCounter("rejected_requests") }).Should(BeEquivalentTo(2))
	})

	It("increments the bad_gateway metric", func() {
		metricsReporter.CaptureBadGateway(req)
		Eventually(func() uint64 { return sender.GetCounter("bad_gateways") }).Should(BeEquivalentTo(1))

		metricsReporter.CaptureBadGateway(req)
		Eventually(func() uint64 { return sender.GetCounter("bad_gateways") }).Should(BeEquivalentTo(2))
	})

	Context("increments the request metrics", func() {
		It("increments the total requests metric", func() {
			metricsReporter.CaptureRoutingRequest(&route.Endpoint{}, req)
			Eventually(func() uint64 { return sender.GetCounter("total_requests") }).Should(BeEquivalentTo(1))

			metricsReporter.CaptureRoutingRequest(endpoint, req)
			Eventually(func() uint64 { return sender.GetCounter("total_requests") }).Should(BeEquivalentTo(2))
		})

		It("should not emit a request metric for a component when no tags exist", func() {
			metricsReporter.CaptureRoutingRequest(endpoint, req)
			Consistently(func() uint64 { return sender.GetCounter("requests.") }).Should(BeEquivalentTo(0))

			endpoint.Tags["component"] = ""
			metricsReporter.CaptureRoutingRequest(endpoint, req)
			Consistently(func() uint64 { return sender.GetCounter("requests.") }).Should(BeEquivalentTo(0))
		})

		It("increments the requests metric for the given component", func() {
			endpoint.Tags["component"] = "CloudController"
			metricsReporter.CaptureRoutingRequest(endpoint, req)
			Eventually(func() uint64 { return sender.GetCounter("requests.CloudController") }).Should(BeEquivalentTo(1))

			metricsReporter.CaptureRoutingRequest(endpoint, req)
			Eventually(func() uint64 { return sender.GetCounter("requests.CloudController") }).Should(BeEquivalentTo(2))

			endpoint.Tags["component"] = "UAA"
			metricsReporter.CaptureRoutingRequest(endpoint, req)
			Eventually(func() uint64 { return sender.GetCounter("requests.UAA") }).Should(BeEquivalentTo(1))

			metricsReporter.CaptureRoutingRequest(endpoint, req)
			Eventually(func() uint64 { return sender.GetCounter("requests.UAA") }).Should(BeEquivalentTo(2))

		})

		It("increments the routed_app_requests metric", func() {
			endpoint.Tags["component"] = "dea-1"
			metricsReporter.CaptureRoutingRequest(endpoint, req)
			Eventually(func() uint64 { return sender.GetCounter("routed_app_requests") }).Should(BeEquivalentTo(1))

			endpoint.Tags["component"] = "dea-3"
			metricsReporter.CaptureRoutingRequest(endpoint, req)
			Eventually(func() uint64 { return sender.GetCounter("routed_app_requests") }).Should(BeEquivalentTo(2))

			endpoint.Tags["component"] = "CloudController"
			metricsReporter.CaptureRoutingRequest(endpoint, req)
			Consistently(func() uint64 { return sender.GetCounter("routed_app_requests") }).Should(BeEquivalentTo(2))
		})
	})

	Context("increments the response metrics", func() {
		It("increments the 2XX response metrics", func() {
			response := http.Response{
				StatusCode: 200,
			}

			metricsReporter.CaptureRoutingResponse(endpoint, &response, time.Now(), time.Millisecond)
			Eventually(func() uint64 { return sender.GetCounter("responses.2xx") }).Should(BeEquivalentTo(1))

			metricsReporter.CaptureRoutingResponse(endpoint, &response, time.Now(), time.Millisecond)
			Eventually(func() uint64 { return sender.GetCounter("responses.2xx") }).Should(BeEquivalentTo(2))
		})

		It("increments the 3XX response metrics", func() {
			response := http.Response{
				StatusCode: 304,
			}

			metricsReporter.CaptureRoutingResponse(endpoint, &response, time.Now(), time.Millisecond)
			Eventually(func() uint64 { return sender.GetCounter("responses.3xx") }).Should(BeEquivalentTo(1))

			metricsReporter.CaptureRoutingResponse(endpoint, &response, time.Now(), time.Millisecond)
			Eventually(func() uint64 { return sender.GetCounter("responses.3xx") }).Should(BeEquivalentTo(2))
		})

		It("increments the 4XX response metrics", func() {
			response := http.Response{
				StatusCode: 401,
			}

			metricsReporter.CaptureRoutingResponse(endpoint, &response, time.Now(), time.Millisecond)
			Eventually(func() uint64 { return sender.GetCounter("responses.4xx") }).Should(BeEquivalentTo(1))

			metricsReporter.CaptureRoutingResponse(endpoint, &response, time.Now(), time.Millisecond)
			Eventually(func() uint64 { return sender.GetCounter("responses.4xx") }).Should(BeEquivalentTo(2))
		})

		It("increments the 5XX response metrics", func() {
			response := http.Response{
				StatusCode: 504,
			}

			metricsReporter.CaptureRoutingResponse(endpoint, &response, time.Now(), time.Millisecond)
			Eventually(func() uint64 { return sender.GetCounter("responses.5xx") }).Should(BeEquivalentTo(1))

			metricsReporter.CaptureRoutingResponse(endpoint, &response, time.Now(), time.Millisecond)
			Eventually(func() uint64 { return sender.GetCounter("responses.5xx") }).Should(BeEquivalentTo(2))
		})

		It("increments the XXX response metrics", func() {
			response := http.Response{
				StatusCode: 100,
			}

			metricsReporter.CaptureRoutingResponse(endpoint, &response, time.Now(), time.Millisecond)
			Eventually(func() uint64 { return sender.GetCounter("responses.xxx") }).Should(BeEquivalentTo(1))

			metricsReporter.CaptureRoutingResponse(endpoint, &response, time.Now(), time.Millisecond)
			Eventually(func() uint64 { return sender.GetCounter("responses.xxx") }).Should(BeEquivalentTo(2))
		})

		It("increments the XXX response metrics with null response", func() {
			metricsReporter.CaptureRoutingResponse(endpoint, nil, time.Now(), time.Millisecond)
			Eventually(func() uint64 { return sender.GetCounter("responses.xxx") }).Should(BeEquivalentTo(1))

			metricsReporter.CaptureRoutingResponse(endpoint, nil, time.Now(), time.Millisecond)
			Eventually(func() uint64 { return sender.GetCounter("responses.xxx") }).Should(BeEquivalentTo(2))
		})

		It("increments the total responses", func() {
			response2xx := http.Response{
				StatusCode: 205,
			}
			response4xx := http.Response{
				StatusCode: 401,
			}

			metricsReporter.CaptureRoutingResponse(endpoint, &response2xx, time.Now(), time.Millisecond)
			Eventually(func() uint64 { return sender.GetCounter("responses") }).Should(BeEquivalentTo(1))

			metricsReporter.CaptureRoutingResponse(endpoint, &response4xx, time.Now(), time.Millisecond)
			Eventually(func() uint64 { return sender.GetCounter("responses") }).Should(BeEquivalentTo(2))

		})

		It("sends the latency", func() {
			response := http.Response{
				StatusCode: 401,
			}

			metricsReporter.CaptureRoutingResponse(endpoint, &response, time.Now(), 2*time.Second)
			Eventually(func() fake.Metric { return sender.GetValue("latency") }).Should(Equal(
				fake.Metric{
					Value: 2000,
					Unit:  "ms",
				}))
		})

		It("sends the latency for the given component", func() {
			response := http.Response{
				StatusCode: 200,
			}

			endpoint.Tags["component"] = "CloudController"
			metricsReporter.CaptureRoutingResponse(endpoint, &response, time.Now(), 2*time.Second)
			Eventually(func() fake.Metric { return sender.GetValue("latency.CloudController") }).Should(Equal(
				fake.Metric{
					Value: 2000,
					Unit:  "ms",
				}))
		})
	})

	Context("sends route metrics", func() {
		var endpoint *route.Endpoint

		BeforeEach(func() {
			endpoint = new(route.Endpoint)
		})

		It("sends number of nats messages received from each component", func() {
			endpoint.Tags = map[string]string{"component": "uaa"}
			metricsReporter.CaptureRegistryMessage(endpoint)

			endpoint.Tags = map[string]string{"component": "route-emitter"}
			metricsReporter.CaptureRegistryMessage(endpoint)

			Eventually(func() uint64 { return sender.GetCounter("registry_message.route-emitter") }).Should(BeEquivalentTo(1))
			Eventually(func() uint64 { return sender.GetCounter("registry_message.uaa") }).Should(BeEquivalentTo(1))
		})

		It("sends the total routes", func() {
			metricsReporter.CaptureRouteStats(12, 5)
			Eventually(func() fake.Metric { return sender.GetValue("total_routes") }).Should(Equal(
				fake.Metric{
					Value: 12,
					Unit:  "",
				}))
		})

		It("sends the time since last update", func() {
			metricsReporter.CaptureRouteStats(12, 5)
			Eventually(func() fake.Metric { return sender.GetValue("ms_since_last_registry_update") }).Should(Equal(
				fake.Metric{
					Value: 5,
					Unit:  "ms",
				}))
		})
	})
})
