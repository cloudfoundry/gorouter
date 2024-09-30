package handlers_test

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/urfave/negroni/v3"

	"code.cloudfoundry.org/gorouter/config"
	"code.cloudfoundry.org/gorouter/handlers"
	"code.cloudfoundry.org/gorouter/route"
	"code.cloudfoundry.org/gorouter/test_util"
)

var _ = Describe("HopByHop", func() {
	var (
		handler *negroni.Negroni

		resp         http.ResponseWriter
		req          *http.Request
		rawPath      string
		header       http.Header
		result       *http.Response
		responseBody []byte
		requestBody  *bytes.Buffer

		cfg      *config.Config
		logger   *test_util.TestLogger
		hopByHop *handlers.HopByHop

		nextCalled bool
	)

	nextHandler := negroni.HandlerFunc(func(rw http.ResponseWriter, req *http.Request, next http.HandlerFunc) {
		_, err := io.ReadAll(req.Body)
		Expect(err).NotTo(HaveOccurred())

		rw.WriteHeader(http.StatusTeapot)
		for name, values := range req.Header {
			for _, value := range values {
				rw.Header().Set(name, value)
			}
		}

		rw.Write([]byte("I'm a little teapot, short and stout."))

		if next != nil {
			next(rw, req)
		}

		nextCalled = true
	})

	handleRequest := func() {
		var err error
		handler.ServeHTTP(resp, req)

		result = resp.(*httptest.ResponseRecorder).Result()
		responseBody, err = io.ReadAll(result.Body)
		Expect(err).NotTo(HaveOccurred())
		result.Body.Close()
	}

	BeforeEach(func() {
		cfg = &config.Config{
			HopByHopHeadersToFilter: make([]string, 0),
			LoadBalance:             config.LOAD_BALANCE_RR,
		}
		requestBody = bytes.NewBufferString("What are you?")
		rawPath = "/"
		header = http.Header{}
		resp = httptest.NewRecorder()
	})

	JustBeforeEach(func() {
		logger = test_util.NewTestLogger("test")
		handler = negroni.New()
		hopByHop = handlers.NewHopByHop(cfg, logger.Logger)
		handler.Use(hopByHop)
		handler.Use(nextHandler)

		nextCalled = false

		var err error
		req, err = http.NewRequest("GET", "http://example.com"+rawPath, requestBody)
		Expect(err).NotTo(HaveOccurred())

		req.Header = header
		reqInfo := &handlers.RequestInfo{
			RoutePool: route.NewPool(&route.PoolOpts{}),
		}
		reqInfo.RoutePool.Put(route.NewEndpoint(&route.EndpointOpts{
			AppId:             "fake-app",
			Host:              "fake-host",
			Port:              1234,
			PrivateInstanceId: "fake-instance",
		}))
		req = req.WithContext(context.WithValue(req.Context(), handlers.RequestInfoCtxKey, reqInfo))
	})

	Context("when HopByHopHeadersToFilter is empty", func() {
		BeforeEach(func() {
			header.Add("Connection", "X-Forwarded-Proto")
		})

		It("does not touch headers listed in the Connection header", func() {
			handleRequest()
			Expect(resp.Header().Get("Connection")).To(ContainSubstring("X-Forwarded-Proto"))
			Expect(result.StatusCode).To(Equal(http.StatusTeapot))
			Expect(result.Status).To(Equal("418 I'm a teapot"))
			Expect(string(responseBody)).To(Equal("I'm a little teapot, short and stout."))

		})
		It("calls the next handler", func() {
			handleRequest()
			Expect(nextCalled).To(BeTrue())
		})
		It("doesn't set the reqInfo's RouteEndpoint", func() {
			handleRequest()
			reqInfo, err := handlers.ContextRequestInfo(req)
			Expect(err).NotTo(HaveOccurred())

			Expect(reqInfo.RouteEndpoint).To(BeNil())
		})
	})

	Context("when HopByHopHeadersToFilter is set", func() {
		BeforeEach(func() {
			cfg.HopByHopHeadersToFilter = append(cfg.HopByHopHeadersToFilter, "X-Forwarded-Proto")
			header.Add("Connection", "X-Forwarded-Proto")
		})

		It("removes the headers listed in the Connection header", func() {
			handleRequest()
			Expect(resp.Header().Get("Connection")).To(BeEmpty())
			Expect(result.StatusCode).To(Equal(http.StatusTeapot))
			Expect(result.Status).To(Equal("418 I'm a teapot"))
			Expect(string(responseBody)).To(Equal("I'm a little teapot, short and stout."))

		})
		It("calls the next handler", func() {
			handleRequest()
			Expect(nextCalled).To(BeTrue())
		})
		It("doesn't set the reqInfo's RouteEndpoint", func() {
			handleRequest()
			reqInfo, err := handlers.ContextRequestInfo(req)
			Expect(err).NotTo(HaveOccurred())

			Expect(reqInfo.RouteEndpoint).To(BeNil())
		})
	})

})
