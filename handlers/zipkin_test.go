package handlers_test

import (
	"context"
	"net/http"
	"net/http/httptest"

	"code.cloudfoundry.org/gorouter/handlers"
	"code.cloudfoundry.org/gorouter/test_util"

	"code.cloudfoundry.org/gorouter/logger"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/openzipkin/zipkin-go/propagation/b3"
)

// 64-bit random hexadecimal string
const (
	b3IDRegex            = `^[[:xdigit:]]{32}$`
	b3Regex              = `^[[:xdigit:]]{32}-[[:xdigit:]]{16}(-[01d](-[[:xdigit:]]{16})?)?$`
	b3TraceID            = "7f46165474d11ee5836777d85df2cdab"
	UUID                 = "7f461654-74d1-1ee5-8367-77d85df2cdab"
	b3SpanID             = "54ebcb82b14862d9"
	b3SpanRegex          = `[[:xdigit:]]{16}$`
	b3ParentSpanID       = "e56b75d6af463476"
	invalidB3Single      = "1g56165474d11ee5836777d85df2cdab-32ebcb82b14862d9-1-ab6b75d6af463476"
	validB3Single        = "6d8780b7d3ee13f5108c880d778f29eb-108c880d778f29eb"
	invalidUUIDB3Single  = "6d8780b7d3eed3f5108c880d778f29eb-108c880d778f29eb"
	invalidUUIDb3TraceID = "6d8780b7d3eed3f5108c880d778f29eb"
	invalidUUIDb3SpanID  = "108c880d778f29eb"
)

