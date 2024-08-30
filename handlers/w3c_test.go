package handlers_test

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"

	"code.cloudfoundry.org/gorouter/handlers"
	log "code.cloudfoundry.org/gorouter/logger"
	"code.cloudfoundry.org/gorouter/test_util"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gbytes"
	"go.uber.org/zap/zapcore"
)

var _ = Describe("W3C", func() {
	extractParentID := func(traceparent string) string {
		r := regexp.MustCompile("^00-[[:xdigit:]]{32}-([[:xdigit:]]{16})-01$")

		matches := r.FindStringSubmatch(traceparent)

		// First match is entire string
		// Seocnd match is parentID
		if len(matches) != 2 {
			return ""
		}

		return matches[1]
	}

	var (
		handler    *handlers.W3C
		testSink   *test_util.TestSink
		logger     *slog.Logger
		resp       http.ResponseWriter
		req        *http.Request
		reqInfo    *handlers.RequestInfo
		nextCalled bool
	)

	nextHandler := http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		var err error
		reqInfo, err = handlers.ContextRequestInfo(r)
		Expect(err).NotTo(HaveOccurred())
		nextCalled = true
	})

	BeforeEach(func() {
		logger = log.CreateLoggerWithSource("w3c", "")
		testSink = &test_util.TestSink{Buffer: gbytes.NewBuffer()}
		log.SetDynamicWriteSyncer(zapcore.NewMultiWriteSyncer(testSink, zapcore.AddSync(GinkgoWriter)))
		log.SetLoggingLevel("Debug")

		ri := new(handlers.RequestInfo)
		req = test_util.NewRequest("GET", "example.com", "/", nil).
			WithContext(context.WithValue(context.Background(), handlers.RequestInfoCtxKey, ri))

		resp = httptest.NewRecorder()
		nextCalled = false
	})

	AfterEach(func() {
	})

	Context("with W3C enabled", func() {
		Context("without a tenantID set", func() {
			BeforeEach(func() {
				handler = handlers.NewW3C(true, "", logger)
			})

			Context("when there are no pre-existing headers", func() {
				Context("when request context has trace and span id", func() {
					BeforeEach(func() {
						ri := new(handlers.RequestInfo)
						ri.TraceInfo.TraceID = strings.Repeat("1", 32)
						ri.TraceInfo.SpanID = strings.Repeat("2", 16)
						ri.TraceInfo.UUID = "11111111-1111-1111-1111-111111111111"
						req = test_util.NewRequest("GET", "example.com", "/", nil).
							WithContext(context.WithValue(context.Background(), handlers.RequestInfoCtxKey, ri))
					})

					It("uses trace ID from request context", func() {
						handler.ServeHTTP(resp, req, nextHandler)

						traceparentHeader := req.Header.Get(handlers.W3CTraceparentHeader)

						Expect(traceparentHeader).To(Equal("00-11111111111111111111111111111111-2222222222222222-01"))

						Expect(reqInfo.TraceInfo.TraceID).To(Equal(strings.Repeat("1", 32)))
						Expect(reqInfo.TraceInfo.SpanID).To(Equal(strings.Repeat("2", 16)))
						Expect(reqInfo.TraceInfo.UUID).To(Equal("11111111-1111-1111-1111-111111111111"))
					})
				})

				Context("when request context has invalid trace and span id", func() {
					BeforeEach(func() {
						ri := new(handlers.RequestInfo)
						ri.TraceInfo.TraceID = strings.Repeat("g", 32)
						ri.TraceInfo.SpanID = strings.Repeat("2", 16)
						ri.TraceInfo.UUID = "11111111-1111-1111-1111-111111111111"
						req = test_util.NewRequest("GET", "example.com", "/", nil).
							WithContext(context.WithValue(context.Background(), handlers.RequestInfoCtxKey, ri))
					})

					It("does not set traceparentHeader and logs the error", func() {
						handler.ServeHTTP(resp, req, nextHandler)

						traceparentHeader := req.Header.Get(handlers.W3CTraceparentHeader)

						Expect(traceparentHeader).To(BeEmpty())

						Expect(string(testSink.Contents())).To(ContainSubstring(`failed-to-create-w3c-traceparent`))
					})
				})

				Context("when request context doesn't have trace and span id", func() {
					It("sets W3C headers from request context and calls the next handler", func() {
						handler.ServeHTTP(resp, req, nextHandler)

						traceparentHeader := req.Header.Get(handlers.W3CTraceparentHeader)

						Expect(traceparentHeader).To(MatchRegexp(
							"^00-[[:xdigit:]]{32}-[[:xdigit:]]{16}-01$"), "Must match the W3C spec",
						)

						parentID := extractParentID(traceparentHeader)

						Expect(req.Header.Get(handlers.W3CTracestateHeader)).To(
							Equal(fmt.Sprintf("gorouter=%s", parentID)),
						)

						Expect(nextCalled).To(BeTrue(), "Expected the next handler to be called.")

						Expect(reqInfo.TraceInfo.TraceID).To(MatchRegexp(b3IDRegex))
						Expect(reqInfo.TraceInfo.SpanID).To(MatchRegexp(b3SpanRegex))
						Expect(reqInfo.TraceInfo.UUID).To(MatchRegexp(UUIDRegex))
					})
				})
			})

			Context("when there are pre-existing headers", func() {
				BeforeEach(func() {
					req.Header.Set(
						handlers.W3CTraceparentHeader,
						"00-11111111111111111111111111111111-2222222222222222-01",
					)

					req.Header.Set(
						handlers.W3CTracestateHeader,
						"rojo=00f067aa0ba902b7,congo=t61rcWkgMzE",
					)
				})

				It("sets W3C headers and calls the next handler", func() {
					handler.ServeHTTP(resp, req, nextHandler)

					traceparentHeader := req.Header.Get(handlers.W3CTraceparentHeader)

					Expect(traceparentHeader).To(MatchRegexp(
						"^00-[1]{32}-[a-f0-9]{16}-01$"),
						"Must update the parent ID but not the trace ID",
					)

					parentID := extractParentID(traceparentHeader)

					Expect(req.Header.Get(handlers.W3CTracestateHeader)).To(
						Equal(fmt.Sprintf(
							"gorouter=%s,rojo=00f067aa0ba902b7,congo=t61rcWkgMzE", parentID,
						)),
					)

					Expect(nextCalled).To(BeTrue(), "Expected the next handler to be called.")
				})

				Context("when request context has trace and span id", func() {
					BeforeEach(func() {
						ri := new(handlers.RequestInfo)
						ri.TraceInfo.TraceID = strings.Repeat("3", 32)
						ri.TraceInfo.SpanID = strings.Repeat("4", 16)
						ri.TraceInfo.UUID = "33333333-3333-3333-3333-333333333333"
						req = test_util.NewRequest("GET", "example.com", "/", nil).
							WithContext(context.WithValue(context.Background(), handlers.RequestInfoCtxKey, ri))
					})

					It("doesn't update request context", func() {
						handler.ServeHTTP(resp, req, nextHandler)
						Expect(reqInfo.TraceInfo.TraceID).To(Equal(strings.Repeat("3", 32)))
						Expect(reqInfo.TraceInfo.SpanID).To(Equal(strings.Repeat("4", 16)))
						Expect(reqInfo.TraceInfo.UUID).To(Equal("33333333-3333-3333-3333-333333333333"))
					})
				})

				Context("when request context doesn't have trace and span id", func() {
					It("updates request context with trace ID and generated parent ID", func() {
						handler.ServeHTTP(resp, req, nextHandler)
						Expect(reqInfo.TraceInfo.TraceID).To(Equal(strings.Repeat("1", 32)))
						Expect(reqInfo.TraceInfo.SpanID).To(MatchRegexp(b3SpanRegex))
						Expect(reqInfo.TraceInfo.UUID).To(Equal("11111111-1111-1111-1111-111111111111"))
					})
				})
			})

			Context("when there are pre-existing headers including gorouter", func() {
				BeforeEach(func() {
					req.Header.Set(
						handlers.W3CTraceparentHeader,
						"00-11111111111111111111111111111111-2222222222222222-01",
					)

					req.Header.Set(
						handlers.W3CTracestateHeader,
						"rojo=00f067aa0ba902b7,gorouter=t61rcWkgMzE",
					)
				})

				It("sets W3C headers, replacing the older gorouter header, and calls the next handler", func() {
					handler.ServeHTTP(resp, req, nextHandler)

					traceparentHeader := req.Header.Get(handlers.W3CTraceparentHeader)

					Expect(traceparentHeader).To(MatchRegexp(
						"^00-[1]{32}-[a-f0-9]{16}-01$"),
						"Must update the parent ID but not the trace ID",
					)

					parentID := extractParentID(traceparentHeader)

					Expect(req.Header.Get(handlers.W3CTracestateHeader)).To(
						Equal(fmt.Sprintf("gorouter=%s,rojo=00f067aa0ba902b7", parentID)),
						"The old gorouter value should be replaced by the newer parentID",
					)

					Expect(nextCalled).To(BeTrue(), "Expected the next handler to be called.")
				})

				It("sets request context", func() {
					handler.ServeHTTP(resp, req, nextHandler)
					Expect(reqInfo.TraceInfo.TraceID).To(MatchRegexp(b3IDRegex))
					Expect(reqInfo.TraceInfo.SpanID).To(MatchRegexp(b3SpanRegex))
					Expect(reqInfo.TraceInfo.UUID).To(MatchRegexp(UUIDRegex))
				})
			})
		})
		Context("with a tenantID set", func() {
			BeforeEach(func() {
				handler = handlers.NewW3C(true, "tid", logger)
			})

			Context("when there are no pre-existing headers", func() {
				It("sets W3C headers and calls the next handler", func() {
					handler.ServeHTTP(resp, req, nextHandler)

					traceparentHeader := req.Header.Get(handlers.W3CTraceparentHeader)

					Expect(traceparentHeader).To(MatchRegexp(
						"^00-[a-f0-9]{32}-[a-f0-9]{16}-01$"), "Must match the W3C spec",
					)

					parentID := extractParentID(traceparentHeader)

					Expect(req.Header.Get(handlers.W3CTracestateHeader)).To(
						Equal(fmt.Sprintf("tid@gorouter=%s", parentID)),
					)

					Expect(nextCalled).To(BeTrue(), "Expected the next handler to be called.")
				})
			})

			Context("when there are pre-existing headers", func() {
				BeforeEach(func() {
					req.Header.Set(
						handlers.W3CTraceparentHeader,
						"00-11111111111111111111111111111111-2222222222222222-01",
					)

					req.Header.Set(
						handlers.W3CTracestateHeader,
						"rojo=00f067aa0ba902b7,congo=t61rcWkgMzE",
					)
				})

				It("sets W3C headers and calls the next handler", func() {
					handler.ServeHTTP(resp, req, nextHandler)

					traceparentHeader := req.Header.Get(handlers.W3CTraceparentHeader)

					Expect(traceparentHeader).To(MatchRegexp(
						"^00-[1]{32}-[a-f0-9]{16}-01$"),
						"Must update the parent ID but not the trace ID",
					)

					parentID := extractParentID(traceparentHeader)

					Expect(req.Header.Get(handlers.W3CTracestateHeader)).To(
						Equal(fmt.Sprintf(
							"tid@gorouter=%s,rojo=00f067aa0ba902b7,congo=t61rcWkgMzE", parentID,
						)),
					)

					Expect(nextCalled).To(BeTrue(), "Expected the next handler to be called.")
				})

				It("sets request context", func() {
					handler.ServeHTTP(resp, req, nextHandler)
					Expect(reqInfo.TraceInfo.TraceID).To(MatchRegexp(b3IDRegex))
					Expect(reqInfo.TraceInfo.SpanID).To(MatchRegexp(b3SpanRegex))
					Expect(reqInfo.TraceInfo.UUID).To(MatchRegexp(UUIDRegex))
				})
			})

			Context("when there are pre-existing headers including gorouter which has a different tenant ID", func() {
				BeforeEach(func() {
					req.Header.Set(
						handlers.W3CTraceparentHeader,
						"00-11111111111111111111111111111111-2222222222222222-01",
					)

					req.Header.Set(
						handlers.W3CTracestateHeader,
						"rojo=00f067aa0ba902b7,gorouter=t61rcWkgMzE",
					)
				})

				It("sets W3C headers, without replacing the older gorouter header, and calls the next handler", func() {
					handler.ServeHTTP(resp, req, nextHandler)

					traceparentHeader := req.Header.Get(handlers.W3CTraceparentHeader)

					Expect(traceparentHeader).To(MatchRegexp(
						"^00-[1]{32}-[a-f0-9]{16}-01$"),
						"Must update the parent ID but not the trace ID",
					)

					parentID := extractParentID(traceparentHeader)

					Expect(req.Header.Get(handlers.W3CTracestateHeader)).To(
						Equal(fmt.Sprintf("tid@gorouter=%s,rojo=00f067aa0ba902b7,gorouter=t61rcWkgMzE", parentID)),
						"The other gorouter value should not be replaced by the newer parentID as they have separate tenant IDs",
					)

					Expect(nextCalled).To(BeTrue(), "Expected the next handler to be called.")
				})

				It("sets request context", func() {
					handler.ServeHTTP(resp, req, nextHandler)
					Expect(reqInfo.TraceInfo.TraceID).To(MatchRegexp(b3IDRegex))
					Expect(reqInfo.TraceInfo.SpanID).To(MatchRegexp(b3SpanRegex))
					Expect(reqInfo.TraceInfo.UUID).To(MatchRegexp(UUIDRegex))
				})
			})

			Context("when there are pre-existing headers including gorouter which has the same tenant ID", func() {
				BeforeEach(func() {
					req.Header.Set(
						handlers.W3CTraceparentHeader,
						"00-11111111111111111111111111111111-2222222222222222-01",
					)

					req.Header.Set(
						handlers.W3CTracestateHeader,
						"rojo=00f067aa0ba902b7,tid@gorouter=t61rcWkgMzE",
					)
				})

				It("sets W3C headers, replacing the other gorouter header, and calls the next handler", func() {
					handler.ServeHTTP(resp, req, nextHandler)

					traceparentHeader := req.Header.Get(handlers.W3CTraceparentHeader)

					Expect(traceparentHeader).To(MatchRegexp(
						"^00-[1]{32}-[a-f0-9]{16}-01$"),
						"Must update the parent ID but not the trace ID",
					)

					parentID := extractParentID(traceparentHeader)

					Expect(req.Header.Get(handlers.W3CTracestateHeader)).To(
						Equal(fmt.Sprintf("tid@gorouter=%s,rojo=00f067aa0ba902b7", parentID)),
						"The other gorouter value should be replaced by the newer parentID as they have same tenant IDs",
					)

					Expect(nextCalled).To(BeTrue(), "Expected the next handler to be called.")
				})

				It("sets request context", func() {
					handler.ServeHTTP(resp, req, nextHandler)
					Expect(reqInfo.TraceInfo.TraceID).To(MatchRegexp(b3IDRegex))
					Expect(reqInfo.TraceInfo.SpanID).To(MatchRegexp(b3SpanRegex))
					Expect(reqInfo.TraceInfo.UUID).To(MatchRegexp(UUIDRegex))
				})
			})
		})

	})

	Context("with W3C disabled", func() {
		BeforeEach(func() {
			handler = handlers.NewW3C(false, "", logger)
		})

		It("doesn't set any headers", func() {
			handler.ServeHTTP(resp, req, nextHandler)

			Expect(req.Header.Get(handlers.W3CTraceparentHeader)).To(BeEmpty())
			Expect(req.Header.Get(handlers.W3CTracestateHeader)).To(BeEmpty())

			Expect(nextCalled).To(BeTrue(), "Expected the next handler to be called.")
		})

		It("sets request context", func() {
			handler.ServeHTTP(resp, req, nextHandler)
			Expect(reqInfo.TraceInfo.TraceID).To(BeEmpty())
			Expect(reqInfo.TraceInfo.SpanID).To(BeEmpty())
			Expect(reqInfo.TraceInfo.UUID).To(BeEmpty())
		})
	})
})
