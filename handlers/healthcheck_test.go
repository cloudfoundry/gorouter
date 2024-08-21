package handlers_test

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"

	"github.com/onsi/gomega/gbytes"
	"go.uber.org/zap/zapcore"

	"code.cloudfoundry.org/gorouter/common/health"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"code.cloudfoundry.org/gorouter/handlers"
	log "code.cloudfoundry.org/gorouter/logger"
	"code.cloudfoundry.org/gorouter/test_util"
)

var _ = Describe("Healthcheck", func() {
	var (
		handler      http.Handler
		testSink     *test_util.TestSink
		logger       *slog.Logger
		resp         *httptest.ResponseRecorder
		req          *http.Request
		healthStatus *health.Health
	)

	BeforeEach(func() {
		logger = log.CreateLogger()
		testSink = &test_util.TestSink{Buffer: gbytes.NewBuffer()}
		log.SetDynamicWriteSyncer(zapcore.NewMultiWriteSyncer(testSink, zapcore.AddSync(GinkgoWriter)))
		log.SetLoggingLevel("Debug")
		req = test_util.NewRequest("GET", "example.com", "/", nil)
		resp = httptest.NewRecorder()
		healthStatus = &health.Health{}
		healthStatus.SetHealth(health.Healthy)

		handler = handlers.NewHealthcheck(healthStatus, logger)
	})

	It("closes the request", func() {
		handler.ServeHTTP(resp, req)
		Expect(req.Close).To(BeTrue())
	})

	It("responds with 200 OK", func() {
		handler.ServeHTTP(resp, req)
		Expect(resp.Code).To(Equal(200))
		bodyString, err := io.ReadAll(resp.Body)
		Expect(err).ToNot(HaveOccurred())
		Expect(bodyString).To(ContainSubstring("ok\n"))
	})

	It("sets the Cache-Control and Expires headers", func() {
		handler.ServeHTTP(resp, req)
		Expect(resp.Header().Get("Cache-Control")).To(Equal("private, max-age=0"))
		Expect(resp.Header().Get("Expires")).To(Equal("0"))
	})

	Context("when draining is in progress", func() {
		BeforeEach(func() {
			healthStatus.SetHealth(health.Degraded)
		})

		It("responds with a 503 Service Unavailable", func() {
			handler.ServeHTTP(resp, req)
			Expect(resp.Code).To(Equal(503))
		})

		It("sets the Cache-Control and Expires headers", func() {
			handler.ServeHTTP(resp, req)
			Expect(resp.Header().Get("Cache-Control")).To(Equal("private, max-age=0"))
			Expect(resp.Header().Get("Expires")).To(Equal("0"))
		})
	})
})
