package handlers_test

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/urfave/negroni/v3"

	"code.cloudfoundry.org/gorouter/handlers"
	metrics_fakes "code.cloudfoundry.org/gorouter/metrics/fakes"
	"code.cloudfoundry.org/gorouter/route"
	"code.cloudfoundry.org/gorouter/test_util"
)

var _ = Describe("Http Prometheus Latency", func() {
	var (
		handler     *negroni.Negroni
		nextHandler http.HandlerFunc

		resp http.ResponseWriter
		req  *http.Request

		fakeReporter *metrics_fakes.FakeProxyReporter

		nextCalled bool
	)

	BeforeEach(func() {
		body := bytes.NewBufferString("What are you?")
		req = test_util.NewRequest("GET", "example.com", "/", body)
		resp = httptest.NewRecorder()

		fakeReporter = new(metrics_fakes.FakeProxyReporter)

		nextHandler = http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
			_, err := io.ReadAll(req.Body)
			Expect(err).NotTo(HaveOccurred())

			rw.WriteHeader(http.StatusTeapot)
			rw.Write([]byte("I'm a little teapot, short and stout."))

			requestInfo, err := handlers.ContextRequestInfo(req)
			Expect(err).ToNot(HaveOccurred())
			requestInfo.RouteEndpoint = route.NewEndpoint(&route.EndpointOpts{
				Tags: map[string]string{
					"source_id": "some-source-id",
				},
			})

			nextCalled = true
		})
		nextCalled = false
	})
	Context("when the request info is set", func() {
		JustBeforeEach(func() {
			handler = negroni.New()
			handler.Use(handlers.NewRequestInfo())
			handler.Use(handlers.NewHTTPLatencyPrometheus(fakeReporter))
			handler.UseHandlerFunc(nextHandler)
		})
		It("forwards the request", func() {
			handler.ServeHTTP(resp, req)

			Expect(nextCalled).To(BeTrue(), "Expected the next handler to be called.")
		})

		It("records http latency", func() {
			handler.ServeHTTP(resp, req)

			Expect(fakeReporter.CaptureHTTPLatencyCallCount()).ToNot(Equal(0))
		})
	})

	Context("when the request info is not set", func() {
		It("sets source id to gorouter", func() {
			handler = negroni.New()
			handler.Use(handlers.NewHTTPLatencyPrometheus(fakeReporter))
			handler.ServeHTTP(resp, req)

			Expect(fakeReporter.CaptureHTTPLatencyCallCount()).ToNot(Equal(0))

			_, sourceID := fakeReporter.CaptureHTTPLatencyArgsForCall(0)
			Expect(sourceID).To(Equal("gorouter"))
		})

		It("sets source id to gorouter", func() {
			nextHandler = http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
				_, err := io.ReadAll(req.Body)
				Expect(err).NotTo(HaveOccurred())

				rw.WriteHeader(http.StatusTeapot)
				rw.Write([]byte("I'm a little teapot, short and stout."))

				requestInfo, err := handlers.ContextRequestInfo(req)
				Expect(err).ToNot(HaveOccurred())
				requestInfo.RouteEndpoint = route.NewEndpoint(&route.EndpointOpts{
					Tags: map[string]string{
						"source_id": "",
					},
				})
			})
			handler = negroni.New()
			handler.Use(handlers.NewRequestInfo())
			handler.Use(handlers.NewHTTPLatencyPrometheus(fakeReporter))
			handler.UseHandlerFunc(nextHandler)
			handler.ServeHTTP(resp, req)

			Expect(fakeReporter.CaptureHTTPLatencyCallCount()).ToNot(Equal(0))

			_, sourceID := fakeReporter.CaptureHTTPLatencyArgsForCall(0)
			Expect(sourceID).To(Equal("gorouter"))
		})
	})
})
