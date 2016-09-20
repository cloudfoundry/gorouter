package handlers_test

import (
	"io/ioutil"
	"net/http"
	"net/http/httptest"

	"code.cloudfoundry.org/gorouter/access_log/schema"
	"code.cloudfoundry.org/gorouter/handlers"
	"code.cloudfoundry.org/gorouter/proxy/utils"
	"code.cloudfoundry.org/gorouter/test_util"
	"code.cloudfoundry.org/lager"
	"code.cloudfoundry.org/lager/lagertest"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/urfave/negroni"
)

var _ = Describe("Healthcheck", func() {
	var (
		handler     negroni.Handler
		logger      lager.Logger
		resp        *httptest.ResponseRecorder
		proxyWriter utils.ProxyResponseWriter
		req         *http.Request
		alr         *schema.AccessLogRecord
		nextCalled  bool
		heartbeatOK int32
	)

	nextHandler := http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		nextCalled = true
	})

	TestHealthcheckOK := func() {
		It("closes the request", func() {
			handler.ServeHTTP(proxyWriter, req, nextHandler)
			Expect(req.Close).To(BeTrue())
			Expect(nextCalled).To(BeFalse())
		})

		It("responds with 200 OK", func() {
			handler.ServeHTTP(proxyWriter, req, nextHandler)
			Expect(resp.Code).To(Equal(200))
			bodyString, err := ioutil.ReadAll(resp.Body)
			Expect(err).ToNot(HaveOccurred())
			Expect(bodyString).To(ContainSubstring("ok\n"))
		})

		It("sets the access log record's status code to 200", func() {
			handler.ServeHTTP(proxyWriter, req, nextHandler)
			Expect(alr.StatusCode).To(Equal(200))
		})

		It("sets the Cache-Control and Expires headers", func() {
			handler.ServeHTTP(proxyWriter, req, nextHandler)
			Expect(resp.Header().Get("Cache-Control")).To(Equal("private, max-age=0"))
			Expect(resp.Header().Get("Expires")).To(Equal("0"))
		})
	}

	TestHealthcheckServiceUnavailable := func() {
		It("closes the request", func() {
			handler.ServeHTTP(proxyWriter, req, nextHandler)
			Expect(req.Close).To(BeTrue())
			Expect(nextCalled).To(BeFalse())
		})

		It("responds with a 503 Service Unavailable", func() {
			handler.ServeHTTP(proxyWriter, req, nextHandler)
			Expect(resp.Code).To(Equal(503))
		})

		It("sets the access log record's status code to 503", func() {
			handler.ServeHTTP(proxyWriter, req, nextHandler)
			Expect(alr.StatusCode).To(Equal(503))
		})
		It("sets the Cache-Control and Expires headers", func() {
			handler.ServeHTTP(proxyWriter, req, nextHandler)
			Expect(resp.Header().Get("Cache-Control")).To(Equal("private, max-age=0"))
			Expect(resp.Header().Get("Expires")).To(Equal("0"))
		})
	}

	BeforeEach(func() {
		logger = lagertest.NewTestLogger("zipkin")
		req = test_util.NewRequest("GET", "example.com", "/", nil)
		resp = httptest.NewRecorder()
		proxyWriter = utils.NewProxyResponseWriter(resp)
		alr = &schema.AccessLogRecord{
			Request: req,
		}
		proxyWriter.AddToContext("AccessLogRecord", alr)
		nextCalled = false
		heartbeatOK = 1
	})

	Context("with User-Agent checking", func() {
		BeforeEach(func() {
			handler = handlers.NewHealthcheck("HTTP-Monitor/1.1", &heartbeatOK, logger)
		})

		Context("when User-Agent is set to the healthcheck User-Agent", func() {
			BeforeEach(func() {
				req.Header.Set("User-Agent", "HTTP-Monitor/1.1")
			})

			TestHealthcheckOK()

			Context("when draining is in progress", func() {
				BeforeEach(func() {
					heartbeatOK = 0
				})

				TestHealthcheckServiceUnavailable()
			})
		})

		Context("when User-Agent is not set to the healthcheck User-Agent", func() {
			It("does not close the request and forwards the request to the next handler", func() {
				handler.ServeHTTP(proxyWriter, req, nextHandler)
				Expect(req.Close).To(BeFalse())
				Expect(nextCalled).To(BeTrue())
			})

			It("does not set anything on the response", func() {
				handler.ServeHTTP(proxyWriter, req, nextHandler)
				Expect(resp.Header().Get("Cache-Control")).To(BeEmpty())
				Expect(resp.Header().Get("Expires")).To(BeEmpty())
				bodyString, err := ioutil.ReadAll(resp.Body)
				Expect(err).ToNot(HaveOccurred())
				Expect(bodyString).To(BeEmpty())
			})

			It("does not set the access log record's status code to 200", func() {
				handler.ServeHTTP(proxyWriter, req, nextHandler)
				Expect(alr.StatusCode).To(Equal(0))
			})
		})
	})

	Context("without User-Agent checking", func() {
		BeforeEach(func() {
			handler = handlers.NewHealthcheck("", &heartbeatOK, logger)
		})

		TestHealthcheckOK()

		Context("when draining is in progress", func() {
			BeforeEach(func() {
				heartbeatOK = 0
			})

			TestHealthcheckServiceUnavailable()
		})

		Context("when User-Agent is set to something", func() {
			BeforeEach(func() {
				req.Header.Set("User-Agent", "HTTP-Monitor/1.1")
			})
			TestHealthcheckOK()
		})
	})
})
