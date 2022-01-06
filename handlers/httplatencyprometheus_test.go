package handlers_test

import (
	"bytes"
	"io/ioutil"
	"net/http"
	"net/http/httptest"

	"code.cloudfoundry.org/gorouter/route"

	fake_registry "code.cloudfoundry.org/go-metric-registry/testhelpers"
	"code.cloudfoundry.org/gorouter/handlers"
	"code.cloudfoundry.org/gorouter/test_util"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/urfave/negroni"
)

var _ = Describe("Http Prometheus Latency", func() {
	var (
		handler     *negroni.Negroni
		nextHandler http.HandlerFunc

		resp http.ResponseWriter
		req  *http.Request

		fakeRegistry *fake_registry.SpyMetricsRegistry

		nextCalled bool
	)

	BeforeEach(func() {
		body := bytes.NewBufferString("What are you?")
		req = test_util.NewRequest("GET", "example.com", "/", body)
		resp = httptest.NewRecorder()

		fakeRegistry = fake_registry.NewMetricsRegistry()

		nextHandler = http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
			_, err := ioutil.ReadAll(req.Body)
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
			handler.Use(handlers.NewHTTPLatencyPrometheus(fakeRegistry))
			handler.UseHandlerFunc(nextHandler)
		})
		It("forwards the request", func() {
			handler.ServeHTTP(resp, req)

			Expect(nextCalled).To(BeTrue(), "Expected the next handler to be called.")
		})

		It("records http latency", func() {
			handler.ServeHTTP(resp, req)

			metric := fakeRegistry.GetMetric("http_latency_seconds", map[string]string{"source_id": "some-source-id"})
			Expect(metric.Value()).ToNot(Equal(0))
		})

		It("http metric has help text", func() {
			handler.ServeHTTP(resp, req)

			metric := fakeRegistry.GetMetric("http_latency_seconds", map[string]string{"source_id": "some-source-id"})
			Expect(metric.HelpText()).To(Equal("the latency of http requests from gorouter and back"))
		})
		It("http metrics have expotential buckets", func() {
			handler.ServeHTTP(resp, req)

			metric := fakeRegistry.GetMetric("http_latency_seconds", map[string]string{"source_id": "some-source-id"})
			Expect(metric.Buckets()).To(Equal([]float64{
				0.1, 0.2, 0.4, 0.8, 1.6, 3.2, 6.4, 12.8, 25.6,
			}))
		})
	})

	Context("when the request info is not set", func() {
		It("sets source id to gorouter", func() {
			handler = negroni.New()
			handler.Use(handlers.NewHTTPLatencyPrometheus(fakeRegistry))
			handler.ServeHTTP(resp, req)

			metric := fakeRegistry.GetMetric("http_latency_seconds", map[string]string{"source_id": "gorouter"})
			Expect(metric.Value()).ToNot(Equal(0))
		})

		It("sets source id to gorouter", func() {
			nextHandler = http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
				_, err := ioutil.ReadAll(req.Body)
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
			handler.Use(handlers.NewHTTPLatencyPrometheus(fakeRegistry))
			handler.UseHandlerFunc(nextHandler)
			handler.ServeHTTP(resp, req)

			metric := fakeRegistry.GetMetric("http_latency_seconds", map[string]string{"source_id": "gorouter"})
			Expect(metric.Value()).ToNot(Equal(0))
		})
	})
})
