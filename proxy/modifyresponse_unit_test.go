package proxy

import (
	"context"
	"net/http"
	"net/http/httptest"

	router_http "code.cloudfoundry.org/gorouter/common/http"
	"code.cloudfoundry.org/gorouter/handlers"
	"code.cloudfoundry.org/gorouter/logger/fakes"
	"code.cloudfoundry.org/gorouter/route"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("modifyResponse", func() {
	var (
		p       *proxy
		resp    *http.Response
		reqInfo *handlers.RequestInfo
	)
	BeforeEach(func() {
		p = &proxy{}
		rw := httptest.NewRecorder()
		rw.WriteHeader(http.StatusOK)
		resp = rw.Result()
		req, err := http.NewRequest("GET", "example.com", nil)
		Expect(err).ToNot(HaveOccurred())
		req.Header.Set(handlers.VcapRequestIdHeader, "foo-uuid")
		req.Header.Set(router_http.VcapTraceHeader, "trace-key")

		var modifiedReq *http.Request
		handlers.NewRequestInfo().ServeHTTP(nil, req, func(rw http.ResponseWriter, r *http.Request) {
			modifiedReq = r
		})
		reqInfo, err = handlers.ContextRequestInfo(modifiedReq)
		Expect(err).ToNot(HaveOccurred())
		reqInfo.RouteEndpoint = route.NewEndpoint(&route.EndpointOpts{Host: "1.2.3.4", Port: 5678})
		reqInfo.RoutePool = route.NewPool(&route.PoolOpts{
			Logger:             new(fakes.FakeLogger),
			RetryAfterFailure:  0,
			Host:               "foo.com",
			ContextPath:        "context-path",
			MaxConnsPerBackend: 0,
		})
		resp.Request = modifiedReq
	})
	Context("when Request is not attached to the response", func() {
		BeforeEach(func() {
			resp.Request = nil
		})
		It("returns an error", func() {
			err := p.modifyResponse(resp)
			Expect(err).To(HaveOccurred())
		})
	})
	Context("when RequestInfo is not attached to the request", func() {
		BeforeEach(func() {
			resp.Request = resp.Request.WithContext(context.Background())
		})
		It("returns an error", func() {
			err := p.modifyResponse(resp)
			Expect(err).To(HaveOccurred())
		})
	})
	Context("when RouteEndpoint is not attached to the request", func() {
		BeforeEach(func() {
			reqInfo.RouteEndpoint = nil
		})
		It("returns an error", func() {
			err := p.modifyResponse(resp)
			Expect(err).To(HaveOccurred())
		})
	})
	Context("when RoutePool is not attached to the request", func() {
		BeforeEach(func() {
			reqInfo.RoutePool = nil
		})
		It("returns an error", func() {
			err := p.modifyResponse(resp)
			Expect(err).To(HaveOccurred())
		})
	})
	Describe("X-Vcap-Request-Id header", func() {
		It("adds X-Vcap-Request-Id if it doesn't already exist in the response", func() {
			err := p.modifyResponse(resp)
			Expect(err).ToNot(HaveOccurred())
			Expect(resp.Header.Get(handlers.VcapRequestIdHeader)).To(Equal("foo-uuid"))
		})

		Context("when X-Vcap-Request-Id already exists in the response", func() {
			BeforeEach(func() {
				resp.Header.Set(handlers.VcapRequestIdHeader, "some-other-uuid")
			})
			It("does not add X-Vcap-Request-Id", func() {
				err := p.modifyResponse(resp)
				Expect(err).ToNot(HaveOccurred())
				Expect(resp.Header.Get(handlers.VcapRequestIdHeader)).To(Equal("some-other-uuid"))
			})
		})
	})
	Describe("Vcap Trace Headers", func() {
		It("does not add any headers when trace key is empty", func() {
			err := p.modifyResponse(resp)
			Expect(err).ToNot(HaveOccurred())
			Expect(resp.Header.Get(router_http.VcapRouterHeader)).To(BeEmpty())
			Expect(resp.Header.Get(router_http.VcapBackendHeader)).To(BeEmpty())
			Expect(resp.Header.Get(router_http.CfRouteEndpointHeader)).To(BeEmpty())
		})

		Context("when trace key is provided", func() {
			Context("when X-Vcap-Trace does not match", func() {
				BeforeEach(func() {
					p.traceKey = "other-key"
				})
				It("does not add any headers", func() {
					err := p.modifyResponse(resp)
					Expect(err).ToNot(HaveOccurred())
					Expect(resp.Header.Get(router_http.VcapRouterHeader)).To(BeEmpty())
					Expect(resp.Header.Get(router_http.VcapBackendHeader)).To(BeEmpty())
					Expect(resp.Header.Get(router_http.CfRouteEndpointHeader)).To(BeEmpty())
				})
			})
			Context("when X-Vcap-Trace does match", func() {
				BeforeEach(func() {
					p.traceKey = "trace-key"
					p.ip = "1.1.1.1"
				})
				It("adds the Vcap Trace headers", func() {
					err := p.modifyResponse(resp)
					Expect(err).ToNot(HaveOccurred())
					Expect(resp.Header.Get(router_http.VcapRouterHeader)).To(Equal("1.1.1.1"))
					Expect(resp.Header.Get(router_http.VcapBackendHeader)).To(Equal("1.2.3.4:5678"))
					Expect(resp.Header.Get(router_http.CfRouteEndpointHeader)).To(Equal("1.2.3.4:5678"))
				})
			})
		})
	})
})
