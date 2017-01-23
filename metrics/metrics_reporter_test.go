package metrics_test

import (
	"net/http"
	"time"

	"code.cloudfoundry.org/gorouter/metrics"
	"code.cloudfoundry.org/gorouter/metrics/fakes"
	"code.cloudfoundry.org/routing-api/models"

	"code.cloudfoundry.org/gorouter/route"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("MetricsReporter", func() {
	var (
		endpoint       *route.Endpoint
		sender         *fakes.MetricSender
		batcher        *fakes.MetricBatcher
		metricReporter *metrics.MetricsReporter
	)

	BeforeEach(func() {
		endpoint = route.NewEndpoint("someId", "host", 2222, "privateId", "2", map[string]string{}, 30, "", models.ModificationTag{})
		sender = new(fakes.MetricSender)
		batcher = new(fakes.MetricBatcher)
		metricReporter = metrics.NewMetricsReporter(sender, batcher)
	})

	It("increments the bad_requests metric", func() {
		metricReporter.CaptureBadRequest()

		Expect(batcher.BatchIncrementCounterCallCount()).To(Equal(1))
		Expect(batcher.BatchIncrementCounterArgsForCall(0)).To(Equal("rejected_requests"))

		metricReporter.CaptureBadRequest()

		Expect(batcher.BatchIncrementCounterCallCount()).To(Equal(2))
		Expect(batcher.BatchIncrementCounterArgsForCall(1)).To(Equal("rejected_requests"))
	})

	It("increments the bad_gateway metric", func() {
		metricReporter.CaptureBadGateway()

		Expect(batcher.BatchIncrementCounterCallCount()).To(Equal(1))
		Expect(batcher.BatchIncrementCounterArgsForCall(0)).To(Equal("bad_gateways"))

		metricReporter.CaptureBadGateway()

		Expect(batcher.BatchIncrementCounterCallCount()).To(Equal(2))
		Expect(batcher.BatchIncrementCounterArgsForCall(1)).To(Equal("bad_gateways"))
	})

	Context("increments the request metrics", func() {
		It("increments the total requests metric", func() {
			metricReporter.CaptureRoutingRequest(&route.Endpoint{})

			Expect(batcher.BatchIncrementCounterCallCount()).To(Equal(1))
			Expect(batcher.BatchIncrementCounterArgsForCall(0)).To(Equal("total_requests"))

			metricReporter.CaptureRoutingRequest(&route.Endpoint{})

			Expect(batcher.BatchIncrementCounterCallCount()).To(Equal(2))
			Expect(batcher.BatchIncrementCounterArgsForCall(0)).To(Equal("total_requests"))
		})

		It("increments the requests metric for the given component", func() {
			endpoint.Tags["component"] = "CloudController"
			metricReporter.CaptureRoutingRequest(endpoint)

			Expect(batcher.BatchIncrementCounterCallCount()).To(Equal(2))
			Expect(batcher.BatchIncrementCounterArgsForCall(1)).To(Equal("requests.CloudController"))

			metricReporter.CaptureRoutingRequest(endpoint)

			Expect(batcher.BatchIncrementCounterCallCount()).To(Equal(4))
			Expect(batcher.BatchIncrementCounterArgsForCall(3)).To(Equal("requests.CloudController"))

			endpoint.Tags["component"] = "UAA"
			metricReporter.CaptureRoutingRequest(endpoint)

			Expect(batcher.BatchIncrementCounterCallCount()).To(Equal(6))
			Expect(batcher.BatchIncrementCounterArgsForCall(5)).To(Equal("requests.UAA"))

			metricReporter.CaptureRoutingRequest(endpoint)

			Expect(batcher.BatchIncrementCounterCallCount()).To(Equal(8))
			Expect(batcher.BatchIncrementCounterArgsForCall(7)).To(Equal("requests.UAA"))
		})

		It("increments the routed_app_requests metric", func() {
			endpoint.Tags["component"] = "dea-1"
			metricReporter.CaptureRoutingRequest(endpoint)

			Expect(batcher.BatchIncrementCounterCallCount()).To(Equal(3))
			Expect(batcher.BatchIncrementCounterArgsForCall(2)).To(Equal("routed_app_requests"))

			endpoint.Tags["component"] = "dea-3"
			metricReporter.CaptureRoutingRequest(endpoint)

			Expect(batcher.BatchIncrementCounterCallCount()).To(Equal(6))
			Expect(batcher.BatchIncrementCounterArgsForCall(5)).To(Equal("routed_app_requests"))

			endpoint.Tags["component"] = "CloudController"
			metricReporter.CaptureRoutingRequest(endpoint)

			Expect(batcher.BatchIncrementCounterCallCount()).To(Equal(8))
		})
	})

	Context("increments the response metrics for route services", func() {
		It("increments the 2XX route services response metrics", func() {
			response := http.Response{
				StatusCode: 200,
			}

			metricReporter.CaptureRouteServiceResponse(&response)
			Expect(batcher.BatchIncrementCounterCallCount()).To(Equal(2))
			Expect(batcher.BatchIncrementCounterArgsForCall(0)).To(Equal("responses.route_services.2xx"))

			metricReporter.CaptureRouteServiceResponse(&response)
			Expect(batcher.BatchIncrementCounterCallCount()).To(Equal(4))
			Expect(batcher.BatchIncrementCounterArgsForCall(2)).To(Equal("responses.route_services.2xx"))
		})

		It("increments the 3XX response metrics", func() {
			response := http.Response{
				StatusCode: 304,
			}

			metricReporter.CaptureRouteServiceResponse(&response)
			Expect(batcher.BatchIncrementCounterCallCount()).To(Equal(2))
			Expect(batcher.BatchIncrementCounterArgsForCall(0)).To(Equal("responses.route_services.3xx"))

			metricReporter.CaptureRouteServiceResponse(&response)
			Expect(batcher.BatchIncrementCounterCallCount()).To(Equal(4))
			Expect(batcher.BatchIncrementCounterArgsForCall(2)).To(Equal("responses.route_services.3xx"))
		})

		It("increments the 4XX response metrics", func() {
			response := http.Response{
				StatusCode: 401,
			}

			metricReporter.CaptureRouteServiceResponse(&response)
			Expect(batcher.BatchIncrementCounterCallCount()).To(Equal(2))
			Expect(batcher.BatchIncrementCounterArgsForCall(0)).To(Equal("responses.route_services.4xx"))

			metricReporter.CaptureRouteServiceResponse(&response)
			Expect(batcher.BatchIncrementCounterCallCount()).To(Equal(4))
			Expect(batcher.BatchIncrementCounterArgsForCall(2)).To(Equal("responses.route_services.4xx"))
		})

		It("increments the 5XX response metrics", func() {
			response := http.Response{
				StatusCode: 504,
			}

			metricReporter.CaptureRouteServiceResponse(&response)
			Expect(batcher.BatchIncrementCounterCallCount()).To(Equal(2))
			Expect(batcher.BatchIncrementCounterArgsForCall(0)).To(Equal("responses.route_services.5xx"))

			metricReporter.CaptureRouteServiceResponse(&response)
			Expect(batcher.BatchIncrementCounterCallCount()).To(Equal(4))
			Expect(batcher.BatchIncrementCounterArgsForCall(2)).To(Equal("responses.route_services.5xx"))
		})

		It("increments the XXX response metrics", func() {
			response := http.Response{
				StatusCode: 100,
			}

			metricReporter.CaptureRouteServiceResponse(&response)
			Expect(batcher.BatchIncrementCounterCallCount()).To(Equal(2))
			Expect(batcher.BatchIncrementCounterArgsForCall(0)).To(Equal("responses.route_services.xxx"))

			metricReporter.CaptureRouteServiceResponse(&response)
			Expect(batcher.BatchIncrementCounterCallCount()).To(Equal(4))
			Expect(batcher.BatchIncrementCounterArgsForCall(2)).To(Equal("responses.route_services.xxx"))
		})

		It("increments the XXX response metrics with null response", func() {
			metricReporter.CaptureRouteServiceResponse(nil)
			Expect(batcher.BatchIncrementCounterCallCount()).To(Equal(2))
			Expect(batcher.BatchIncrementCounterArgsForCall(0)).To(Equal("responses.route_services.xxx"))

			metricReporter.CaptureRouteServiceResponse(nil)
			Expect(batcher.BatchIncrementCounterCallCount()).To(Equal(4))
			Expect(batcher.BatchIncrementCounterArgsForCall(2)).To(Equal("responses.route_services.xxx"))
		})

		It("increments the total responses", func() {
			response2xx := http.Response{
				StatusCode: 205,
			}
			response4xx := http.Response{
				StatusCode: 401,
			}

			metricReporter.CaptureRouteServiceResponse(&response2xx)
			Expect(batcher.BatchIncrementCounterCallCount()).To(Equal(2))
			Expect(batcher.BatchIncrementCounterArgsForCall(1)).To(Equal("responses.route_services"))

			metricReporter.CaptureRouteServiceResponse(&response4xx)
			Expect(batcher.BatchIncrementCounterCallCount()).To(Equal(4))
			Expect(batcher.BatchIncrementCounterArgsForCall(3)).To(Equal("responses.route_services"))
		})
	})

	Context("increments the response metrics", func() {
		It("increments the 2XX response metrics", func() {
			response := http.Response{
				StatusCode: 200,
			}

			metricReporter.CaptureRoutingResponse(endpoint, &response, time.Now(), time.Millisecond)
			Expect(batcher.BatchIncrementCounterCallCount()).To(Equal(2))
			Expect(batcher.BatchIncrementCounterArgsForCall(0)).To(Equal("responses.2xx"))

			metricReporter.CaptureRoutingResponse(endpoint, &response, time.Now(), time.Millisecond)
			Expect(batcher.BatchIncrementCounterCallCount()).To(Equal(4))
			Expect(batcher.BatchIncrementCounterArgsForCall(2)).To(Equal("responses.2xx"))
		})

		It("increments the 3XX response metrics", func() {
			response := http.Response{
				StatusCode: 304,
			}

			metricReporter.CaptureRoutingResponse(endpoint, &response, time.Now(), time.Millisecond)
			Expect(batcher.BatchIncrementCounterCallCount()).To(Equal(2))
			Expect(batcher.BatchIncrementCounterArgsForCall(0)).To(Equal("responses.3xx"))

			metricReporter.CaptureRoutingResponse(endpoint, &response, time.Now(), time.Millisecond)
			Expect(batcher.BatchIncrementCounterCallCount()).To(Equal(4))
			Expect(batcher.BatchIncrementCounterArgsForCall(2)).To(Equal("responses.3xx"))
		})

		It("increments the 4XX response metrics", func() {
			response := http.Response{
				StatusCode: 401,
			}

			metricReporter.CaptureRoutingResponse(endpoint, &response, time.Now(), time.Millisecond)
			Expect(batcher.BatchIncrementCounterCallCount()).To(Equal(2))
			Expect(batcher.BatchIncrementCounterArgsForCall(0)).To(Equal("responses.4xx"))

			metricReporter.CaptureRoutingResponse(endpoint, &response, time.Now(), time.Millisecond)
			Expect(batcher.BatchIncrementCounterCallCount()).To(Equal(4))
			Expect(batcher.BatchIncrementCounterArgsForCall(2)).To(Equal("responses.4xx"))
		})

		It("increments the 5XX response metrics", func() {
			response := http.Response{
				StatusCode: 504,
			}

			metricReporter.CaptureRoutingResponse(endpoint, &response, time.Now(), time.Millisecond)
			Expect(batcher.BatchIncrementCounterCallCount()).To(Equal(2))
			Expect(batcher.BatchIncrementCounterArgsForCall(0)).To(Equal("responses.5xx"))

			metricReporter.CaptureRoutingResponse(endpoint, &response, time.Now(), time.Millisecond)
			Expect(batcher.BatchIncrementCounterCallCount()).To(Equal(4))
			Expect(batcher.BatchIncrementCounterArgsForCall(2)).To(Equal("responses.5xx"))
		})

		It("increments the XXX response metrics", func() {
			response := http.Response{
				StatusCode: 100,
			}

			metricReporter.CaptureRoutingResponse(endpoint, &response, time.Now(), time.Millisecond)
			Expect(batcher.BatchIncrementCounterCallCount()).To(Equal(2))
			Expect(batcher.BatchIncrementCounterArgsForCall(0)).To(Equal("responses.xxx"))

			metricReporter.CaptureRoutingResponse(endpoint, &response, time.Now(), time.Millisecond)
			Expect(batcher.BatchIncrementCounterCallCount()).To(Equal(4))
			Expect(batcher.BatchIncrementCounterArgsForCall(2)).To(Equal("responses.xxx"))
		})

		It("increments the XXX response metrics with null response", func() {
			metricReporter.CaptureRoutingResponse(endpoint, nil, time.Now(), time.Millisecond)
			Expect(batcher.BatchIncrementCounterCallCount()).To(Equal(2))
			Expect(batcher.BatchIncrementCounterArgsForCall(0)).To(Equal("responses.xxx"))

			metricReporter.CaptureRoutingResponse(endpoint, nil, time.Now(), time.Millisecond)
			Expect(batcher.BatchIncrementCounterCallCount()).To(Equal(4))
			Expect(batcher.BatchIncrementCounterArgsForCall(2)).To(Equal("responses.xxx"))
		})

		It("increments the total responses", func() {
			response2xx := http.Response{
				StatusCode: 205,
			}
			response4xx := http.Response{
				StatusCode: 401,
			}

			metricReporter.CaptureRoutingResponse(endpoint, &response2xx, time.Now(), time.Millisecond)
			Expect(batcher.BatchIncrementCounterCallCount()).To(Equal(2))
			Expect(batcher.BatchIncrementCounterArgsForCall(1)).To(Equal("responses"))

			metricReporter.CaptureRoutingResponse(endpoint, &response4xx, time.Now(), time.Millisecond)
			Expect(batcher.BatchIncrementCounterCallCount()).To(Equal(4))
			Expect(batcher.BatchIncrementCounterArgsForCall(3)).To(Equal("responses"))
		})
	})

	It("sends the latency", func() {
		response := http.Response{
			StatusCode: 401,
		}

		metricReporter.CaptureRoutingResponse(endpoint, &response, time.Now(), 2*time.Second)

		Expect(sender.SendValueCallCount()).To(Equal(1))
		name, value, unit := sender.SendValueArgsForCall(0)
		Expect(name).To(Equal("latency"))
		Expect(value).To(BeEquivalentTo(2000))
		Expect(unit).To(Equal("ms"))

	})

	It("sends the latency for the given component", func() {
		response := http.Response{
			StatusCode: 200,
		}

		endpoint.Tags["component"] = "CloudController"
		metricReporter.CaptureRoutingResponse(endpoint, &response, time.Now(), 2*time.Second)

		Expect(sender.SendValueCallCount()).To(Equal(2))
		name, value, unit := sender.SendValueArgsForCall(1)
		Expect(name).To(Equal("latency.CloudController"))
		Expect(value).To(BeEquivalentTo(2000))
		Expect(unit).To(Equal("ms"))
	})

	Context("sends route metrics", func() {
		var endpoint *route.Endpoint

		BeforeEach(func() {
			endpoint = new(route.Endpoint)
		})

		It("sends number of nats messages received from each component", func() {
			endpoint.Tags = map[string]string{"component": "uaa"}
			metricReporter.CaptureRegistryMessage(endpoint)

			endpoint.Tags = map[string]string{"component": "route-emitter"}
			metricReporter.CaptureRegistryMessage(endpoint)

			Expect(sender.IncrementCounterCallCount()).To(Equal(2))
			Expect(sender.IncrementCounterArgsForCall(0)).To(Equal("registry_message.uaa"))
			Expect(sender.IncrementCounterArgsForCall(1)).To(Equal("registry_message.route-emitter"))
		})

		It("sends the total routes", func() {
			metricReporter.CaptureRouteStats(12, 5)

			Expect(sender.SendValueCallCount()).To(Equal(2))
			name, value, unit := sender.SendValueArgsForCall(0)
			Expect(name).To(Equal("total_routes"))
			Expect(value).To(BeEquivalentTo(12))
			Expect(unit).To(Equal(""))
		})

		It("sends the time since last update", func() {
			metricReporter.CaptureRouteStats(12, 5)

			Expect(sender.SendValueCallCount()).To(Equal(2))
			name, value, unit := sender.SendValueArgsForCall(1)
			Expect(name).To(Equal("ms_since_last_registry_update"))
			Expect(value).To(BeEquivalentTo(5))
			Expect(unit).To(Equal("ms"))
		})

		It("sends the lookup time for routing table", func() {
			metricReporter.CaptureLookupTime(time.Duration(9) * time.Second)

			Expect(sender.SendValueCallCount()).To(Equal(1))
			name, value, unit := sender.SendValueArgsForCall(0)
			Expect(name).To(Equal("route_lookup_time"))
			Expect(value).To(BeEquivalentTo(9000000000))
			Expect(unit).To(Equal("ns"))
		})
	})
})
