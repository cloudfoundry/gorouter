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

// 64-bit random hexadecimal string
const b3_id_regex = `^[[:xdigit:]]{16}$`

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
			Expect(req.Header.Get(router_http.B3ParentSpanIdHeader)).To(BeEmpty())
		})

		It("adds zipkin headers to access log record", func() {
			handler.ServeHTTP(resp, req, nextHandler)
			Expect(*headersToLog).To(ContainElement(router_http.B3SpanIdHeader))
			Expect(*headersToLog).To(ContainElement(router_http.B3TraceIdHeader))
			Expect(*headersToLog).To(ContainElement(router_http.B3ParentSpanIdHeader))
		})

		Context("with B3TraceIdHeader and B3SpanIdHeader already set", func() {
			BeforeEach(func() {
				req.Header.Set(router_http.B3TraceIdHeader, "Bogus Value")
				req.Header.Set(router_http.B3SpanIdHeader, "Span Value")
			})

			It("doesn't overwrite the B3TraceIdHeader", func() {
				handler.ServeHTTP(resp, req, nextHandler)
				Expect(req.Header.Get(router_http.B3TraceIdHeader)).To(Equal("Bogus Value"))
				Expect(req.Header.Get(router_http.B3SpanIdHeader)).To(MatchRegexp(b3_id_regex))
				Expect(req.Header.Get(router_http.B3ParentSpanIdHeader)).To(Equal("Span Value"))
			})
		})

		Context("with only B3SpanIdHeader set", func() {
			BeforeEach(func() {
				req.Header.Set(router_http.B3SpanIdHeader, "Span Value")
			})

			It("adds the B3TraceIdHeader and overwrites the SpanId", func() {
				handler.ServeHTTP(resp, req, nextHandler)
				Expect(req.Header.Get(router_http.B3TraceIdHeader)).To(MatchRegexp(b3_id_regex))
				Expect(req.Header.Get(router_http.B3SpanIdHeader)).To(MatchRegexp(b3_id_regex))
				Expect(req.Header.Get(router_http.B3ParentSpanIdHeader)).To(BeEmpty())
			})
		})

		Context("with only B3TraceIdHeader set", func() {
			BeforeEach(func() {
				req.Header.Set(router_http.B3TraceIdHeader, "Bogus Value")
			})

			It("overwrites the B3TraceIdHeader and adds a SpanId", func() {
				handler.ServeHTTP(resp, req, nextHandler)
				Expect(req.Header.Get(router_http.B3TraceIdHeader)).To(MatchRegexp(b3_id_regex))
				Expect(req.Header.Get(router_http.B3SpanIdHeader)).To(MatchRegexp(b3_id_regex))
				Expect(req.Header.Get(router_http.B3ParentSpanIdHeader)).To(BeEmpty())
			})
		})

		Context("when X-B3-* headers are already set to be logged", func() {
			BeforeEach(func() {
				newSlice := []string{router_http.B3TraceIdHeader, router_http.B3SpanIdHeader, router_http.B3ParentSpanIdHeader}
				headersToLog = &newSlice
			})
			It("adds zipkin headers to access log record", func() {
				handler.ServeHTTP(resp, req, nextHandler)
				Expect(*headersToLog).To(ContainElement(router_http.B3SpanIdHeader))
				Expect(*headersToLog).To(ContainElement(router_http.B3TraceIdHeader))
				Expect(*headersToLog).To(ContainElement(router_http.B3ParentSpanIdHeader))
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
			Expect(req.Header.Get(router_http.B3ParentSpanIdHeader)).To(BeEmpty())
		})

		It("does not add zipkin headers to access log record", func() {
			handler.ServeHTTP(resp, req, nextHandler)
			Expect(*headersToLog).NotTo(ContainElement(router_http.B3SpanIdHeader))
			Expect(*headersToLog).NotTo(ContainElement(router_http.B3ParentSpanIdHeader))
			Expect(*headersToLog).NotTo(ContainElement(router_http.B3TraceIdHeader))
		})

		Context("when X-B3-* headers are already set to be logged", func() {
			BeforeEach(func() {
				newSlice := []string{router_http.B3TraceIdHeader, router_http.B3SpanIdHeader, router_http.B3ParentSpanIdHeader}
				headersToLog = &newSlice
			})
			It("adds zipkin headers to access log record", func() {
				handler.ServeHTTP(resp, req, nextHandler)
				Expect(*headersToLog).To(ContainElement(router_http.B3SpanIdHeader))
				Expect(*headersToLog).To(ContainElement(router_http.B3ParentSpanIdHeader))
				Expect(*headersToLog).To(ContainElement(router_http.B3TraceIdHeader))
			})
		})
	})
})
