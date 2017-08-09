package handlers_test

import (
	"bytes"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"time"

	"code.cloudfoundry.org/gorouter/handlers"
	logger_fakes "code.cloudfoundry.org/gorouter/logger/fakes"
	metrics_fakes "code.cloudfoundry.org/gorouter/metrics/fakes"
	"code.cloudfoundry.org/gorouter/route"
	"code.cloudfoundry.org/gorouter/test_util"
	"code.cloudfoundry.org/routing-api/models"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/urfave/negroni"
)

var _ = Describe("Reporter Handler", func() {
	var (
		handler     *negroni.Negroni
		nextHandler http.HandlerFunc

		resp http.ResponseWriter
		req  *http.Request

		fakeReporter *metrics_fakes.FakeCombinedReporter
		fakeLogger   *logger_fakes.FakeLogger

		nextCalled bool
	)

	BeforeEach(func() {
		body := bytes.NewBufferString("What are you?")
		req = test_util.NewRequest("GET", "example.com", "/", body)
		resp = httptest.NewRecorder()

		fakeReporter = new(metrics_fakes.FakeCombinedReporter)
		fakeLogger = new(logger_fakes.FakeLogger)

		nextHandler = http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
			_, err := ioutil.ReadAll(req.Body)
			Expect(err).NotTo(HaveOccurred())

			rw.WriteHeader(http.StatusTeapot)
			rw.Write([]byte("I'm a little teapot, short and stout."))

			reqInfo, err := handlers.ContextRequestInfo(req)
			Expect(err).NotTo(HaveOccurred())
			reqInfo.RouteEndpoint = route.NewEndpoint(
				"appID", "blah", uint16(1234), "id", "1", nil, 0, "",
				models.ModificationTag{}, "", false)
			reqInfo.StoppedAt = time.Now()

			nextCalled = true
		})
		nextCalled = false
	})

	JustBeforeEach(func() {
		handler = negroni.New()
		handler.Use(handlers.NewRequestInfo())
		handler.Use(handlers.NewProxyWriter(fakeLogger))
		handler.Use(handlers.NewReporter(fakeReporter, fakeLogger))
		handler.UseHandlerFunc(nextHandler)
	})

	It("emits routing response metrics", func() {
		handler.ServeHTTP(resp, req)

		Expect(fakeReporter.CaptureBadGatewayCallCount()).To(Equal(0))

		Expect(fakeReporter.CaptureRoutingResponseCallCount()).To(Equal(1))
		capturedRespCode := fakeReporter.CaptureRoutingResponseArgsForCall(0)
		Expect(capturedRespCode).To(Equal(http.StatusTeapot))

		Expect(fakeReporter.CaptureRoutingResponseLatencyCallCount()).To(Equal(1))
		capturedEndpoint, capturedRespCode, startTime, latency := fakeReporter.CaptureRoutingResponseLatencyArgsForCall(0)
		Expect(capturedEndpoint.ApplicationId).To(Equal("appID"))
		Expect(capturedEndpoint.PrivateInstanceId).To(Equal("id"))
		Expect(capturedEndpoint.PrivateInstanceIndex).To(Equal("1"))
		Expect(capturedRespCode).To(Equal(http.StatusTeapot))
		Expect(startTime).To(BeTemporally("~", time.Now(), 100*time.Millisecond))
		Expect(latency).To(BeNumerically(">", 0))
		Expect(latency).To(BeNumerically("<", 10*time.Millisecond))

		Expect(nextCalled).To(BeTrue(), "Expected the next handler to be called.")
	})

	Context("when reqInfo.StoppedAt is 0", func() {
		BeforeEach(func() {
			nextHandler = http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
				_, err := ioutil.ReadAll(req.Body)
				Expect(err).NotTo(HaveOccurred())

				rw.WriteHeader(http.StatusTeapot)
				rw.Write([]byte("I'm a little teapot, short and stout."))

				reqInfo, err := handlers.ContextRequestInfo(req)
				Expect(err).NotTo(HaveOccurred())
				reqInfo.RouteEndpoint = route.NewEndpoint(
					"appID", "blah", uint16(1234), "id", "1", nil, 0, "",
					models.ModificationTag{}, "", false)

				nextCalled = true
			})
		})
		It("emits the routing response status code, but does not emit a latency metric", func() {
			handler.ServeHTTP(resp, req)
			Expect(fakeLogger.ErrorCallCount()).To(Equal(0))
			Expect(fakeReporter.CaptureBadGatewayCallCount()).To(Equal(0))
			Expect(fakeReporter.CaptureRoutingResponseCallCount()).To(Equal(1))
			capturedRespCode := fakeReporter.CaptureRoutingResponseArgsForCall(0)
			Expect(capturedRespCode).To(Equal(http.StatusTeapot))

			Expect(fakeReporter.CaptureRoutingResponseLatencyCallCount()).To(Equal(0))

			Expect(nextCalled).To(BeTrue(), "Expected the next handler to be called.")
		})
	})

	Context("when endpoint is nil", func() {
		BeforeEach(func() {
			nextHandler = http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
				_, err := ioutil.ReadAll(req.Body)
				Expect(err).NotTo(HaveOccurred())

				rw.WriteHeader(http.StatusTeapot)
				rw.Write([]byte("I'm a little teapot, short and stout."))

				reqInfo, err := handlers.ContextRequestInfo(req)
				Expect(err).NotTo(HaveOccurred())
				reqInfo.StoppedAt = time.Now()
			})
		})
		It("does not emit routing response metrics", func() {
			handler.ServeHTTP(resp, req)
			Expect(fakeLogger.ErrorCallCount()).To(Equal(0))
			Expect(fakeReporter.CaptureBadGatewayCallCount()).To(Equal(0))
			Expect(fakeReporter.CaptureRoutingResponseCallCount()).To(Equal(0))
			Expect(fakeReporter.CaptureRoutingResponseLatencyCallCount()).To(Equal(0))

			Expect(nextCalled).To(BeFalse())
		})
	})

	Context("when request info is not set on the request context", func() {
		var badHandler *negroni.Negroni
		BeforeEach(func() {
			badHandler = negroni.New()
			badHandler.Use(handlers.NewReporter(fakeReporter, fakeLogger))
		})
		It("calls Fatal on the logger", func() {
			badHandler.ServeHTTP(resp, req)
			Expect(fakeLogger.FatalCallCount()).To(Equal(1))
			Expect(fakeReporter.CaptureBadGatewayCallCount()).To(Equal(0))
			Expect(fakeReporter.CaptureRoutingResponseCallCount()).To(Equal(0))
			Expect(fakeReporter.CaptureRoutingResponseLatencyCallCount()).To(Equal(0))

			Expect(nextCalled).To(BeFalse())
		})
	})
})
