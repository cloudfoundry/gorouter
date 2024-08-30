package handlers_test

import (
	"io"
	"net/http"
	"net/http/httptest"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"code.cloudfoundry.org/gorouter/common/health"
	"code.cloudfoundry.org/gorouter/handlers"
	"code.cloudfoundry.org/gorouter/test_util"
)

var _ = Describe("Healthcheck", func() {
	var (
		handler      http.Handler
		logger       *test_util.TestLogger
		resp         *httptest.ResponseRecorder
		req          *http.Request
		healthStatus *health.Health
	)

	BeforeEach(func() {
		logger = test_util.NewTestLogger("healthcheck")
		req = test_util.NewRequest("GET", "example.com", "/", nil)
		resp = httptest.NewRecorder()
		healthStatus = &health.Health{}
		healthStatus.SetHealth(health.Healthy)

		handler = handlers.NewHealthcheck(healthStatus, logger.Logger)
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
