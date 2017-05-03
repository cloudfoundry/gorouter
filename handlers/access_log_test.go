package handlers_test

import (
	"bytes"
	"io/ioutil"
	"net/http"
	"net/http/httptest"

	"code.cloudfoundry.org/gorouter/access_log/fakes"
	"code.cloudfoundry.org/gorouter/handlers"
	logger_fakes "code.cloudfoundry.org/gorouter/logger/fakes"
	"code.cloudfoundry.org/gorouter/test_util"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/urfave/negroni"
)

var _ = Describe("AccessLog", func() {
	var (
		handler *negroni.Negroni

		resp http.ResponseWriter
		req  *http.Request

		accessLogger      *fakes.FakeAccessLogger
		extraHeadersToLog []string

		nextCalled bool

		reqChan chan *http.Request
	)

	nextHandler := http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		_, err := ioutil.ReadAll(req.Body)
		Expect(err).NotTo(HaveOccurred())

		rw.WriteHeader(http.StatusTeapot)
		rw.Write([]byte("I'm a little teapot, short and stout."))

		reqChan <- req
		nextCalled = true
	})

	BeforeEach(func() {
		body := bytes.NewBufferString("What are you?")
		req = test_util.NewRequest("GET", "example.com", "/", body)
		resp = httptest.NewRecorder()

		extraHeadersToLog = []string{}

		accessLogger = &fakes.FakeAccessLogger{}

		fakeLogger := new(logger_fakes.FakeLogger)

		handler = negroni.New()
		handler.Use(handlers.NewRequestInfo())
		handler.Use(handlers.NewProxyWriter(fakeLogger))
		handler.Use(handlers.NewAccessLog(accessLogger, extraHeadersToLog))
		handler.UseHandlerFunc(nextHandler)

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

		Expect(alr.StartedAt).ToNot(BeZero())
		Expect(alr.Request.Header).To(Equal(req.Header))
		Expect(alr.Request.Method).To(Equal(req.Method))
		Expect(alr.Request.URL).To(Equal(req.URL))
		Expect(alr.Request.RemoteAddr).To(Equal(req.RemoteAddr))
		Expect(alr.ExtraHeadersToLog).To(Equal(extraHeadersToLog))
		Expect(alr.FinishedAt).ToNot(BeZero())
		Expect(alr.RequestBytesReceived).To(Equal(13))
		Expect(alr.BodyBytesSent).To(Equal(37))
		Expect(alr.StatusCode).To(Equal(http.StatusTeapot))
	})
})
