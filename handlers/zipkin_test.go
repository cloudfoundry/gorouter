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
const (
	b3IDRegex      = `^[[:xdigit:]]{16,32}$`
	b3Regex        = `^[[:xdigit:]]{16,32}-[[:xdigit:]]{16}(-[01d](-[[:xdigit:]]{16,32})?)?$`
	b3TraceID      = "7f46165474d11ee5836777d85df2cdab"
	b3SpanID       = "54ebcb82b14862d9"
	b3ParentSpanID = "e56b75d6af463476"
	b3Single       = "1g56165474d11ee5836777d85df2cdab-32ebcb82b14862d9-1-ab6b75d6af463476"
)

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
			Expect(req.Header.Get(handlers.B3Header)).ToNot(BeEmpty())

			Expect(nextCalled).To(BeTrue(), "Expected the next handler to be called.")
		})

		It("adds zipkin headers to access log record", func() {
			newHeadersToLog := handler.HeadersToLog()

			Expect(newHeadersToLog).To(ContainElement(handlers.B3SpanIdHeader))
			Expect(newHeadersToLog).To(ContainElement(handlers.B3TraceIdHeader))
			Expect(newHeadersToLog).To(ContainElement(handlers.B3ParentSpanIdHeader))
			Expect(newHeadersToLog).To(ContainElement(handlers.B3Header))
			Expect(newHeadersToLog).To(ContainElement("foo-header"))
		})

		Context("with B3TraceIdHeader, B3SpanIdHeader and B3ParentSpanIdHeader already set", func() {
			BeforeEach(func() {
				req.Header.Set(handlers.B3TraceIdHeader, b3TraceID)
				req.Header.Set(handlers.B3SpanIdHeader, b3SpanID)
				req.Header.Set(handlers.B3ParentSpanIdHeader, b3ParentSpanID)
			})

			It("doesn't overwrite the B3ParentSpanIdHeader", func() {
				handler.ServeHTTP(resp, req, nextHandler)
				Expect(req.Header.Get(handlers.B3ParentSpanIdHeader)).To(Equal(b3ParentSpanID))

				Expect(nextCalled).To(BeTrue(), "Expected the next handler to be called.")
			})

			It("doesn't overwrite the B3SpanIdHeader", func() {
				handler.ServeHTTP(resp, req, nextHandler)
				Expect(req.Header.Get(handlers.B3SpanIdHeader)).To(Equal(b3SpanID))

				Expect(nextCalled).To(BeTrue(), "Expected the next handler to be called.")
			})

			It("doesn't overwrite the B3TraceIdHeader", func() {
				handler.ServeHTTP(resp, req, nextHandler)
				Expect(req.Header.Get(handlers.B3TraceIdHeader)).To(Equal(b3TraceID))

				Expect(nextCalled).To(BeTrue(), "Expected the next handler to be called.")
			})
		})
		Context("with B3TraceIdHeader and B3SpanIdHeader already set", func() {
			BeforeEach(func() {
				req.Header.Set(handlers.B3TraceIdHeader, b3TraceID)
				req.Header.Set(handlers.B3SpanIdHeader, b3SpanID)
			})

			It("propagates the B3Header", func() {
				req.Header.Set(handlers.B3SampledHeader, "true")
				req.Header.Set(handlers.B3ParentSpanIdHeader, b3ParentSpanID)

				handler.ServeHTTP(resp, req, nextHandler)

				Expect(req.Header.Get(handlers.B3Header)).To(Equal(b3TraceID + "-" + b3SpanID + "-1-" + b3ParentSpanID))
				Expect(nextCalled).To(BeTrue(), "Expected the next handler to be called.")
			})

			It("doesn't overwrite the B3SpanIdHeader", func() {
				handler.ServeHTTP(resp, req, nextHandler)
				Expect(req.Header.Get(handlers.B3SpanIdHeader)).To(Equal(b3SpanID))
				Expect(req.Header.Get(handlers.B3Header)).To(Equal(b3TraceID + "-" + b3SpanID))
				Expect(req.Header.Get(handlers.B3ParentSpanIdHeader)).To(BeEmpty())

				Expect(nextCalled).To(BeTrue(), "Expected the next handler to be called.")
			})

			It("doesn't overwrite the B3TraceIdHeader", func() {
				handler.ServeHTTP(resp, req, nextHandler)
				Expect(req.Header.Get(handlers.B3TraceIdHeader)).To(Equal(b3TraceID))

				Expect(nextCalled).To(BeTrue(), "Expected the next handler to be called.")
			})
		})

		Context("with only B3SpanIdHeader set", func() {
			BeforeEach(func() {
				req.Header.Set(handlers.B3SpanIdHeader, b3SpanID)
			})

			It("adds the B3TraceIdHeader and overwrites the SpanId", func() {
				handler.ServeHTTP(resp, req, nextHandler)
				Expect(req.Header.Get(handlers.B3TraceIdHeader)).To(MatchRegexp(b3IDRegex))
				Expect(req.Header.Get(handlers.B3SpanIdHeader)).To(MatchRegexp(b3IDRegex))
				Expect(req.Header.Get(handlers.B3ParentSpanIdHeader)).To(BeEmpty())
				Expect(req.Header.Get(handlers.B3Header)).To(MatchRegexp(b3Regex))

				Expect(nextCalled).To(BeTrue(), "Expected the next handler to be called.")
			})
		})

		Context("with only B3TraceIdHeader set", func() {
			BeforeEach(func() {
				req.Header.Set(handlers.B3TraceIdHeader, b3TraceID)
			})

			It("overwrites the B3TraceIdHeader and adds a SpanId", func() {
				handler.ServeHTTP(resp, req, nextHandler)
				Expect(req.Header.Get(handlers.B3TraceIdHeader)).To(MatchRegexp(b3IDRegex))
				Expect(req.Header.Get(handlers.B3SpanIdHeader)).To(MatchRegexp(b3IDRegex))
				Expect(req.Header.Get(handlers.B3ParentSpanIdHeader)).To(BeEmpty())
				Expect(req.Header.Get(handlers.B3Header)).To(MatchRegexp(b3Regex))

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

		Context("with B3Header already set", func() {
			BeforeEach(func() {
				req.Header.Set(handlers.B3Header, b3Single)
			})

			It("doesn't overwrite the B3Header", func() {
				handler.ServeHTTP(resp, req, nextHandler)
				Expect(req.Header.Get(handlers.B3Header)).To(Equal(b3Single))
				Expect(req.Header.Get(handlers.B3TraceIdHeader)).To(BeEmpty())
				Expect(req.Header.Get(handlers.B3SpanIdHeader)).To(BeEmpty())
				Expect(req.Header.Get(handlers.B3ParentSpanIdHeader)).To(BeEmpty())

				Expect(nextCalled).To(BeTrue(), "Expected the next handler to be called.")
			})
		})

		Context("when b3 headers are already set to be logged", func() {
			BeforeEach(func() {
				newSlice := []string{handlers.B3Header}
				headersToLog = newSlice
			})
			It("adds zipkin headers to access log record", func() {
				newHeadersToLog := handler.HeadersToLog()

				Expect(newHeadersToLog).To(ContainElement(handlers.B3Header))
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
			Expect(req.Header.Get(handlers.B3Header)).To(BeEmpty())

			Expect(nextCalled).To(BeTrue(), "Expected the next handler to be called.")
		})

		It("does not add zipkin headers to access log record", func() {
			newHeadersToLog := handler.HeadersToLog()
			Expect(newHeadersToLog).NotTo(ContainElement(handlers.B3SpanIdHeader))
			Expect(newHeadersToLog).NotTo(ContainElement(handlers.B3ParentSpanIdHeader))
			Expect(newHeadersToLog).NotTo(ContainElement(handlers.B3TraceIdHeader))
			Expect(newHeadersToLog).NotTo(ContainElement(handlers.B3Header))
			Expect(newHeadersToLog).To(ContainElement("foo-header"))
		})

		Context("when X-B3-* headers are already set to be logged", func() {
			It("adds zipkin headers to access log record", func() {
				newSlice := []string{handlers.B3TraceIdHeader, handlers.B3SpanIdHeader, handlers.B3ParentSpanIdHeader, handlers.B3Header}
				handler := handlers.NewZipkin(false, newSlice, logger)
				newHeadersToLog := handler.HeadersToLog()
				Expect(newHeadersToLog).To(ContainElement(handlers.B3SpanIdHeader))
				Expect(newHeadersToLog).To(ContainElement(handlers.B3ParentSpanIdHeader))
				Expect(newHeadersToLog).To(ContainElement(handlers.B3TraceIdHeader))
				Expect(newHeadersToLog).To(ContainElement(handlers.B3Header))
			})
		})
	})
})

var _ = Describe("B3SingleHeaderBuilder", func() {
	It("builds B3Header correctly", func() {
		cases := []struct {
			traceID        string
			spanID         string
			sampling       string
			flags          string
			parentID       string
			expectedHeader string
		}{
			{traceID: "", spanID: "", expectedHeader: ""},
			{traceID: b3TraceID, spanID: b3SpanID, expectedHeader: b3TraceID + "-" + b3SpanID},
			{traceID: b3TraceID, spanID: b3SpanID, sampling: "true", expectedHeader: b3TraceID + "-" + b3SpanID + "-1"},
			{traceID: b3TraceID, spanID: b3SpanID, flags: "1", sampling: "false", expectedHeader: b3TraceID + "-" + b3SpanID + "-d"},
			{traceID: b3TraceID, spanID: b3SpanID, sampling: "false", parentID: b3ParentSpanID, expectedHeader: b3TraceID + "-" + b3SpanID + "-0-" + b3ParentSpanID},
		}

		for _, c := range cases {
			Expect(handlers.BuildB3SingleHeader(c.traceID, c.spanID, c.sampling, c.flags, c.parentID)).To(Equal(c.expectedHeader))
		}
	})
})
