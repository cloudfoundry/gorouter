package handlers_test

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"time"

	"code.cloudfoundry.org/gorouter/handlers"
	"code.cloudfoundry.org/gorouter/logger"
	metrics_fakes "code.cloudfoundry.org/gorouter/metrics/fakes"
	"code.cloudfoundry.org/gorouter/route"
	"code.cloudfoundry.org/gorouter/test_util"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gbytes"
	"github.com/urfave/negroni/v3"
)

var _ = Describe("Reporter Handler", func() {
	var (
		handler     *negroni.Negroni
		nextHandler http.HandlerFunc

		resp http.ResponseWriter
		req  *http.Request

		fakeReporter *metrics_fakes.FakeCombinedReporter
		logger       logger.Logger
		prevHandler  negroni.Handler

		nextCalled bool
	)

	BeforeEach(func() {
		body := bytes.NewBufferString("What are you?")
		req = test_util.NewRequest("GET", "example.com", "/", body)
		resp = httptest.NewRecorder()

		fakeReporter = new(metrics_fakes.FakeCombinedReporter)
		logger = test_util.NewTestZapLogger("test")

		nextHandler = http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
			_, err := io.ReadAll(req.Body)
			Expect(err).NotTo(HaveOccurred())

			rw.WriteHeader(http.StatusTeapot)
			rw.Write([]byte("I'm a little teapot, short and stout."))

			reqInfo, err := handlers.ContextRequestInfo(req)
			Expect(err).NotTo(HaveOccurred())
			reqInfo.RouteEndpoint = route.NewEndpoint(&route.EndpointOpts{AppId: "appID", PrivateInstanceIndex: "1", PrivateInstanceId: "id"})
			reqInfo.AppRequestFinishedAt = time.Now()

			nextCalled = true
		})
		nextCalled = false
		prevHandler = &PrevHandler{}
	})

	JustBeforeEach(func() {
		handler = negroni.New()
		handler.Use(handlers.NewRequestInfo())
		handler.Use(handlers.NewProxyWriter(logger))
		handler.Use(prevHandler)
		handler.Use(handlers.NewReporter(fakeReporter, logger))
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
				_, err := io.ReadAll(req.Body)
				Expect(err).NotTo(HaveOccurred())

				rw.WriteHeader(http.StatusTeapot)
				rw.Write([]byte("I'm a little teapot, short and stout."))

				reqInfo, err := handlers.ContextRequestInfo(req)
				Expect(err).NotTo(HaveOccurred())
				reqInfo.RouteEndpoint = route.NewEndpoint(&route.EndpointOpts{AppId: "appID"})

				nextCalled = true
			})
		})
		It("emits the routing response status code, but does not emit a latency metric", func() {
			handler.ServeHTTP(resp, req)
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
				_, err := io.ReadAll(req.Body)
				Expect(err).NotTo(HaveOccurred())

				rw.WriteHeader(http.StatusTeapot)
				rw.Write([]byte("I'm a little teapot, short and stout."))

				reqInfo, err := handlers.ContextRequestInfo(req)
				Expect(err).NotTo(HaveOccurred())
				reqInfo.AppRequestFinishedAt = time.Now()
			})
		})

		It("does not emit routing response metrics", func() {
			handler.ServeHTTP(resp, req)
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
			badHandler.Use(handlers.NewReporter(fakeReporter, logger))
		})

		It("calls Panic on the logger", func() {
			defer func() {
				recover()
				Expect(logger).To(gbytes.Say(`"error":"RequestInfo not set on context"`))
				Expect(nextCalled).To(BeFalse())
			}()
			badHandler.ServeHTTP(resp, req)

			Expect(fakeReporter.CaptureBadGatewayCallCount()).To(Equal(0))
			Expect(fakeReporter.CaptureRoutingResponseCallCount()).To(Equal(0))
			Expect(fakeReporter.CaptureRoutingResponseLatencyCallCount()).To(Equal(0))

			Expect(nextCalled).To(BeFalse())
		})
	})
})
