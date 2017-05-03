package handlers_test

import (
	"bytes"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"time"

	"code.cloudfoundry.org/gorouter/handlers"
	"code.cloudfoundry.org/gorouter/test_util"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/urfave/negroni"
)

var _ = Describe("RequestInfo", func() {
	var (
		handler negroni.Handler

		resp http.ResponseWriter
		req  *http.Request

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

		handler = handlers.NewRequestInfo()

		reqChan = make(chan *http.Request, 1)

		nextCalled = false
	})

	AfterEach(func() {
		Expect(nextCalled).To(BeTrue(), "Expected the next handler to be called.")
		close(reqChan)
	})

	It("sets RequestInfo with StartTime on the context", func() {
		handler.ServeHTTP(resp, req, nextHandler)
		var contextReq *http.Request
		Eventually(reqChan).Should(Receive(&contextReq))

		expectedStartTime := time.Now()

		ri, err := handlers.ContextRequestInfo(contextReq)
		Expect(err).ToNot(HaveOccurred())
		Expect(ri).ToNot(BeNil())
		Expect(ri.StartedAt).To(BeTemporally("~", expectedStartTime))

	})

})
