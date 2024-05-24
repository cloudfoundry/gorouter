package handlers_test

import (
	"net/http"
	"net/http/httptest"
	"net/http/httputil"

	"code.cloudfoundry.org/gorouter/handlers"
	"code.cloudfoundry.org/gorouter/test_util"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/urfave/negroni/v3"
)

var _ = Describe("Proxy Picker", func() {
	var (
		handler                                                 negroni.Handler
		resp                                                    *httptest.ResponseRecorder
		req                                                     *http.Request
		nextHandler                                             http.HandlerFunc
		nextCalled, rproxyCalled, expect100ContinueRProxyCalled bool
		rproxy, expect100ContinueRProxy                         *httputil.ReverseProxy
	)
	BeforeEach(func() {
		req = test_util.NewRequest("GET", "example.com", "/", nil)
		resp = httptest.NewRecorder()

		rproxy = &httputil.ReverseProxy{
			Director: func(r *http.Request) {
				rproxyCalled = true
			},
		}
		expect100ContinueRProxy = &httputil.ReverseProxy{
			Rewrite: func(t *httputil.ProxyRequest) {
				expect100ContinueRProxyCalled = true
			},
		}

		nextCalled = false
		rproxyCalled = false
		expect100ContinueRProxyCalled = false

		handler = handlers.NewProxyPicker(rproxy, expect100ContinueRProxy)
		nextHandler = http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
			nextCalled = true
		})

	})

	Context("when the request has an Expect: 100-continue", func() {
		BeforeEach(func() {
			req.Header.Set("Expect", "100-continue")
		})
		It("Chooses the expect100ContinueRProxy", func() {
			handler.ServeHTTP(resp, req, nextHandler)
			Expect(expect100ContinueRProxyCalled).To(BeTrue())
			Expect(rproxyCalled).To(BeFalse())
		})

		Context("when upper/lower case mixtures", func() {
			BeforeEach(func() {
				req.Header.Set("Expect", "100-CoNTiNuE")
			})
			It("is case insensitive", func() {
				handler.ServeHTTP(resp, req, nextHandler)
				Expect(expect100ContinueRProxyCalled).To(BeTrue())
				Expect(rproxyCalled).To(BeFalse())
			})
		})
	})
	Context("when the request does not have an Expect: 100-continue", func() {
		It("Chooses the main rproxy", func() {
			handler.ServeHTTP(resp, req, nextHandler)
			Expect(expect100ContinueRProxyCalled).To(BeFalse())
			Expect(rproxyCalled).To(BeTrue())
		})

	})
	It("calls next()", func() {
		handler.ServeHTTP(resp, req, nextHandler)
		Expect(nextCalled).To(BeTrue())
	})
})
