package proxy_test

import (
	"errors"
	"net"
	"net/http"

	"github.com/cloudfoundry/gorouter/access_log"
	securefakes "github.com/cloudfoundry/gorouter/common/secure/fakes"
	"github.com/cloudfoundry/gorouter/proxy"
	proxyfakes "github.com/cloudfoundry/gorouter/proxy/fakes"
	"github.com/cloudfoundry/gorouter/route"
	routefakes "github.com/cloudfoundry/gorouter/route/fakes"
	"github.com/cloudfoundry/gorouter/route_service"
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
			crypto            *securefakes.FakeCrypto
		)

		BeforeEach(func() {
			endpointIterator = &routefakes.FakeEndpointIterator{}
			req = test_util.NewRequest("GET", "myapp.com", "/", nil)
			req.URL.Scheme = "http"
			resp = &proxyfakes.FakeProxyResponseWriter{}
			nullVarz := nullVarz{}
			nullAccessRecord := &access_log.AccessLogRecord{}

			handler := proxy.NewRequestHandler(req, resp, nullVarz, nullAccessRecord)
			crypto = &securefakes.FakeCrypto{}
			transport = &proxyfakes.FakeRoundTripper{}

			proxyRoundTripper = &proxy.ProxyRoundTripper{
				Iter:      endpointIterator,
				Handler:   &handler,
				Transport: transport,
			}
		})

		Context("when the first request to the route service fails", func() {
			BeforeEach(func() {
				firstCall := true

				transport.RoundTripStub = func(req *http.Request) (*http.Response, error) {
					var err error

					err = nil

					if firstCall {
						err = &net.OpError{
							Err: errors.New("error"),
							Op:  "dial",
						}
					}
					firstCall = false

					return nil, err
				}
			})

			It("does not set X-CF-Forwarded-Url to the route service URL", func() {
				endpoint := &route.Endpoint{
					RouteServiceUrl: "https://routeservice.net/",
				}
				endpointIterator.NextReturns(endpoint)
				crypto.EncryptReturns([]byte("signature"), []byte{}, []byte{}, nil)
				req := test_util.NewRequest("GET", "myapp.com", "/", nil)
				req.URL.Scheme = "http"
				req.Header.Set(route_service.RouteServiceForwardedUrl, "http://myapp.com/")
				_, err := proxyRoundTripper.RoundTrip(req)
				Expect(err).NotTo(HaveOccurred())
				Expect(req.Header.Get(route_service.RouteServiceForwardedUrl)).To(Equal("http://myapp.com/"))
			})
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
