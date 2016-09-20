package handlers_test

import (
	"bytes"
	"io/ioutil"
	"net/http"
	"net/http/httptest"

	"code.cloudfoundry.org/gorouter/access_log/fakes"
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

var _ = Describe("AccessLog", func() {
	var (
		handler negroni.Handler
		logger  lager.Logger

		resp        http.ResponseWriter
		proxyWriter utils.ProxyResponseWriter
		req         *http.Request

		accessLogger      *fakes.FakeAccessLogger
		extraHeadersToLog []string

		nextCalled bool
	)

	nextHandler := http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		_, err := ioutil.ReadAll(req.Body)
		Expect(err).NotTo(HaveOccurred())

		rw.WriteHeader(http.StatusTeapot)
		rw.Write([]byte("I'm a little teapot, short and stout."))

		nextCalled = true
	})

	BeforeEach(func() {
		logger = lagertest.NewTestLogger("zipkin")
		body := bytes.NewBufferString("What are you?")
		req = test_util.NewRequest("GET", "example.com", "/", body)
		resp = httptest.NewRecorder()
		proxyWriter = utils.NewProxyResponseWriter(resp)

		extraHeadersToLog = []string{}

		accessLogger = &fakes.FakeAccessLogger{}

		handler = handlers.NewAccessLog(accessLogger, &extraHeadersToLog)

		nextCalled = false
	})

	AfterEach(func() {
		Expect(nextCalled).To(BeTrue(), "Expected the next handler to be called.")
	})

	It("sets an access log record on the context", func() {
		handler.ServeHTTP(proxyWriter, req, nextHandler)
		alr := proxyWriter.Context().Value("AccessLogRecord")
		Expect(alr).ToNot(BeNil())
		Expect(alr).To(BeAssignableToTypeOf(&schema.AccessLogRecord{}))
	})

	It("logs the access log record after all subsequent handlers have run", func() {
		handler.ServeHTTP(proxyWriter, req, nextHandler)

		Expect(accessLogger.LogCallCount()).To(Equal(1))

		alr := accessLogger.LogArgsForCall(0)

		Expect(alr.StartedAt).ToNot(BeZero())
		Expect(alr.Request).To(Equal(req))
		Expect(*alr.ExtraHeadersToLog).To(Equal(extraHeadersToLog))
		Expect(alr.FinishedAt).ToNot(BeZero())
		Expect(alr.RequestBytesReceived).To(Equal(13))
		Expect(alr.BodyBytesSent).To(Equal(37))
	})
})
