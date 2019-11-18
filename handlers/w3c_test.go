package handlers_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"regexp"

	"code.cloudfoundry.org/gorouter/handlers"
	"code.cloudfoundry.org/gorouter/test_util"

	"code.cloudfoundry.org/gorouter/logger"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("W3C", func() {
	extractParentID := func(traceparent string) string {
		r := regexp.MustCompile("^00-[a-f0-9]{32}-([a-f0-9]{16})-01$")

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
		logger     logger.Logger
		resp       http.ResponseWriter
		req        *http.Request
		nextCalled bool
	)

	nextHandler := http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		nextCalled = true
	})

	BeforeEach(func() {
		logger = test_util.NewTestZapLogger("w3c")
		req = test_util.NewRequest("GET", "example.com", "/", nil)
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
				It("sets W3C headers and calls the next handler", func() {
					handler.ServeHTTP(resp, req, nextHandler)

					traceparentHeader := req.Header.Get(handlers.W3CTraceparentHeader)

					Expect(traceparentHeader).To(MatchRegexp(
						"^00-[a-f0-9]{32}-[a-f0-9]{16}-01$"), "Must match the W3C spec",
					)

					parentID := extractParentID(traceparentHeader)

					Expect(req.Header.Get(handlers.W3CTracestateHeader)).To(
						Equal(fmt.Sprintf("gorouter=%s", parentID)),
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
							"gorouter=%s,rojo=00f067aa0ba902b7,congo=t61rcWkgMzE", parentID,
						)),
					)

					Expect(nextCalled).To(BeTrue(), "Expected the next handler to be called.")
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
	})
})
