package handlers_test

import (
	"errors"
	"net/http"
	"net/http/httptest"

	router_http "code.cloudfoundry.org/gorouter/common/http"

	"code.cloudfoundry.org/gorouter/common/health"

	"code.cloudfoundry.org/gorouter/handlers"
	"code.cloudfoundry.org/gorouter/logger"
	"code.cloudfoundry.org/gorouter/test_util"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gbytes"
	"github.com/urfave/negroni"
)

var _ = Describe("Paniccheck", func() {
	var (
		healthStatus *health.Health
		testLogger   logger.Logger
		panicHandler negroni.Handler
		request      *http.Request
		recorder     *httptest.ResponseRecorder
	)

	BeforeEach(func() {
		healthStatus = &health.Health{}
		healthStatus.SetHealth(health.Healthy)

		testLogger = test_util.NewTestZapLogger("test")
		request = httptest.NewRequest("GET", "http://example.com/foo", nil)
		request.Host = "somehost.com"
		recorder = httptest.NewRecorder()
		panicHandler = handlers.NewPanicCheck(healthStatus, testLogger)
	})

	Context("when something panics", func() {
		var expectedPanic = func(http.ResponseWriter, *http.Request) {
			panic(errors.New("we expect this panic"))
		}

		It("responds with a 502 Bad Gateway Error", func() {
			panicHandler.ServeHTTP(recorder, request, expectedPanic)
			resp := recorder.Result()
			Expect(resp.StatusCode).To(Equal(502))
		})

		It("responds with an x-cf-RouterError", func() {
			panicHandler.ServeHTTP(recorder, request, expectedPanic)
			Expect(recorder.Header().Get(router_http.CfRouterError)).To(Equal("unknown_failure"))
		})

		It("logs the panic message with Host", func() {
			panicHandler.ServeHTTP(recorder, request, expectedPanic)
			Expect(testLogger).To(gbytes.Say("somehost.com"))
			Expect(testLogger).To(gbytes.Say("we expect this panic"))
			Expect(testLogger).To(gbytes.Say("stacktrace"))
		})
	})

	Context("when there is no panic", func() {
		var noop = func(http.ResponseWriter, *http.Request) {}

		It("leaves the healthcheck set to true", func() {
			panicHandler.ServeHTTP(recorder, request, noop)
			Expect(healthStatus.Health()).To(Equal(health.Healthy))
		})

		It("responds with a 200", func() {
			panicHandler.ServeHTTP(recorder, request, noop)
			resp := recorder.Result()
			Expect(resp.StatusCode).To(Equal(200))
		})

		It("does not log anything", func() {
			panicHandler.ServeHTTP(recorder, request, noop)
			Expect(testLogger).NotTo(gbytes.Say("panic-check"))
		})
	})

	Context("when the panic is due to an abort", func() {
		var errAbort = func(http.ResponseWriter, *http.Request) {
			// The ErrAbortHandler panic occurs when a client goes away in the middle of a request
			// this is a panic we expect to see in normal operation and is safe to allow the panic
			// http.Server will handle it appropriately
			panic(http.ErrAbortHandler)
		}

		It("the healthStatus is still healthy", func() {
			Expect(func() {
				panicHandler.ServeHTTP(recorder, request, errAbort)
			}).To(Panic())

			Expect(healthStatus.Health()).To(Equal(health.Healthy))
		})

		It("does not log anything", func() {
			Expect(func() {
				panicHandler.ServeHTTP(recorder, request, errAbort)
			}).To(Panic())

			Expect(testLogger).NotTo(gbytes.Say("panic-check"))
		})
	})
})
