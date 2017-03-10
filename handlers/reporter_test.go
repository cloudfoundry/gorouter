package handlers_test

import (
	"bytes"
	"context"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"time"

	"code.cloudfoundry.org/gorouter/access_log/schema"
	"code.cloudfoundry.org/gorouter/handlers"
	logger_fakes "code.cloudfoundry.org/gorouter/logger/fakes"
	metrics_fakes "code.cloudfoundry.org/gorouter/metrics/fakes"
	"code.cloudfoundry.org/gorouter/proxy/utils"
	"code.cloudfoundry.org/gorouter/route"
	"code.cloudfoundry.org/gorouter/test_util"
	"code.cloudfoundry.org/routing-api/models"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/urfave/negroni"
)

var _ = Describe("Reporter Handler", func() {
	var (
		handler negroni.Handler

		resp        http.ResponseWriter
		proxyWriter utils.ProxyResponseWriter
		req         *http.Request

		fakeReporter *metrics_fakes.FakeCombinedReporter
		fakeLogger   *logger_fakes.FakeLogger

		nextCalled bool
	)

	nextHandler := http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		_, err := ioutil.ReadAll(req.Body)
		Expect(err).NotTo(HaveOccurred())

		rw.WriteHeader(http.StatusTeapot)
		rw.Write([]byte("I'm a little teapot, short and stout."))

		nextCalled = true
	})

	alrHandler := http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		alr := req.Context().Value("AccessLogRecord")
		Expect(alr).ToNot(BeNil())
		accessLog := alr.(*schema.AccessLogRecord)
		accessLog.RouteEndpoint = route.NewEndpoint(
			"appID", "blah", uint16(1234), "id", "1", nil, 0, "",
			models.ModificationTag{})

		nextHandler(rw, req)
	})

	BeforeEach(func() {
		body := bytes.NewBufferString("What are you?")
		req = test_util.NewRequest("GET", "example.com", "/", body)
		resp = httptest.NewRecorder()
		proxyWriter = utils.NewProxyResponseWriter(resp)

		alr := &schema.AccessLogRecord{
			StartedAt: time.Now(),
		}
		req = req.WithContext(context.WithValue(req.Context(), "AccessLogRecord", alr))

		fakeReporter = new(metrics_fakes.FakeCombinedReporter)
		fakeLogger = new(logger_fakes.FakeLogger)
		handler = handlers.NewReporter(fakeReporter, fakeLogger)

		nextCalled = false
	})

	AfterEach(func() {
		Expect(nextCalled).To(BeTrue(), "Expected the next handler to be called.")
	})

	It("emits routing response metrics", func() {
		handler.ServeHTTP(proxyWriter, req, alrHandler)

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
	})

	Context("when endpoint is nil", func() {
		It("does not emit routing response metrics", func() {
			handler.ServeHTTP(proxyWriter, req, nextHandler)
			Expect(fakeLogger.ErrorCallCount()).To(Equal(0))
			Expect(fakeReporter.CaptureBadGatewayCallCount()).To(Equal(0))
			Expect(fakeReporter.CaptureRoutingResponseCallCount()).To(Equal(0))
			Expect(fakeReporter.CaptureRoutingResponseLatencyCallCount()).To(Equal(0))
		})
	})

	Context("when access log record is not set on the request context", func() {
		BeforeEach(func() {
			body := bytes.NewBufferString("What are you?")
			req = test_util.NewRequest("GET", "example.com", "/", body)
		})
		It("logs an error and doesn't report anything", func() {
			handler.ServeHTTP(proxyWriter, req, nextHandler)
			Expect(fakeLogger.ErrorCallCount()).To(Equal(1))
			Expect(fakeReporter.CaptureBadGatewayCallCount()).To(Equal(0))
			Expect(fakeReporter.CaptureRoutingResponseCallCount()).To(Equal(0))
			Expect(fakeReporter.CaptureRoutingResponseLatencyCallCount()).To(Equal(0))
		})

	})
})
