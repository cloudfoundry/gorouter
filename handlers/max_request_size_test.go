package handlers_test

import (
	"bytes"
	"io/ioutil"
	"net/http"
	"net/http/httptest"

	"code.cloudfoundry.org/gorouter/handlers"
	logger_fakes "code.cloudfoundry.org/gorouter/logger/fakes"
	"code.cloudfoundry.org/gorouter/route"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/urfave/negroni"
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

		maxSize    int
		fakeLogger *logger_fakes.FakeLogger

		nextCalled bool
		reqChan    chan *http.Request
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

	handleRequest := func() {
		var err error
		handler.ServeHTTP(resp, req)

		result = resp.(*httptest.ResponseRecorder).Result()
		responseBody, err = ioutil.ReadAll(result.Body)
		Expect(err).NotTo(HaveOccurred())
		result.Body.Close()
	}

	BeforeEach(func() {
		requestBody = bytes.NewBufferString("What are you?")
		rawPath = "/"
		header = http.Header{}
		resp = httptest.NewRecorder()

		maxSize = 40
		fakeLogger = new(logger_fakes.FakeLogger)
	})

	JustBeforeEach(func() {
		handler = negroni.New()
		handler.Use(handlers.NewMaxRequestSize(maxSize, fakeLogger))
		handler.Use(nextHandler)

		reqChan = make(chan *http.Request, 1)

		nextCalled = false

		var err error
		req, err = http.NewRequest("GET", "http://example.com"+rawPath, requestBody)
		req.Header = header
		Expect(err).NotTo(HaveOccurred())
	})

	AfterEach(func() {
		close(reqChan)
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
			rawPath = "/?q=v"
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
	})

	Describe("NewMaxRequestSize()", func() {
		It("returns a new requestSizeHandler using the provided size", func() {
			rh := handlers.NewMaxRequestSize(1234, fakeLogger)
			Expect(rh.MaxSize).To(Equal(1234))
		})

		It("defaults to 1mb if the provided size is negative", func() {
			rh := handlers.NewMaxRequestSize(-1, fakeLogger)
			Expect(rh.MaxSize).To(Equal(1024 * 1024))
		})

		It("defaults to 1mb if the provided size is 0", func() {
			rh := handlers.NewMaxRequestSize(0, fakeLogger)
			Expect(rh.MaxSize).To(Equal(1024 * 1024))
		})

		It("defaults  to 1mb if the provided size is greater than 1mb and logs a warning", func() {
			rh := handlers.NewMaxRequestSize(2*1024*1024, fakeLogger)
			Expect(rh.MaxSize).To(Equal(1024 * 1024))
		})
		It("logs a warning if the provided size is greater than 1mb and logs a warning", func() {
			handlers.NewMaxRequestSize(2*1024*1024, fakeLogger)
			Expect(fakeLogger.WarnCallCount()).To(Equal(1))
		})
	})
})
