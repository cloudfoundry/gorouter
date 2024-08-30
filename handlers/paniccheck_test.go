package handlers_test

import (
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"

	"code.cloudfoundry.org/gorouter/common/health"
	router_http "code.cloudfoundry.org/gorouter/common/http"
	"code.cloudfoundry.org/gorouter/handlers"
	log "code.cloudfoundry.org/gorouter/logger"
	"code.cloudfoundry.org/gorouter/test_util"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gbytes"
	"github.com/urfave/negroni/v3"
	"go.uber.org/zap/zapcore"
)

var _ = Describe("Paniccheck", func() {
	var (
		healthStatus *health.Health
		testSink     *test_util.TestSink
		logger       *slog.Logger
		panicHandler negroni.Handler
		request      *http.Request
		recorder     *httptest.ResponseRecorder
	)

	BeforeEach(func() {
		healthStatus = &health.Health{}
		healthStatus.SetHealth(health.Healthy)

		logger = log.CreateLogger()
		testSink = &test_util.TestSink{Buffer: gbytes.NewBuffer()}
		log.SetDynamicWriteSyncer(zapcore.NewMultiWriteSyncer(testSink, zapcore.AddSync(GinkgoWriter)))
		log.SetLoggingLevel("Debug")
		request = httptest.NewRequest("GET", "http://example.com/foo", nil)
		request.Host = "somehost.com"
		recorder = httptest.NewRecorder()
		panicHandler = handlers.NewPanicCheck(healthStatus, logger)
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
			Expect(testSink.Lines()[0]).To(ContainSubstring("somehost.com"))
			Expect(string(testSink.Contents())).To(And(ContainSubstring("we expect this panic"), ContainSubstring("stacktrace")))
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
			Expect(string(testSink.Contents())).NotTo(ContainSubstring("panic-check"))
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
			Expect(string(testSink.Contents())).NotTo(ContainSubstring("panic-check"))
		})
	})
})
