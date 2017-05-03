package handlers_test

import (
	"bytes"
	"io/ioutil"
	"net/http"
	"net/http/httptest"

	"code.cloudfoundry.org/gorouter/handlers"
	logger_fakes "code.cloudfoundry.org/gorouter/logger/fakes"
	"code.cloudfoundry.org/gorouter/proxy/utils"
	"code.cloudfoundry.org/gorouter/test_util"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/urfave/negroni"
)

var _ = Describe("ProxyWriter", func() {
	var (
		handler *negroni.Negroni

		resp http.ResponseWriter
		req  *http.Request

		nextCalled bool
		fakeLogger *logger_fakes.FakeLogger

		reqChan  chan *http.Request
		respChan chan http.ResponseWriter
	)

	nextHandler := http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		_, err := ioutil.ReadAll(req.Body)
		Expect(err).NotTo(HaveOccurred())

		rw.WriteHeader(http.StatusTeapot)
		rw.Write([]byte("I'm a little teapot, short and stout."))

		reqChan <- req
		respChan <- rw
		nextCalled = true
	})

	BeforeEach(func() {
		fakeLogger = new(logger_fakes.FakeLogger)
		body := bytes.NewBufferString("What are you?")
		req = test_util.NewRequest("GET", "example.com", "/", body)
		resp = httptest.NewRecorder()

		handler = negroni.New()
		handler.Use(handlers.NewRequestInfo())
		handler.Use(handlers.NewProxyWriter(fakeLogger))
		handler.UseHandlerFunc(nextHandler)

		reqChan = make(chan *http.Request, 1)
		respChan = make(chan http.ResponseWriter, 1)

		nextCalled = false
	})

	AfterEach(func() {
		close(reqChan)
		close(respChan)
	})

	It("sets the proxy response writer on the request context", func() {
		handler.ServeHTTP(resp, req)
		var contextReq *http.Request
		Eventually(reqChan).Should(Receive(&contextReq))
		reqInfo, err := handlers.ContextRequestInfo(contextReq)
		Expect(err).ToNot(HaveOccurred())
		Expect(reqInfo.ProxyResponseWriter).ToNot(BeNil())
		Expect(nextCalled).To(BeTrue(), "Expected the next handler to be called.")
	})

	It("passes the proxy response writer to the next handler", func() {
		handler.ServeHTTP(resp, req)
		var rw http.ResponseWriter
		Eventually(respChan).Should(Receive(&rw))
		Expect(rw).ToNot(BeNil())
		Expect(rw).To(BeAssignableToTypeOf(utils.NewProxyResponseWriter(resp)))
		Expect(nextCalled).To(BeTrue(), "Expected the next handler to be called.")
	})

	Context("when request info is not set on the request context", func() {
		var badHandler *negroni.Negroni
		BeforeEach(func() {
			badHandler = negroni.New()
			badHandler.Use(handlers.NewProxyWriter(fakeLogger))
			badHandler.UseHandlerFunc(nextHandler)
		})
		It("calls Fatal on the logger", func() {
			badHandler.ServeHTTP(resp, req)
			Expect(fakeLogger.FatalCallCount()).To(Equal(1))
			Expect(nextCalled).To(BeFalse())
		})
	})
})
