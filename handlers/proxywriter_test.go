package handlers_test

import (
	"bytes"
	"io/ioutil"
	"net/http"
	"net/http/httptest"

	"code.cloudfoundry.org/gorouter/handlers"
	"code.cloudfoundry.org/gorouter/proxy/utils"
	"code.cloudfoundry.org/gorouter/test_util"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/urfave/negroni"
)

var _ = Describe("ProxyWriter", func() {
	var (
		handler negroni.Handler

		resp http.ResponseWriter
		req  *http.Request

		nextCalled bool

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
		body := bytes.NewBufferString("What are you?")
		req = test_util.NewRequest("GET", "example.com", "/", body)
		resp = httptest.NewRecorder()

		handler = handlers.NewProxyWriter()

		reqChan = make(chan *http.Request, 1)
		respChan = make(chan http.ResponseWriter, 1)

		nextCalled = false
	})

	AfterEach(func() {
		Expect(nextCalled).To(BeTrue(), "Expected the next handler to be called.")
		close(reqChan)
		close(respChan)
	})

	It("sets the proxy response writer on the request context", func() {
		handler.ServeHTTP(resp, req, nextHandler)
		var contextReq *http.Request
		Eventually(reqChan).Should(Receive(&contextReq))
		rw := contextReq.Context().Value(handlers.ProxyResponseWriterCtxKey)
		Expect(rw).ToNot(BeNil())
		Expect(rw).To(BeAssignableToTypeOf(utils.NewProxyResponseWriter(resp)))
	})

	It("passes the proxy response writer to the next handler", func() {
		handler.ServeHTTP(resp, req, nextHandler)
		var rw http.ResponseWriter
		Eventually(respChan).Should(Receive(&rw))
		Expect(rw).ToNot(BeNil())
		Expect(rw).To(BeAssignableToTypeOf(utils.NewProxyResponseWriter(resp)))
	})

})
