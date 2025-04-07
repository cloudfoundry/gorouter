package metrics_test

import (
	"net/http"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"code.cloudfoundry.org/gorouter/metrics"
	"code.cloudfoundry.org/gorouter/metrics/fakes"
	"code.cloudfoundry.org/gorouter/route"
)

var _ = Describe("CompositeReporter", func() {

	var fakeVarzReporter *fakes.FakeVarzReporter
	var fakeMultiReporter *fakes.FakeMetricReporter
	var composite metrics.MetricReporter

	var endpoint *route.Endpoint
	var response *http.Response
	var responseTime time.Time
	var responseDuration time.Duration

	BeforeEach(func() {
		fakeVarzReporter = new(fakes.FakeVarzReporter)
		fakeMultiReporter = new(fakes.FakeMetricReporter)

		composite = &metrics.CompositeReporter{VarzReporter: fakeVarzReporter, MetricReporter: fakeMultiReporter}
		endpoint = route.NewEndpoint(&route.EndpointOpts{})
		response = &http.Response{StatusCode: 200}
		responseTime = time.Now()
		responseDuration = time.Second
	})

	It("forwards CaptureBadRequest to both reporters", func() {
		composite.CaptureBadRequest()

		Expect(fakeVarzReporter.CaptureBadRequestCallCount()).To(Equal(1))
		Expect(fakeMultiReporter.CaptureBadRequestCallCount()).To(Equal(1))
	})

	It("forwards CaptureBackendExhaustedConns to the proxy reporter", func() {
		composite.CaptureBackendExhaustedConns()
		Expect(fakeMultiReporter.CaptureBackendExhaustedConnsCallCount()).To(Equal(1))
	})

	It("forwards CaptureBackendInvalidID() to the proxy reporter", func() {
		composite.CaptureBackendInvalidID()
		Expect(fakeMultiReporter.CaptureBackendInvalidIDCallCount()).To(Equal(1))
	})

	It("forwards CaptureBackendInvalidTLSCert() to the proxy reporter", func() {
		composite.CaptureBackendInvalidTLSCert()
		Expect(fakeMultiReporter.CaptureBackendInvalidTLSCertCallCount()).To(Equal(1))
	})

	It("forwards CaptureBadGateway to both reporters", func() {
		composite.CaptureBadGateway()
		Expect(fakeVarzReporter.CaptureBadGatewayCallCount()).To(Equal(1))
		Expect(fakeMultiReporter.CaptureBadGatewayCallCount()).To(Equal(1))
	})

	It("forwards CaptureRoutingRequest to both reporters", func() {
		composite.CaptureRoutingRequest(endpoint)
		Expect(fakeVarzReporter.CaptureRoutingRequestCallCount()).To(Equal(1))
		Expect(fakeMultiReporter.CaptureRoutingRequestCallCount()).To(Equal(1))

		callEndpoint := fakeVarzReporter.CaptureRoutingRequestArgsForCall(0)
		Expect(callEndpoint).To(Equal(endpoint))

		callEndpoint = fakeMultiReporter.CaptureRoutingRequestArgsForCall(0)
		Expect(callEndpoint).To(Equal(endpoint))
	})

	It("forwards CaptureRoutingResponseLatency to both reporters", func() {
		composite.CaptureRoutingResponseLatency(endpoint, response.StatusCode, responseTime, responseDuration)

		Expect(fakeVarzReporter.CaptureRoutingResponseLatencyCallCount()).To(Equal(1))
		Expect(fakeMultiReporter.CaptureRoutingResponseLatencyCallCount()).To(Equal(1))

		callEndpoint, callStatusCode, callTime, callDuration := fakeVarzReporter.CaptureRoutingResponseLatencyArgsForCall(0)
		Expect(callEndpoint).To(Equal(endpoint))
		Expect(callStatusCode).To(Equal(response.StatusCode))
		Expect(callTime).To(Equal(responseTime))
		Expect(callDuration).To(Equal(responseDuration))

		callEndpoint, _, _, callDuration = fakeMultiReporter.CaptureRoutingResponseLatencyArgsForCall(0)
		Expect(callEndpoint).To(Equal(endpoint))
		Expect(callDuration).To(Equal(responseDuration))
	})

	It("forwards CaptureGorouterTime to Multireporter", func() {
		composite.CaptureGorouterTime(3000)

		Expect(fakeMultiReporter.CaptureGorouterTimeCallCount()).To(Equal(1))
		value := fakeMultiReporter.CaptureGorouterTimeArgsForCall(0)
		Expect(value).To(BeEquivalentTo(3000))
	})

	It("forwards CaptureRoutingServiceResponse to proxy reporter", func() {
		composite.CaptureRouteServiceResponse(response)

		Expect(fakeMultiReporter.CaptureRouteServiceResponseCallCount()).To(Equal(1))

		callResponse := fakeMultiReporter.CaptureRouteServiceResponseArgsForCall(0)
		Expect(callResponse).To(Equal(response))
	})

	It("forwards CaptureRoutingResponse to proxy reporter", func() {
		composite.CaptureRoutingResponse(response.StatusCode)

		Expect(fakeMultiReporter.CaptureRoutingResponseCallCount()).To(Equal(1))

		callResponseCode := fakeMultiReporter.CaptureRoutingResponseArgsForCall(0)
		Expect(callResponseCode).To(Equal(response.StatusCode))
	})

	It("forwards CaptureWebSocketUpdate to proxy reporter", func() {
		composite.CaptureWebSocketUpdate()

		Expect(fakeMultiReporter.CaptureWebSocketUpdateCallCount()).To(Equal(1))
	})

	It("forwards CaptureWebSocketFailure to proxy reporter", func() {
		composite.CaptureWebSocketFailure()

		Expect(fakeMultiReporter.CaptureWebSocketFailureCallCount()).To(Equal(1))
	})

	It("forwards CaptureHTTPLatency to the proxy reporter", func() {
		composite.CaptureHTTPLatency(time.Second, "")
		Expect(fakeMultiReporter.CaptureHTTPLatencyCallCount()).To(Equal(1))
	})

})
