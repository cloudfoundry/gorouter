package handlers_test

import (
	"bytes"
	"io/ioutil"
	"net/http"
	"net/http/httptest"

	"code.cloudfoundry.org/gorouter/accesslog/fakes"
	"code.cloudfoundry.org/gorouter/handlers"
	logger_fakes "code.cloudfoundry.org/gorouter/logger/fakes"
	"code.cloudfoundry.org/gorouter/proxy/utils"
	"code.cloudfoundry.org/gorouter/route"
	"code.cloudfoundry.org/gorouter/test_util"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/urfave/negroni"
)

var _ = Describe("AccessLog", func() {
	var (
		handler *negroni.Negroni

		resp http.ResponseWriter
		req  *http.Request

		fakeLogger        *logger_fakes.FakeLogger
		accessLogger      *fakes.FakeAccessLogger
		extraHeadersToLog []string

		nextCalled bool

		reqChan chan *http.Request
	)
	testEndpoint := route.NewEndpoint(&route.EndpointOpts{
		Host: "host",
		Port: 1234,
	})
	testHeaders := http.Header{
		"Foo":               []string{"foobar"},
		"X-Forwarded-For":   []string{"1.2.3.4"},
		"X-Forwarded-Proto": []string{"https"},
	}

	nextHandler := negroni.HandlerFunc(func(rw http.ResponseWriter, req *http.Request, next http.HandlerFunc) {
		_, err := ioutil.ReadAll(req.Body)
		Expect(err).NotTo(HaveOccurred())

		rw.WriteHeader(http.StatusTeapot)
		rw.Write([]byte("I'm a little teapot, short and stout."))

		reqInfo, err := handlers.ContextRequestInfo(req)
		if err == nil {
			reqInfo.RouteEndpoint = testEndpoint
		}

		if next != nil {
			next(rw, req)
		}

		reqChan <- req
		nextCalled = true
	})

	testProxyWriterHandler := func(rw http.ResponseWriter, req *http.Request, next http.HandlerFunc) {
		proxyWriter := utils.NewProxyResponseWriter(rw)
		next(proxyWriter, req)
	}

	BeforeEach(func() {
		body := bytes.NewBufferString("What are you?")
		req = test_util.NewRequest("GET", "example.com", "/", body)
		resp = httptest.NewRecorder()

		extraHeadersToLog = []string{}

		accessLogger = &fakes.FakeAccessLogger{}

		fakeLogger = new(logger_fakes.FakeLogger)

		handler = negroni.New()
		handler.Use(handlers.NewRequestInfo())
		handler.Use(handlers.NewProxyWriter(fakeLogger))
		handler.Use(handlers.NewAccessLog(accessLogger, extraHeadersToLog, false, fakeLogger))
		handler.Use(nextHandler)

		reqChan = make(chan *http.Request, 1)

		nextCalled = false
	})

	AfterEach(func() {
		Expect(nextCalled).To(BeTrue(), "Expected the next handler to be called.")
		close(reqChan)
	})

	It("logs the access log record after all subsequent handlers have run", func() {
		handler.ServeHTTP(resp, req)

		Expect(accessLogger.LogCallCount()).To(Equal(1))

		alr := accessLogger.LogArgsForCall(0)

		Expect(alr.ReceivedAt).ToNot(BeZero())
		Expect(alr.Request.Header).To(Equal(req.Header))
		Expect(alr.Request.Method).To(Equal(req.Method))
		Expect(alr.Request.URL).To(Equal(req.URL))
		Expect(alr.Request.RemoteAddr).To(Equal(req.RemoteAddr))
		Expect(alr.ExtraHeadersToLog).To(Equal(extraHeadersToLog))
		Expect(alr.FinishedAt).ToNot(BeZero())
		Expect(alr.RequestBytesReceived).To(Equal(13))
		Expect(alr.BodyBytesSent).To(Equal(37))
		Expect(alr.StatusCode).To(Equal(http.StatusTeapot))
		Expect(alr.RouteEndpoint).To(Equal(testEndpoint))
		Expect(alr.HeadersOverride).To(BeNil())
		Expect(alr.RouterError).To(BeEmpty())
	})

	Context("when there are backend request headers on the context", func() {
		BeforeEach(func() {
			extraHeadersHandler := http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
				reqInfo, err := handlers.ContextRequestInfo(req)
				if err == nil {
					reqInfo.BackendReqHeaders = testHeaders
				}
			})

			handler.UseHandlerFunc(extraHeadersHandler)
		})
		It("uses those headers instead", func() {
			handler.ServeHTTP(resp, req)

			Expect(accessLogger.LogCallCount()).To(Equal(1))

			alr := accessLogger.LogArgsForCall(0)

			Expect(alr.Request.Header).To(Equal(req.Header))
			Expect(alr.Request.Method).To(Equal(req.Method))
			Expect(alr.Request.URL).To(Equal(req.URL))
			Expect(alr.Request.RemoteAddr).To(Equal(req.RemoteAddr))
			Expect(alr.HeadersOverride).To(Equal(testHeaders))
		})
	})

	Context("when request info is not set on the request context", func() {
		BeforeEach(func() {
			handler = negroni.New()
			handler.UseFunc(testProxyWriterHandler)
			handler.Use(handlers.NewAccessLog(accessLogger, extraHeadersToLog, false, fakeLogger))
			handler.Use(nextHandler)
		})
		It("calls Panic on the logger", func() {
			handler.ServeHTTP(resp, req)
			Expect(fakeLogger.PanicCallCount()).To(Equal(1))
		})
	})

	Context("when there is an X-Cf-RouterError header on the response", func() {
		BeforeEach(func() {
			resp.Header().Add("X-Cf-RouterError", "endpoint-failed")
		})

		It("logs the header and value", func() {
			handler.ServeHTTP(resp, req)
			Expect(accessLogger.LogCallCount()).To(Equal(1))

			alr := accessLogger.LogArgsForCall(0)

			Expect(alr.RouterError).To(Equal("endpoint-failed"))
		})
	})

})