var _ = Describe("Zipkin", func() {
	var (
		handler    *handlers.Zipkin
		logger     logger.Logger
		resp       http.ResponseWriter
		req        *http.Request
		nextCalled bool
		reqInfo    *handlers.RequestInfo
	)

	nextHandler := http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		var err error
		reqInfo, err = handlers.ContextRequestInfo(r)
		Expect(err).NotTo(HaveOccurred())
		nextCalled = true
	})

	BeforeEach(func() {
		logger = test_util.NewTestZapLogger("zipkin")
		ri := new(handlers.RequestInfo)
		req = test_util.NewRequest("GET", "example.com", "/", nil).
			WithContext(context.WithValue(context.Background(), handlers.RequestInfoCtxKey, ri))
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
			Expect(req.Header.Get(b3.SpanID)).To(MatchRegexp(b3SpanRegex))
			Expect(req.Header.Get(b3.TraceID)).ToNot(BeEmpty())
			Expect(req.Header.Get(b3.ParentSpanID)).To(BeEmpty())
			Expect(req.Header.Get(b3.Context)).ToNot(BeEmpty())

			Expect(nextCalled).To(BeTrue(), "Expected the next handler to be called.")
		})

		It("sets request context", func() {
			handler.ServeHTTP(resp, req, nextHandler)
			Expect(reqInfo.TraceInfo.TraceID).To(MatchRegexp(b3IDRegex))
			Expect(reqInfo.TraceInfo.SpanID).To(MatchRegexp(b3SpanRegex))
			Expect(reqInfo.TraceInfo.UUID).To(MatchRegexp(UUIDRegex))
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

			It("sets request context", func() {
				handler.ServeHTTP(resp, req, nextHandler)
				Expect(reqInfo.TraceInfo.TraceID).To(Equal(b3TraceID))
				Expect(reqInfo.TraceInfo.SpanID).To(Equal(b3SpanID))
				Expect(reqInfo.TraceInfo.UUID).To(Equal(UUID))
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

			It("sets request context", func() {
				handler.ServeHTTP(resp, req, nextHandler)
				Expect(reqInfo.TraceInfo.TraceID).To(Equal(b3TraceID))
				Expect(reqInfo.TraceInfo.SpanID).To(Equal(b3SpanID))
				Expect(reqInfo.TraceInfo.UUID).To(Equal(UUID))
			})
		})

		Context("with B3TraceIdHeader that is invalid UUID and valid B3SpanIdHeader already set", func() {
			BeforeEach(func() {
				req.Header.Set(b3.TraceID, invalidUUIDb3TraceID)
				req.Header.Set(b3.SpanID, b3SpanID)
			})

			It("doesn't overwrite the B3 headers", func() {
				handler.ServeHTTP(resp, req, nextHandler)
				Expect(req.Header.Get(b3.TraceID)).To(Equal(invalidUUIDb3TraceID))
				Expect(req.Header.Get(b3.SpanID)).To(Equal(b3SpanID))
				Expect(req.Header.Get(b3.Context)).To(Equal(invalidUUIDb3TraceID + "-" + b3SpanID))

				Expect(nextCalled).To(BeTrue(), "Expected the next handler to be called.")
			})

			It("sets request context", func() {
				handler.ServeHTTP(resp, req, nextHandler)
				Expect(reqInfo.TraceInfo.TraceID).NotTo(Equal(invalidUUIDb3TraceID))
				Expect(reqInfo.TraceInfo.SpanID).NotTo(Equal(b3SpanID))
				Expect(reqInfo.TraceInfo.TraceID).To(MatchRegexp(b3IDRegex))
				Expect(reqInfo.TraceInfo.SpanID).To(MatchRegexp(b3SpanRegex))
				Expect(reqInfo.TraceInfo.UUID).To(MatchRegexp(UUIDRegex))
			})
		})

		Context("with only B3SpanIdHeader set", func() {
			BeforeEach(func() {
				req.Header.Set(b3.SpanID, b3SpanID)
			})

			It("adds the B3TraceIdHeader and overwrites the SpanId", func() {
				handler.ServeHTTP(resp, req, nextHandler)
				Expect(req.Header.Get(b3.TraceID)).To(MatchRegexp(b3IDRegex))
				Expect(req.Header.Get(b3.SpanID)).To(MatchRegexp(b3SpanRegex))
				Expect(req.Header.Get(b3.ParentSpanID)).To(BeEmpty())
				Expect(req.Header.Get(b3.Context)).To(MatchRegexp(b3Regex))

				Expect(nextCalled).To(BeTrue(), "Expected the next handler to be called.")
			})

			It("sets request context", func() {
				handler.ServeHTTP(resp, req, nextHandler)
				Expect(reqInfo.TraceInfo.TraceID).To(MatchRegexp(b3IDRegex))
				Expect(reqInfo.TraceInfo.SpanID).To(MatchRegexp(b3SpanRegex))
				Expect(reqInfo.TraceInfo.UUID).To(MatchRegexp(UUIDRegex))
			})
		})

		Context("with only B3TraceIdHeader set", func() {
			BeforeEach(func() {
				req.Header.Set(b3.TraceID, b3TraceID)
			})

			It("overwrites the B3TraceIdHeader and adds a SpanId", func() {
				handler.ServeHTTP(resp, req, nextHandler)
				Expect(req.Header.Get(b3.TraceID)).To(MatchRegexp(b3IDRegex))
				Expect(req.Header.Get(b3.SpanID)).To(MatchRegexp(b3SpanRegex))
				Expect(req.Header.Get(b3.ParentSpanID)).To(BeEmpty())
				Expect(req.Header.Get(b3.Context)).To(MatchRegexp(b3Regex))

				Expect(nextCalled).To(BeTrue(), "Expected the next handler to be called.")
			})

			It("sets request context", func() {
				handler.ServeHTTP(resp, req, nextHandler)
				Expect(reqInfo.TraceInfo.TraceID).To(MatchRegexp(b3IDRegex))
				Expect(reqInfo.TraceInfo.SpanID).To(MatchRegexp(b3SpanRegex))
				Expect(reqInfo.TraceInfo.UUID).To(MatchRegexp(UUIDRegex))
			})
		})

		Context("with a valid B3Header already set", func() {
			BeforeEach(func() {
				req.Header.Set(b3.Context, validB3Single)
			})

			It("doesn't overwrite the B3Header", func() {
				handler.ServeHTTP(resp, req, nextHandler)
				Expect(req.Header.Get(b3.Context)).To(Equal(validB3Single))
				Expect(req.Header.Get(b3.TraceID)).To(BeEmpty())
				Expect(req.Header.Get(b3.SpanID)).To(BeEmpty())
				Expect(req.Header.Get(b3.ParentSpanID)).To(BeEmpty())

				Expect(nextCalled).To(BeTrue(), "Expected the next handler to be called.")
			})

			It("sets request context", func() {
				handler.ServeHTTP(resp, req, nextHandler)
				Expect(reqInfo.TraceInfo.TraceID).To(MatchRegexp(b3IDRegex))
				Expect(reqInfo.TraceInfo.SpanID).To(MatchRegexp(b3SpanRegex))
				Expect(reqInfo.TraceInfo.UUID).To(MatchRegexp(UUIDRegex))
			})
		})

		Context("with invalid B3Header already set", func() {
			BeforeEach(func() {
				req.Header.Set(b3.Context, invalidB3Single)
			})

			It("overwrites the B3Header", func() {
				handler.ServeHTTP(resp, req, nextHandler)
				Expect(req.Header.Get(b3.Context)).NotTo(Equal(invalidB3Single))
				Expect(req.Header.Get(b3.Context)).NotTo(BeEmpty())
				Expect(req.Header.Get(b3.TraceID)).NotTo(BeEmpty())
				Expect(req.Header.Get(b3.SpanID)).NotTo(BeEmpty())
				Expect(req.Header.Get(b3.ParentSpanID)).To(BeEmpty())

				Expect(nextCalled).To(BeTrue(), "Expected the next handler to be called.")
			})

			It("sets request context", func() {
				handler.ServeHTTP(resp, req, nextHandler)
				Expect(reqInfo.TraceInfo.TraceID).To(MatchRegexp(b3IDRegex))
				Expect(reqInfo.TraceInfo.SpanID).To(MatchRegexp(b3SpanRegex))
				Expect(reqInfo.TraceInfo.UUID).To(MatchRegexp(UUIDRegex))
			})
		})

		Context("with B3Header which is an invalid UUID", func() {
			BeforeEach(func() {
				req.Header.Set(b3.Context, invalidUUIDB3Single)
			})

			It("overwrites the B3Header", func() {
				handler.ServeHTTP(resp, req, nextHandler)
				Expect(req.Header.Get(b3.Context)).To(Equal(invalidUUIDB3Single))
				Expect(req.Header.Get(b3.TraceID)).To(BeEmpty())
				Expect(req.Header.Get(b3.SpanID)).To(BeEmpty())
				Expect(req.Header.Get(b3.ParentSpanID)).To(BeEmpty())

				Expect(nextCalled).To(BeTrue(), "Expected the next handler to be called.")
			})

			It("sets request context", func() {
				handler.ServeHTTP(resp, req, nextHandler)
				Expect(reqInfo.TraceInfo.TraceID).NotTo(Equal(invalidUUIDb3TraceID))
				Expect(reqInfo.TraceInfo.SpanID).NotTo(Equal(b3SpanID))
				Expect(reqInfo.TraceInfo.TraceID).To(MatchRegexp(b3IDRegex))
				Expect(reqInfo.TraceInfo.SpanID).To(MatchRegexp(b3SpanRegex))
				Expect(reqInfo.TraceInfo.UUID).To(MatchRegexp(UUIDRegex))
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

		It("doesn't set trace and span IDs in request context", func() {
			handler.ServeHTTP(resp, req, nextHandler)
			Expect(reqInfo.TraceInfo.TraceID).To(BeEmpty())
			Expect(reqInfo.TraceInfo.SpanID).To(BeEmpty())
			Expect(reqInfo.TraceInfo.UUID).To(BeEmpty())
		})
	})
})
