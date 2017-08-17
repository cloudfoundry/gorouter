package metrics_test

import (
	"code.cloudfoundry.org/gorouter/metrics/fakes"
	"code.cloudfoundry.org/routing-api/models"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	"net/http"
	"time"

	"code.cloudfoundry.org/gorouter/metrics"
	"code.cloudfoundry.org/gorouter/route"
)

var _ = Describe("CompositeReporter", func() {

	var fakeVarzReporter *fakes.FakeVarzReporter
	var fakeProxyReporter *fakes.FakeProxyReporter
	var composite metrics.CombinedReporter

	var endpoint *route.Endpoint
	var response *http.Response
	var responseTime time.Time
	var responseDuration time.Duration

	BeforeEach(func() {
		fakeVarzReporter = new(fakes.FakeVarzReporter)
		fakeProxyReporter = new(fakes.FakeProxyReporter)

		composite = metrics.NewCompositeReporter(fakeVarzReporter, fakeProxyReporter)
		endpoint = route.NewEndpoint("someId", "host", 2222, "privateId", "2", map[string]string{}, 30, "", models.ModificationTag{}, "", false)
		response = &http.Response{StatusCode: 200}
		responseTime = time.Now()
		responseDuration = time.Second
	})

	It("forwards CaptureBadRequest to both reporters", func() {
		composite.CaptureBadRequest()

		Expect(fakeVarzReporter.CaptureBadRequestCallCount()).To(Equal(1))
		Expect(fakeProxyReporter.CaptureBadRequestCallCount()).To(Equal(1))
	})

	It("forwards CaptureBackendExhaustedConns to the proxy reporter", func() {
		composite.CaptureBackendExhaustedConns()
		Expect(fakeProxyReporter.CaptureBackendExhaustedConnsCallCount()).To(Equal(1))
	})

	It("forwards CaptureBadGateway to both reporters", func() {
		composite.CaptureBadGateway()
		Expect(fakeVarzReporter.CaptureBadGatewayCallCount()).To(Equal(1))
		Expect(fakeProxyReporter.CaptureBadGatewayCallCount()).To(Equal(1))
	})

	It("forwards CaptureRoutingRequest to both reporters", func() {
		composite.CaptureRoutingRequest(endpoint)
		Expect(fakeVarzReporter.CaptureRoutingRequestCallCount()).To(Equal(1))
		Expect(fakeProxyReporter.CaptureRoutingRequestCallCount()).To(Equal(1))

		callEndpoint := fakeVarzReporter.CaptureRoutingRequestArgsForCall(0)
		Expect(callEndpoint).To(Equal(endpoint))

		callEndpoint = fakeProxyReporter.CaptureRoutingRequestArgsForCall(0)
		Expect(callEndpoint).To(Equal(endpoint))
	})

	It("forwards CaptureRoutingResponseLatency to both reporters", func() {
		composite.CaptureRoutingResponseLatency(endpoint, response.StatusCode, responseTime, responseDuration)

		Expect(fakeVarzReporter.CaptureRoutingResponseLatencyCallCount()).To(Equal(1))
		Expect(fakeProxyReporter.CaptureRoutingResponseLatencyCallCount()).To(Equal(1))

		callEndpoint, callStatusCode, callTime, callDuration := fakeVarzReporter.CaptureRoutingResponseLatencyArgsForCall(0)
		Expect(callEndpoint).To(Equal(endpoint))
		Expect(callStatusCode).To(Equal(response.StatusCode))
		Expect(callTime).To(Equal(responseTime))
		Expect(callDuration).To(Equal(responseDuration))

		callEndpoint, callDuration = fakeProxyReporter.CaptureRoutingResponseLatencyArgsForCall(0)
		Expect(callEndpoint).To(Equal(endpoint))
		Expect(callDuration).To(Equal(responseDuration))
	})

	It("forwards CaptureRoutingServiceResponse to proxy reporter", func() {
		composite.CaptureRouteServiceResponse(response)

		Expect(fakeProxyReporter.CaptureRouteServiceResponseCallCount()).To(Equal(1))

		callResponse := fakeProxyReporter.CaptureRouteServiceResponseArgsForCall(0)
		Expect(callResponse).To(Equal(response))
	})

	It("forwards CaptureRoutingResponse to proxy reporter", func() {
		composite.CaptureRoutingResponse(response.StatusCode)

		Expect(fakeProxyReporter.CaptureRoutingResponseCallCount()).To(Equal(1))

		callResponseCode := fakeProxyReporter.CaptureRoutingResponseArgsForCall(0)
		Expect(callResponseCode).To(Equal(response.StatusCode))
	})

	It("forwards CaptureWebSocketUpdate to proxy reporter", func() {
		composite.CaptureWebSocketUpdate()

		Expect(fakeProxyReporter.CaptureWebSocketUpdateCallCount()).To(Equal(1))
	})

	It("forwards CaptureWebSocketFailure to proxy reporter", func() {
		composite.CaptureWebSocketFailure()

		Expect(fakeProxyReporter.CaptureWebSocketFailureCallCount()).To(Equal(1))
	})
})
