package handlers_test

import (
	"net/http"
	"net/http/httptest"

	router_http "code.cloudfoundry.org/gorouter/common/http"

	"code.cloudfoundry.org/gorouter/handlers"
	"code.cloudfoundry.org/gorouter/test_util"
	"code.cloudfoundry.org/lager"
	"code.cloudfoundry.org/lager/lagertest"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/urfave/negroni"
)

var _ = Describe("Zipkin", func() {
	var (
		handler      negroni.Handler
		headersToLog *[]string
		logger       lager.Logger
		resp         http.ResponseWriter
		req          *http.Request
		nextCalled   bool
	)

	nextHandler := http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		nextCalled = true
	})

	BeforeEach(func() {
		headersToLog = &[]string{}
		logger = lagertest.NewTestLogger("zipkin")
		req = test_util.NewRequest("GET", "example.com", "/", nil)
		resp = httptest.NewRecorder()
		nextCalled = false
	})

	AfterEach(func() {
		Expect(nextCalled).To(BeTrue(), "Expected the next handler to be called.")
	})

	Context("with Zipkin enabled", func() {
		BeforeEach(func() {
			handler = handlers.NewZipkin(true, headersToLog, logger)
		})

		It("sets zipkin headers", func() {
			handler.ServeHTTP(resp, req, nextHandler)
			Expect(req.Header.Get(router_http.B3SpanIdHeader)).ToNot(BeEmpty())
			Expect(req.Header.Get(router_http.B3TraceIdHeader)).ToNot(BeEmpty())
		})

		It("adds zipkin headers to access log record", func() {
			handler.ServeHTTP(resp, req, nextHandler)
			Expect(*headersToLog).To(ContainElement(router_http.B3SpanIdHeader))
			Expect(*headersToLog).To(ContainElement(router_http.B3TraceIdHeader))
		})

		Context("with B3TraceIdHeader already set", func() {
			BeforeEach(func() {
				req.Header.Set(router_http.B3TraceIdHeader, "Bogus Value")
			})

			It("doesn't overwrite the B3TraceIdHeader", func() {
				handler.ServeHTTP(resp, req, nextHandler)
				Expect(req.Header.Get(router_http.B3TraceIdHeader)).To(Equal("Bogus Value"))
			})
		})

		Context("when X-B3-* headers are already set to be logged", func() {
			BeforeEach(func() {
				newSlice := []string{router_http.B3TraceIdHeader, router_http.B3SpanIdHeader}
				headersToLog = &newSlice
			})
			It("adds zipkin headers to access log record", func() {
				handler.ServeHTTP(resp, req, nextHandler)
				Expect(*headersToLog).To(ContainElement(router_http.B3SpanIdHeader))
				Expect(*headersToLog).To(ContainElement(router_http.B3TraceIdHeader))
			})
		})
	})

	Context("with Zipkin disabled", func() {
		BeforeEach(func() {
			handler = handlers.NewZipkin(false, headersToLog, logger)
		})

		It("doesn't set any headers", func() {
			handler.ServeHTTP(resp, req, nextHandler)
			Expect(req.Header.Get(router_http.B3SpanIdHeader)).To(BeEmpty())
			Expect(req.Header.Get(router_http.B3TraceIdHeader)).To(BeEmpty())
		})

		It("does not add zipkin headers to access log record", func() {
			handler.ServeHTTP(resp, req, nextHandler)
			Expect(*headersToLog).NotTo(ContainElement(router_http.B3SpanIdHeader))
			Expect(*headersToLog).NotTo(ContainElement(router_http.B3TraceIdHeader))
		})

		Context("when X-B3-* headers are already set to be logged", func() {
			BeforeEach(func() {
				newSlice := []string{router_http.B3TraceIdHeader, router_http.B3SpanIdHeader}
				headersToLog = &newSlice
			})
			It("adds zipkin headers to access log record", func() {
				handler.ServeHTTP(resp, req, nextHandler)
				Expect(*headersToLog).To(ContainElement(router_http.B3SpanIdHeader))
				Expect(*headersToLog).To(ContainElement(router_http.B3TraceIdHeader))
			})
		})
	})
})
