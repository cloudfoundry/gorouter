package handlers_test

import (
	"net/http"
	"net/http/httptest"

	"code.cloudfoundry.org/gorouter/handlers"
	"code.cloudfoundry.org/gorouter/test_util"

	"code.cloudfoundry.org/gorouter/logger"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openzipkin/zipkin-go/propagation/b3"
)

// 64-bit random hexadecimal string
const (
	b3IDRegex      = `^[[:xdigit:]]{32}$`
	b3Regex        = `^[[:xdigit:]]{32}-[[:xdigit:]]{32}(-[01d](-[[:xdigit:]]{32})?)?$`
	b3TraceID      = "7f46165474d11ee5836777d85df2cdab"
	b3SpanID       = "54ebcb82b14862d9"
	b3ParentSpanID = "e56b75d6af463476"
	b3Single       = "1g56165474d11ee5836777d85df2cdab-32ebcb82b14862d9-1-ab6b75d6af463476"
)

var _ = Describe("Zipkin", func() {
	var (
		handler    *handlers.Zipkin
		logger     logger.Logger
		resp       http.ResponseWriter
		req        *http.Request
		nextCalled bool
	)

	nextHandler := http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		nextCalled = true
	})

	BeforeEach(func() {
		logger = test_util.NewTestZapLogger("zipkin")
		req = test_util.NewRequest("GET", "example.com", "/", nil)
		resp = httptest.NewRecorder()
		nextCalled = false
	})

	AfterEach(func() {
	})

	Context("with Zipkin enabled", func() {
		BeforeEach(func() {
			handler = handlers.NewZipkin(true, logger)
		})

		It("sets zipkin headers", func() {
			handler.ServeHTTP(resp, req, nextHandler)
			Expect(req.Header.Get(b3.SpanID)).ToNot(BeEmpty())
			Expect(req.Header.Get(b3.TraceID)).ToNot(BeEmpty())
			Expect(req.Header.Get(b3.ParentSpanID)).To(BeEmpty())
			Expect(req.Header.Get(b3.Context)).ToNot(BeEmpty())

			Expect(nextCalled).To(BeTrue(), "Expected the next handler to be called.")
		})

		Context("with B3TraceIdHeader, B3SpanIdHeader and B3ParentSpanIdHeader already set", func() {
			BeforeEach(func() {
				req.Header.Set(b3.TraceID, b3TraceID)
				req.Header.Set(b3.SpanID, b3SpanID)
				req.Header.Set(b3.ParentSpanID, b3ParentSpanID)
			})

			It("doesn't overwrite the B3ParentSpanIdHeader", func() {
				handler.ServeHTTP(resp, req, nextHandler)
				Expect(req.Header.Get(b3.ParentSpanID)).To(Equal(b3ParentSpanID))

				Expect(nextCalled).To(BeTrue(), "Expected the next handler to be called.")
			})

			It("doesn't overwrite the B3SpanIdHeader", func() {
				handler.ServeHTTP(resp, req, nextHandler)
				Expect(req.Header.Get(b3.SpanID)).To(Equal(b3SpanID))

				Expect(nextCalled).To(BeTrue(), "Expected the next handler to be called.")
			})

			It("doesn't overwrite the B3TraceIdHeader", func() {
				handler.ServeHTTP(resp, req, nextHandler)
				Expect(req.Header.Get(b3.TraceID)).To(Equal(b3TraceID))

				Expect(nextCalled).To(BeTrue(), "Expected the next handler to be called.")
			})
		})

		Context("with B3TraceIdHeader and B3SpanIdHeader already set", func() {
			BeforeEach(func() {
				req.Header.Set(b3.TraceID, b3TraceID)
				req.Header.Set(b3.SpanID, b3SpanID)
			})

			It("propagates the B3Header", func() {
				handler.ServeHTTP(resp, req, nextHandler)

				Expect(req.Header.Get(b3.Context)).To(Equal(b3TraceID + "-" + b3SpanID))
				Expect(nextCalled).To(BeTrue(), "Expected the next handler to be called.")
			})

			It("propagates the B3Header with Sampled header", func() {
				req.Header.Set(b3.Sampled, "true")

				handler.ServeHTTP(resp, req, nextHandler)

				Expect(req.Header.Get(b3.Context)).To(Equal(b3TraceID + "-" + b3SpanID + "-1"))
				Expect(nextCalled).To(BeTrue(), "Expected the next handler to be called.")
			})

			It("propagates the B3Header with Flags header", func() {
				req.Header.Set(b3.Flags, "1")
				req.Header.Set(b3.Sampled, "false")

				handler.ServeHTTP(resp, req, nextHandler)

				Expect(req.Header.Get(b3.Context)).To(Equal(b3TraceID + "-" + b3SpanID + "-d"))
				Expect(nextCalled).To(BeTrue(), "Expected the next handler to be called.")
			})

			It("propagates the B3Header with ParentSpanID header", func() {
				req.Header.Set(b3.Sampled, "false")
				req.Header.Set(b3.ParentSpanID, b3ParentSpanID)

				handler.ServeHTTP(resp, req, nextHandler)

				Expect(req.Header.Get(b3.Context)).To(Equal(b3TraceID + "-" + b3SpanID + "-0-" + b3ParentSpanID))
				Expect(nextCalled).To(BeTrue(), "Expected the next handler to be called.")
			})

			It("doesn't overwrite the B3SpanIdHeader", func() {
				handler.ServeHTTP(resp, req, nextHandler)
				Expect(req.Header.Get(b3.SpanID)).To(Equal(b3SpanID))
				Expect(req.Header.Get(b3.Context)).To(Equal(b3TraceID + "-" + b3SpanID))
				Expect(req.Header.Get(b3.ParentSpanID)).To(BeEmpty())

				Expect(nextCalled).To(BeTrue(), "Expected the next handler to be called.")
			})

			It("doesn't overwrite the B3TraceIdHeader", func() {
				handler.ServeHTTP(resp, req, nextHandler)
				Expect(req.Header.Get(b3.TraceID)).To(Equal(b3TraceID))

				Expect(nextCalled).To(BeTrue(), "Expected the next handler to be called.")
			})
		})

		Context("with only B3SpanIdHeader set", func() {
			BeforeEach(func() {
				req.Header.Set(b3.SpanID, b3SpanID)
			})

			It("adds the B3TraceIdHeader and overwrites the SpanId", func() {
				handler.ServeHTTP(resp, req, nextHandler)
				Expect(req.Header.Get(b3.TraceID)).To(MatchRegexp(b3IDRegex))
				Expect(req.Header.Get(b3.SpanID)).To(MatchRegexp(b3IDRegex))
				Expect(req.Header.Get(b3.ParentSpanID)).To(BeEmpty())
				Expect(req.Header.Get(b3.Context)).To(MatchRegexp(b3Regex))

				Expect(nextCalled).To(BeTrue(), "Expected the next handler to be called.")
			})
		})

		Context("with only B3TraceIdHeader set", func() {
			BeforeEach(func() {
				req.Header.Set(b3.TraceID, b3TraceID)
			})

			It("overwrites the B3TraceIdHeader and adds a SpanId", func() {
				handler.ServeHTTP(resp, req, nextHandler)
				Expect(req.Header.Get(b3.TraceID)).To(MatchRegexp(b3IDRegex))
				Expect(req.Header.Get(b3.SpanID)).To(MatchRegexp(b3IDRegex))
				Expect(req.Header.Get(b3.ParentSpanID)).To(BeEmpty())
				Expect(req.Header.Get(b3.Context)).To(MatchRegexp(b3Regex))

				Expect(nextCalled).To(BeTrue(), "Expected the next handler to be called.")
			})
		})

		Context("with B3Header already set", func() {
			BeforeEach(func() {
				req.Header.Set(b3.Context, b3Single)
			})

			It("doesn't overwrite the B3Header", func() {
				handler.ServeHTTP(resp, req, nextHandler)
				Expect(req.Header.Get(b3.Context)).To(Equal(b3Single))
				Expect(req.Header.Get(b3.TraceID)).To(BeEmpty())
				Expect(req.Header.Get(b3.SpanID)).To(BeEmpty())
				Expect(req.Header.Get(b3.ParentSpanID)).To(BeEmpty())

				Expect(nextCalled).To(BeTrue(), "Expected the next handler to be called.")
			})
		})
	})

	Context("with Zipkin disabled", func() {
		BeforeEach(func() {
			handler = handlers.NewZipkin(false, logger)
		})

		It("doesn't set any headers", func() {
			handler.ServeHTTP(resp, req, nextHandler)
			Expect(req.Header.Get(b3.SpanID)).To(BeEmpty())
			Expect(req.Header.Get(b3.TraceID)).To(BeEmpty())
			Expect(req.Header.Get(b3.ParentSpanID)).To(BeEmpty())
			Expect(req.Header.Get(b3.Context)).To(BeEmpty())

			Expect(nextCalled).To(BeTrue(), "Expected the next handler to be called.")
		})
	})
})
