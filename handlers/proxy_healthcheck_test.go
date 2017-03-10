package handlers_test

import (
	"context"
	"io/ioutil"
	"net/http"
	"net/http/httptest"

	"code.cloudfoundry.org/gorouter/access_log/schema"
	"code.cloudfoundry.org/gorouter/handlers"
	"code.cloudfoundry.org/gorouter/logger"
	"code.cloudfoundry.org/gorouter/test_util"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gbytes"
	"github.com/urfave/negroni"
)

var _ = Describe("Proxy Healthcheck", func() {
	var (
		handler     negroni.Handler
		logger      logger.Logger
		resp        *httptest.ResponseRecorder
		req         *http.Request
		heartbeatOK int32
		nextHandler http.HandlerFunc
		nextCalled  bool
		alr         *schema.AccessLogRecord
	)
	var alrFromContext = func(req *http.Request) *schema.AccessLogRecord {
		accessLogRecordFromContext := req.Context().Value("AccessLogRecord")
		accessLog, ok := accessLogRecordFromContext.(*schema.AccessLogRecord)
		Expect(ok).To(BeTrue())
		return accessLog

	}
	BeforeEach(func() {
		logger = test_util.NewTestZapLogger("healthcheck")
		req = test_util.NewRequest("GET", "example.com", "/", nil)
		resp = httptest.NewRecorder()
		heartbeatOK = 1

		handler = handlers.NewProxyHealthcheck("HTTP-Monitor/1.1", &heartbeatOK, logger)
		nextHandler = http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
			nextCalled = true
		})

		alr = &schema.AccessLogRecord{
			Request: req,
		}
	})

	AfterEach(func() {
		nextCalled = false
	})

	Context("when no access log record is set on the request", func() {
		It("logs an error when Access Log Record", func() {
			handler.ServeHTTP(resp, req, nextHandler)

			accessLogRecordFromContext := req.Context().Value("AccessLogRecord")
			Expect(accessLogRecordFromContext).To(BeNil())

			Expect(logger).To(gbytes.Say("AccessLogRecord-not-set-on-context"))
		})
	})

	Context("when User-Agent is set to the healthcheck User-Agent", func() {
		BeforeEach(func() {
			req.Header.Set("User-Agent", "HTTP-Monitor/1.1")
			req = req.WithContext(context.WithValue(req.Context(), "AccessLogRecord", alr))
		})

		It("sets the access log record status", func() {
			handler.ServeHTTP(resp, req, nextHandler)
			accessLog := alrFromContext(req)
			Expect(accessLog.StatusCode).To(Equal(200))
		})

		It("closes the request", func() {
			handler.ServeHTTP(resp, req, nextHandler)
			Expect(req.Close).To(BeTrue())
			Expect(nextCalled).To(BeFalse())
		})

		It("responds with 200 OK", func() {
			handler.ServeHTTP(resp, req, nextHandler)
			Expect(resp.Code).To(Equal(200))
			bodyString, err := ioutil.ReadAll(resp.Body)
			Expect(err).ToNot(HaveOccurred())
			Expect(bodyString).To(ContainSubstring("ok\n"))
		})

		It("sets the Cache-Control and Expires headers", func() {
			handler.ServeHTTP(resp, req, nextHandler)
			Expect(resp.Header().Get("Cache-Control")).To(Equal("private, max-age=0"))
			Expect(resp.Header().Get("Expires")).To(Equal("0"))
		})

		Context("when draining is in progress", func() {
			BeforeEach(func() {
				heartbeatOK = 0
			})

			It("closes the request", func() {
				handler.ServeHTTP(resp, req, nextHandler)
				Expect(req.Close).To(BeTrue())
				Expect(nextCalled).To(BeFalse())
			})

			It("responds with a 503 Service Unavailable", func() {
				handler.ServeHTTP(resp, req, nextHandler)
				Expect(resp.Code).To(Equal(503))
			})

			It("sets the access log record's status code to 503", func() {
				handler.ServeHTTP(resp, req, nextHandler)
				accessLog := alrFromContext(req)
				Expect(accessLog.StatusCode).To(Equal(503))
			})

			It("sets the Cache-Control and Expires headers", func() {
				handler.ServeHTTP(resp, req, nextHandler)
				Expect(resp.Header().Get("Cache-Control")).To(Equal("private, max-age=0"))
				Expect(resp.Header().Get("Expires")).To(Equal("0"))
			})
		})
	})

	Context("when User-Agent is not set to the healthcheck User-Agent", func() {
		BeforeEach(func() {
			// req.Header.Set("User-Agent", "HTTP-Monitor/1.1")
			req.Header.Set("User-Agent", "test-agent")
			req = req.WithContext(context.WithValue(req.Context(), "AccessLogRecord", alr))
		})
		It("does not set anything on the response", func() {
			handler.ServeHTTP(resp, req, nextHandler)
			Expect(resp.Header().Get("Cache-Control")).To(BeEmpty())
			Expect(resp.Header().Get("Expires")).To(BeEmpty())
			bodyString, err := ioutil.ReadAll(resp.Body)
			Expect(err).ToNot(HaveOccurred())
			Expect(bodyString).To(BeEmpty())
		})

		It("does not close the request and forwards the request to the next handler", func() {
			handler.ServeHTTP(resp, req, nextHandler)
			Expect(req.Close).To(BeFalse())
			Expect(nextCalled).To(BeTrue())
		})

		It("does not set the access log record's status code to 200", func() {
			handler.ServeHTTP(resp, req, nextHandler)
			accessLog := alrFromContext(req)
			Expect(accessLog.StatusCode).To(Equal(0))
		})
	})
})
