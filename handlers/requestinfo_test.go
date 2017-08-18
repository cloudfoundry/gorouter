package handlers_test

import (
	"bytes"
	"context"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"time"

	"code.cloudfoundry.org/gorouter/handlers"
	"code.cloudfoundry.org/gorouter/route"
	"code.cloudfoundry.org/gorouter/test_util"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/urfave/negroni"
)

var _ = Describe("RequestInfoHandler", func() {
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

var _ = Describe("GetEndpoint", func() {
	var (
		ctx              context.Context
		requestInfo      *handlers.RequestInfo
		expectedEndpoint *route.Endpoint
	)

	BeforeEach(func() {
		// some hackery to set data on requestInfo using only exported symbols
		req, _ := http.NewRequest("banana", "", nil)
		rih := &handlers.RequestInfoHandler{}
		rih.ServeHTTP(nil, req, func(w http.ResponseWriter, r *http.Request) {
			ctx = r.Context()
			requestInfo, _ = handlers.ContextRequestInfo(r)
		})
		expectedEndpoint = &route.Endpoint{PrivateInstanceId: "some-id"}

		requestInfo.RouteEndpoint = expectedEndpoint
	})

	It("returns the endpoint private instance id", func() {
		endpoint, err := handlers.GetEndpoint(ctx)
		Expect(endpoint).To(Equal(expectedEndpoint))
		Expect(err).NotTo(HaveOccurred())
	})

	Context("when the context is missing the key", func() {
		BeforeEach(func() {
			ctx = context.Background()
		})

		It("returns a friendly error", func() {
			_, err := handlers.GetEndpoint(ctx)
			Expect(err).To(MatchError("RequestInfo not set on context"))
		})
	})

	Context("when the route endpoint is not set", func() {
		BeforeEach(func() {
			requestInfo.RouteEndpoint = nil
		})
		It("returns a friendly error", func() {
			_, err := handlers.GetEndpoint(ctx)
			Expect(err).To(MatchError("route endpoint not set on request info"))
		})

	})
})
