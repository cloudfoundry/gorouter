package handlers_test

import (
	"net/http"
	"net/http/httptest"

	"code.cloudfoundry.org/gorouter/handlers"
	"code.cloudfoundry.org/gorouter/test_util"

	"code.cloudfoundry.org/gorouter/logger"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

// 64-bit random hexadecimal string
const b3_id_regex = `^[[:xdigit:]]{16}$`

var _ = Describe("Zipkin", func() {
	var (
		handler      *handlers.Zipkin
		headersToLog []string
		logger       logger.Logger
		resp         http.ResponseWriter
		req          *http.Request
		nextCalled   bool
	)

	nextHandler := http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		nextCalled = true
	})

	BeforeEach(func() {
		logger = test_util.NewTestZapLogger("zipkin")
		req = test_util.NewRequest("GET", "example.com", "/", nil)
		resp = httptest.NewRecorder()
		nextCalled = false
		headersToLog = []string{"foo-header"}
	})

	AfterEach(func() {
	})

	Context("with Zipkin enabled", func() {
		BeforeEach(func() {
			handler = handlers.NewZipkin(true, headersToLog, logger)
		})

		It("sets zipkin headers", func() {
			handler.ServeHTTP(resp, req, nextHandler)
			Expect(req.Header.Get(handlers.B3SpanIdHeader)).ToNot(BeEmpty())
			Expect(req.Header.Get(handlers.B3TraceIdHeader)).ToNot(BeEmpty())
			Expect(req.Header.Get(handlers.B3ParentSpanIdHeader)).To(BeEmpty())

			Expect(nextCalled).To(BeTrue(), "Expected the next handler to be called.")
		})

		It("adds zipkin headers to access log record", func() {
			newHeadersToLog := handler.HeadersToLog()

			Expect(newHeadersToLog).To(ContainElement(handlers.B3SpanIdHeader))
			Expect(newHeadersToLog).To(ContainElement(handlers.B3TraceIdHeader))
			Expect(newHeadersToLog).To(ContainElement(handlers.B3ParentSpanIdHeader))
			Expect(newHeadersToLog).To(ContainElement("foo-header"))
		})

		Context("with B3TraceIdHeader, B3SpanIdHeader and B3ParentSpanIdHeader already set", func() {
			BeforeEach(func() {
				req.Header.Set(handlers.B3TraceIdHeader, "Bogus Value")
				req.Header.Set(handlers.B3SpanIdHeader, "Span Value")
				req.Header.Set(handlers.B3ParentSpanIdHeader, "Span parent Value")
			})

			It("doesn't overwrite the B3ParentSpanIdHeader", func() {
				handler.ServeHTTP(resp, req, nextHandler)
				Expect(req.Header.Get(handlers.B3ParentSpanIdHeader)).To(Equal("Span parent Value"))

				Expect(nextCalled).To(BeTrue(), "Expected the next handler to be called.")
			})

			It("doesn't overwrite the B3SpanIdHeader", func() {
				handler.ServeHTTP(resp, req, nextHandler)
				Expect(req.Header.Get(handlers.B3SpanIdHeader)).To(Equal("Span Value"))

				Expect(nextCalled).To(BeTrue(), "Expected the next handler to be called.")
			})
			It("doesn't overwrite the B3TraceIdHeader", func() {
				handler.ServeHTTP(resp, req, nextHandler)
				Expect(req.Header.Get(handlers.B3TraceIdHeader)).To(Equal("Bogus Value"))

				Expect(nextCalled).To(BeTrue(), "Expected the next handler to be called.")
			})
		})
		Context("with B3TraceIdHeader and B3SpanIdHeader already set", func() {
			BeforeEach(func() {
				req.Header.Set(handlers.B3TraceIdHeader, "Bogus Value")
				req.Header.Set(handlers.B3SpanIdHeader, "Span Value")
			})

			It("doesn't overwrite the B3SpanIdHeader", func() {
				handler.ServeHTTP(resp, req, nextHandler)
				Expect(req.Header.Get(handlers.B3SpanIdHeader)).To(Equal("Span Value"))
				Expect(req.Header.Get(handlers.B3ParentSpanIdHeader)).To(BeEmpty())

				Expect(nextCalled).To(BeTrue(), "Expected the next handler to be called.")
			})
			It("doesn't overwrite the B3TraceIdHeader", func() {
				handler.ServeHTTP(resp, req, nextHandler)
				Expect(req.Header.Get(handlers.B3TraceIdHeader)).To(Equal("Bogus Value"))

				Expect(nextCalled).To(BeTrue(), "Expected the next handler to be called.")
			})
		})

		Context("with only B3SpanIdHeader set", func() {
			BeforeEach(func() {
				req.Header.Set(handlers.B3SpanIdHeader, "Span Value")
			})

			It("adds the B3TraceIdHeader and overwrites the SpanId", func() {
				handler.ServeHTTP(resp, req, nextHandler)
				Expect(req.Header.Get(handlers.B3TraceIdHeader)).To(MatchRegexp(b3_id_regex))
				Expect(req.Header.Get(handlers.B3SpanIdHeader)).To(MatchRegexp(b3_id_regex))
				Expect(req.Header.Get(handlers.B3ParentSpanIdHeader)).To(BeEmpty())

				Expect(nextCalled).To(BeTrue(), "Expected the next handler to be called.")
			})
		})

		Context("with only B3TraceIdHeader set", func() {
			BeforeEach(func() {
				req.Header.Set(handlers.B3TraceIdHeader, "Bogus Value")
			})

			It("overwrites the B3TraceIdHeader and adds a SpanId", func() {
				handler.ServeHTTP(resp, req, nextHandler)
				Expect(req.Header.Get(handlers.B3TraceIdHeader)).To(MatchRegexp(b3_id_regex))
				Expect(req.Header.Get(handlers.B3SpanIdHeader)).To(MatchRegexp(b3_id_regex))
				Expect(req.Header.Get(handlers.B3ParentSpanIdHeader)).To(BeEmpty())

				Expect(nextCalled).To(BeTrue(), "Expected the next handler to be called.")
			})
		})

		Context("when X-B3-* headers are already set to be logged", func() {
			BeforeEach(func() {
				newSlice := []string{handlers.B3TraceIdHeader, handlers.B3SpanIdHeader, handlers.B3ParentSpanIdHeader}
				headersToLog = newSlice
			})
			It("adds zipkin headers to access log record", func() {
				newHeadersToLog := handler.HeadersToLog()

				Expect(newHeadersToLog).To(ContainElement(handlers.B3SpanIdHeader))
				Expect(newHeadersToLog).To(ContainElement(handlers.B3TraceIdHeader))
				Expect(newHeadersToLog).To(ContainElement(handlers.B3ParentSpanIdHeader))
			})
		})
	})

	Context("with Zipkin disabled", func() {
		BeforeEach(func() {
			handler = handlers.NewZipkin(false, headersToLog, logger)
		})

		It("doesn't set any headers", func() {
			handler.ServeHTTP(resp, req, nextHandler)
			Expect(req.Header.Get(handlers.B3SpanIdHeader)).To(BeEmpty())
			Expect(req.Header.Get(handlers.B3TraceIdHeader)).To(BeEmpty())
			Expect(req.Header.Get(handlers.B3ParentSpanIdHeader)).To(BeEmpty())

			Expect(nextCalled).To(BeTrue(), "Expected the next handler to be called.")
		})

		It("does not add zipkin headers to access log record", func() {
			newHeadersToLog := handler.HeadersToLog()
			Expect(newHeadersToLog).NotTo(ContainElement(handlers.B3SpanIdHeader))
			Expect(newHeadersToLog).NotTo(ContainElement(handlers.B3ParentSpanIdHeader))
			Expect(newHeadersToLog).NotTo(ContainElement(handlers.B3TraceIdHeader))
			Expect(newHeadersToLog).To(ContainElement("foo-header"))
		})

		Context("when X-B3-* headers are already set to be logged", func() {
			It("adds zipkin headers to access log record", func() {
				newSlice := []string{handlers.B3TraceIdHeader, handlers.B3SpanIdHeader, handlers.B3ParentSpanIdHeader}
				handler := handlers.NewZipkin(false, newSlice, logger)
				newHeadersToLog := handler.HeadersToLog()
				Expect(newHeadersToLog).To(ContainElement(handlers.B3SpanIdHeader))
				Expect(newHeadersToLog).To(ContainElement(handlers.B3ParentSpanIdHeader))
				Expect(newHeadersToLog).To(ContainElement(handlers.B3TraceIdHeader))
			})
		})
	})
})
