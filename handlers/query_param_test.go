package handlers_test

import (
	"bytes"
	"io/ioutil"
	"net/http"
	"net/http/httptest"

	router_http "code.cloudfoundry.org/gorouter/common/http"
	"code.cloudfoundry.org/gorouter/common/uuid"
	"code.cloudfoundry.org/gorouter/handlers"
	logger_fakes "code.cloudfoundry.org/gorouter/logger/fakes"
	"code.cloudfoundry.org/gorouter/route"
	"code.cloudfoundry.org/gorouter/test_util"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/uber-go/zap"
	"github.com/urfave/negroni"
)

var _ = Describe("QueryParamHandler", func() {
	var (
		handler *negroni.Negroni

		resp http.ResponseWriter
		req  *http.Request

		fakeLogger *logger_fakes.FakeLogger

		nextCalled bool

		reqChan chan *http.Request
	)
	testEndpoint := route.NewEndpoint(&route.EndpointOpts{
		Host: "host",
		Port: 1234,
	})

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

	BeforeEach(func() {
		body := bytes.NewBufferString("What are you?")
		req = test_util.NewRequest("GET", "example.com", "/", body)
		resp = httptest.NewRecorder()

		fakeLogger = new(logger_fakes.FakeLogger)

		handler = negroni.New()
		handler.Use(handlers.NewRequestInfo())
		handler.Use(handlers.NewProxyWriter(fakeLogger))
		handler.Use(handlers.NewQueryParam(fakeLogger))
		handler.Use(nextHandler)

		reqChan = make(chan *http.Request, 1)

		nextCalled = false
	})

	AfterEach(func() {
		Expect(nextCalled).To(BeTrue(), "Expected the next handler to be called.")
		close(reqChan)
	})

	Context("when checking if semicolons are in the request", func() {
		var id string
		var err error
		BeforeEach(func() {
			id, err = uuid.GenerateUUID()
			Expect(err).ToNot(HaveOccurred())
			req.Header.Add(handlers.VcapRequestIdHeader, id)
		})

		Context("when semicolons are present", func() {
			It("logs a warning", func() {
				req.RequestURI = "/example?param1;param2"
				handler.ServeHTTP(resp, req)

				Expect(fakeLogger.WarnCallCount()).To(Equal(1))
				msg, fields := fakeLogger.WarnArgsForCall(0)
				Expect(msg).To(Equal("deprecated-semicolon-params"))
				Expect(fields).To(Equal([]zap.Field{zap.String("vcap_request_id", id)}))

				Expect(resp.Header().Get(router_http.CfRouterError)).To(Equal("deprecated-semicolon-params"))
			})
		})
		Context("when semicolons are not present", func() {
			It("does not log a warning", func() {
				req.RequestURI = "/example?param1&param2"
				handler.ServeHTTP(resp, req)

				Expect(fakeLogger.WarnCallCount()).To(Equal(0))
				Expect(resp.Header().Get(router_http.CfRouterError)).To(Equal(""))
			})
		})
	})

})
