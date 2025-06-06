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
	"go.uber.org/zap"

	"code.cloudfoundry.org/gorouter/config"
	"code.cloudfoundry.org/gorouter/handlers"
	"code.cloudfoundry.org/gorouter/route"
	"code.cloudfoundry.org/gorouter/test_util"
)

var _ = Describe("MaxRequestSize", func() {
	var (
		handler *negroni.Negroni

		resp         http.ResponseWriter
		req          *http.Request
		rawPath      string
		header       http.Header
		result       *http.Response
		responseBody []byte
		requestBody  *bytes.Buffer

		cfg    *config.Config
		logger *test_util.TestLogger
		rh     *handlers.MaxRequestSize

		nextCalled bool
	)

	nextHandler := negroni.HandlerFunc(func(rw http.ResponseWriter, req *http.Request, next http.HandlerFunc) {
		_, err := io.ReadAll(req.Body)
		Expect(err).NotTo(HaveOccurred())

		rw.WriteHeader(http.StatusTeapot)
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
			MaxRequestHeaderBytes:    89,
			MaxRequestHeaders:        15,
			LoadBalance:              config.LOAD_BALANCE_RR,
			StickySessionCookieNames: config.StringSet{"blarg": struct{}{}},
		}
		requestBody = bytes.NewBufferString("What are you?")
		rawPath = "/"
		header = http.Header{}
		resp = httptest.NewRecorder()
	})

	JustBeforeEach(func() {
		logger = test_util.NewTestLogger("test")
		handler = negroni.New()
		rh = handlers.NewMaxRequestSize(cfg, logger.Logger)
		handler.Use(rh)
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

	Context("when request size is under the limit", func() {
		It("lets the message pass", func() {
			handleRequest()
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
	Context("when request URI is over the limit", func() {
		BeforeEach(func() {
			rawPath = "/thisIsAReallyLongRequestURIThatWillExceedThirtyTwoBytes"
		})
		It("throws an http 431", func() {
			handleRequest()
			Expect(result.StatusCode).To(Equal(http.StatusRequestHeaderFieldsTooLarge))
		})
	})
	Context("when query params are over the limit", func() {
		BeforeEach(func() {
			rawPath = "/?queryParams=reallyLongValuesThatWillExceedThirtyTwoBytes"
		})
		It("throws an http 431", func() {
			handleRequest()
			Expect(result.StatusCode).To(Equal(http.StatusRequestHeaderFieldsTooLarge))
		})
	})
	Context("when a single header key is over the limit", func() {
		BeforeEach(func() {
			header.Add("thisHeaderKeyIsOverThirtyTwoBytesAndWillFail", "doesntmatter")
		})
		It("throws an http 431", func() {
			handleRequest()
			Expect(result.StatusCode).To(Equal(http.StatusRequestHeaderFieldsTooLarge))
		})
	})
	Context("when a single header value is over the limit", func() {
		BeforeEach(func() {
			header.Add("doesntmatter", "thisHeaderValueIsOverThirtyTwoBytesAndWillFail")
		})
		It("throws an http 431", func() {
			handleRequest()
			Expect(result.StatusCode).To(Equal(http.StatusRequestHeaderFieldsTooLarge))
		})
	})
	Context("when a single header has multiple small values taking it over the limit", func() {
		BeforeEach(func() {
			for i := 0; i < 10; i++ {
				header.Add("k", "meow")
			}
		})
		It("throws an http 431", func() {
			handleRequest()
			Expect(result.StatusCode).To(Equal(http.StatusRequestHeaderFieldsTooLarge))
		})
	})
	Context("when a repeated header has a short value and long key taking it over the limit", func() {
		BeforeEach(func() {
			for i := 0; i < 10; i++ {
				header.Add("foobar", "m")
			}
		})
		It("throws an http 431", func() {
			handleRequest()
			Expect(result.StatusCode).To(Equal(http.StatusRequestHeaderFieldsTooLarge))
		})
	})
	Context("when there are too many headers", func() {
		BeforeEach(func() {
			for i := 0; i < 16; i++ {
				header.Add("f", "m")
			}
		})
		It("throws an http 431", func() {
			handleRequest()
			Expect(result.StatusCode).To(Equal(http.StatusRequestHeaderFieldsTooLarge))
			Expect(result.Header).To(HaveKeyWithValue("X-Cf-Routererror", []string{"max-request-size-exceeded"}))
		})
	})
	Context("when enough normally-sized headers put the request over the limit", func() {
		BeforeEach(func() {
			header.Add("header1", "smallRequest")
			header.Add("header2", "smallRequest")
			header.Add("header3", "smallRequest")
		})
		It("throws an http 431", func() {
			handleRequest()
			Expect(result.StatusCode).To(Equal(http.StatusRequestHeaderFieldsTooLarge))
		})
	})
	Context("when any combination of things makes the request over the limit", func() {
		BeforeEach(func() {
			rawPath = "/?q=meowmeow"
			header.Add("header1", "smallRequest")
			header.Add("header2", "smallRequest")
		})
		It("throws an http 431", func() {
			handleRequest()
			Expect(result.StatusCode).To(Equal(http.StatusRequestHeaderFieldsTooLarge))
		})
		It("doesn't call the next handler", func() {
			handleRequest()
			Expect(nextCalled).To(BeFalse())
		})
		It("sets the reqInfo's RouteEndpoint, so access logs work", func() {
			handleRequest()

			reqInfo, err := handlers.ContextRequestInfo(req)
			Expect(err).NotTo(HaveOccurred())

			Expect(reqInfo.RouteEndpoint).ToNot(BeNil())
			Expect(reqInfo.RouteEndpoint.ApplicationId).To(Equal("fake-app"))
		})
	})

	Describe("NewMaxRequestSize()", func() {
		Context("when using a custom MaxHeaderBytes", func() {
			BeforeEach(func() {
				cfg.MaxRequestHeaderBytes = 1234
			})
			It("returns a new requestSizeHandler using the provided size", func() {
				Expect(rh.MaxSize).To(Equal(1234))
			})
		})

		Context("when using a negative MaxHeaderBytes", func() {
			BeforeEach(func() {
				cfg.MaxRequestHeaderBytes = -1
			})
			It("defaults to 1mb", func() {
				Expect(rh.MaxSize).To(Equal(1024 * 1024))
			})
		})
		Context("when using a zero-value MaxHeaderBytes", func() {
			BeforeEach(func() {
				cfg.MaxRequestHeaderBytes = 0
			})
			It("defaults to 1mb", func() {
				Expect(rh.MaxSize).To(Equal(1024 * 1024))
			})
		})

		Context("when using a >1mb MaxHeaderBytes", func() {
			BeforeEach(func() {
				cfg.MaxRequestHeaderBytes = handlers.ONE_MB * 2
			})
			It("defaults  to 1mb if the provided size", func() {
				Expect(rh.MaxSize).To(Equal(1024 * 1024))
			})
			It("logs a warning", func() {
				Expect(logger.Lines(zap.WarnLevel)).To(HaveLen(1))
			})
		})
	})
})
