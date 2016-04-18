package metrics_test

import (
	"github.com/cloudfoundry/gorouter/metrics/reporter"
	"github.com/cloudfoundry/gorouter/metrics/reporter/fakes"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	"net/http"
	"time"

	"github.com/cloudfoundry/gorouter/metrics"
	"github.com/cloudfoundry/gorouter/route"
)

var _ = Describe("CompositeReporter", func() {

	var fakeReporter1 *fakes.FakeProxyReporter
	var fakeReporter2 *fakes.FakeProxyReporter
	var composite reporter.ProxyReporter

	var req *http.Request
	var endpoint *route.Endpoint
	var response *http.Response
	var responseTime time.Time
	var responseDuration time.Duration

	BeforeEach(func() {
		fakeReporter1 = new(fakes.FakeProxyReporter)
		fakeReporter2 = new(fakes.FakeProxyReporter)

		composite = metrics.NewCompositeReporter(fakeReporter1, fakeReporter2)
		req, _ = http.NewRequest("GET", "https://example.com", nil)
		endpoint = route.NewEndpoint("someId", "host", 2222, "privateId", map[string]string{}, 30, "")
		response = &http.Response{StatusCode: 200}
		responseTime = time.Now()
		responseDuration = time.Second
	})

	It("forwards CaptureBadRequest to both reporters", func() {
		composite.CaptureBadRequest(req)

		Expect(fakeReporter1.CaptureBadRequestCallCount()).To(Equal(1))
		Expect(fakeReporter2.CaptureBadRequestCallCount()).To(Equal(1))

		Expect(fakeReporter1.CaptureBadRequestArgsForCall(0)).To(Equal(req))
		Expect(fakeReporter2.CaptureBadRequestArgsForCall(0)).To(Equal(req))
	})

	It("forwards CaptureBadGateway to both reporters", func() {
		composite.CaptureBadGateway(req)
		Expect(fakeReporter1.CaptureBadGatewayCallCount()).To(Equal(1))
		Expect(fakeReporter2.CaptureBadGatewayCallCount()).To(Equal(1))

		Expect(fakeReporter1.CaptureBadGatewayArgsForCall(0)).To(Equal(req))
		Expect(fakeReporter2.CaptureBadGatewayArgsForCall(0)).To(Equal(req))
	})

	It("forwards CaptureRoutingRequest to both reporters", func() {
		composite.CaptureRoutingRequest(endpoint, req)
		Expect(fakeReporter1.CaptureRoutingRequestCallCount()).To(Equal(1))
		Expect(fakeReporter2.CaptureRoutingRequestCallCount()).To(Equal(1))

		callEndpoint, callReq := fakeReporter1.CaptureRoutingRequestArgsForCall(0)
		Expect(callEndpoint).To(Equal(endpoint))
		Expect(callReq).To(Equal(req))

		callEndpoint, callReq = fakeReporter2.CaptureRoutingRequestArgsForCall(0)
		Expect(callEndpoint).To(Equal(endpoint))
		Expect(callReq).To(Equal(req))
	})

	It("forwards CaptureRoutingResponse to both reporters", func() {
		composite.CaptureRoutingResponse(endpoint, response, responseTime, responseDuration)

		Expect(fakeReporter1.CaptureRoutingResponseCallCount()).To(Equal(1))
		Expect(fakeReporter2.CaptureRoutingResponseCallCount()).To(Equal(1))

		callEndpoint, callResponse, callTime, callDuration := fakeReporter1.CaptureRoutingResponseArgsForCall(0)
		Expect(callEndpoint).To(Equal(endpoint))
		Expect(callResponse).To(Equal(response))
		Expect(callTime).To(Equal(responseTime))
		Expect(callDuration).To(Equal(responseDuration))

		callEndpoint, callResponse, callTime, callDuration = fakeReporter2.CaptureRoutingResponseArgsForCall(0)
		Expect(callEndpoint).To(Equal(endpoint))
		Expect(callResponse).To(Equal(response))
		Expect(callTime).To(Equal(responseTime))
		Expect(callDuration).To(Equal(responseDuration))
	})
})
