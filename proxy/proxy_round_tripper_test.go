package proxy_test

import (
	"net/http"

	"github.com/cloudfoundry/gorouter/access_log"
	securefakes "github.com/cloudfoundry/gorouter/common/secure/fakes"
	"github.com/cloudfoundry/gorouter/proxy"
	proxyfakes "github.com/cloudfoundry/gorouter/proxy/fakes"
	routefakes "github.com/cloudfoundry/gorouter/route/fakes"
	"github.com/cloudfoundry/gorouter/test_util"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("ProxyRoundTripper", func() {
	Context("RoundTrip", func() {
		var (
			proxyRoundTripper *proxy.ProxyRoundTripper
			endpointIterator  *routefakes.FakeEndpointIterator
			transport         *proxyfakes.FakeRoundTripper
			req               *http.Request
			resp              *proxyfakes.FakeProxyResponseWriter
		)

		BeforeEach(func() {
			endpointIterator = &routefakes.FakeEndpointIterator{}
			req = test_util.NewRequest("GET", "myapp.com", "/", nil)
			req.URL.Scheme = "http"
			resp = &proxyfakes.FakeProxyResponseWriter{}
			nullVarz := nullVarz{}
			nullAccessRecord := &access_log.AccessLogRecord{}

			handler := proxy.NewRequestHandler(req, resp, nullVarz, nullAccessRecord)
			crypto := &securefakes.FakeCrypto{}
			transport = &proxyfakes.FakeRoundTripper{}

			proxyRoundTripper = &proxy.ProxyRoundTripper{
				Iter:                endpointIterator,
				RouteServiceEnabled: true,
				Handler:             &handler,
				Crypto:              crypto,
				Transport:           transport,
			}
		})

		Context("when there are no more endpoints available", func() {
			It("returns a 502 BadGateway error", func() {
				endpointIterator.NextReturns(nil)
				resp.HeaderReturns(make(http.Header))
				backendRes, err := proxyRoundTripper.RoundTrip(req)
				Expect(err).To(HaveOccurred())
				Expect(backendRes).To(BeNil())
				Expect(resp.WriteHeaderCallCount()).To(Equal(1))
				Expect(resp.WriteHeaderArgsForCall(0)).To(Equal(http.StatusBadGateway))
			})
		})
	})
})
