package handlers_test

import (
	"bytes"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"

	router_http "code.cloudfoundry.org/gorouter/common/http"
	"code.cloudfoundry.org/gorouter/common/uuid"
	"code.cloudfoundry.org/gorouter/handlers"
	log "code.cloudfoundry.org/gorouter/logger"
	"code.cloudfoundry.org/gorouter/route"
	"code.cloudfoundry.org/gorouter/test_util"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gbytes"
	"github.com/urfave/negroni/v3"
	"go.uber.org/zap/zapcore"
)

var _ = Describe("QueryParamHandler", func() {
	var (
		handler *negroni.Negroni

		resp http.ResponseWriter
		req  *http.Request

		testSink *test_util.TestSink
		logger   *slog.Logger

		prevHandler negroni.Handler

		nextCalled bool

		reqChan chan *http.Request
	)
	testEndpoint := route.NewEndpoint(&route.EndpointOpts{
		Host: "host",
		Port: 1234,
	})

	nextHandler := negroni.HandlerFunc(func(rw http.ResponseWriter, req *http.Request, next http.HandlerFunc) {
		_, err := io.ReadAll(req.Body)
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

		logger = log.CreateLoggerWithSource("test", "")
		testSink = &test_util.TestSink{Buffer: gbytes.NewBuffer()}
		log.SetDynamicWriteSyncer(zapcore.NewMultiWriteSyncer(testSink, zapcore.AddSync(GinkgoWriter)))
		log.SetLoggingLevel("Debug")

		reqChan = make(chan *http.Request, 1)

		nextCalled = false
		prevHandler = &PrevHandler{}
	})

	JustBeforeEach(func() {
		handler = negroni.New()
		handler.Use(handlers.NewRequestInfo())
		handler.Use(prevHandler)
		handler.Use(handlers.NewProxyWriter(logger))
		handler.Use(handlers.NewQueryParam(logger))
		handler.Use(nextHandler)
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

				Expect(string(testSink.Contents())).To(ContainSubstring(`deprecated-semicolon-params`))
				Expect(string(testSink.Contents())).To(ContainSubstring(`"data":{"vcap_request_id":"` + id + `"}`))

				Expect(resp.Header().Get(router_http.CfRouterError)).To(Equal("deprecated-semicolon-params"))
			})

			Context("when request context has trace info", func() {
				BeforeEach(func() {
					prevHandler = &PrevHandlerWithTrace{}
				})

				It("logs a warning with trace info", func() {
					req.RequestURI = "/example?param1;param2"
					handler.ServeHTTP(resp, req)

					Expect(string(testSink.Contents())).To(ContainSubstring(`"data":{"trace-id":"1111","span-id":"2222","vcap_request_id":"` + id + `"}`))

					Expect(resp.Header().Get(router_http.CfRouterError)).To(Equal("deprecated-semicolon-params"))
				})
			})
		})
		Context("when semicolons are not present", func() {
			It("does not log a warning", func() {
				req.RequestURI = "/example?param1&param2"
				handler.ServeHTTP(resp, req)

				Expect(testSink.Contents()).NotTo(ContainSubstring(`deprecated-semicolon-params`))
				Expect(resp.Header().Get(router_http.CfRouterError)).To(Equal(""))
			})
		})
	})

})
