package proxy_test

import (
	"errors"
	"net"
	"net/http"

	securefakes "github.com/cloudfoundry/gorouter/common/secure/fakes"
	"github.com/cloudfoundry/gorouter/proxy"
	proxyfakes "github.com/cloudfoundry/gorouter/proxy/fakes"
	"github.com/cloudfoundry/gorouter/route"
	routefakes "github.com/cloudfoundry/gorouter/route/fakes"
	"github.com/cloudfoundry/gorouter/test_util"
	"github.com/cloudfoundry/gosteno"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("ProxyRoundTripper", func() {
	Context("RoundTrip", func() {
		var (
			proxyRoundTripper *proxy.ProxyRoundTripper
			endpointIterator  *routefakes.FakeEndpointIterator
			transport         *proxyfakes.FakeRoundTripper
		)

		BeforeEach(func() {
			endpointIterator = &routefakes.FakeEndpointIterator{}
			logger := gosteno.NewLogger("test")
			handler := &proxy.RequestHandler{
				StenoLogger: logger,
			}
			crypto := &securefakes.FakeCrypto{}
			transport = &proxyfakes.FakeRoundTripper{}

			proxyRoundTripper = &proxy.ProxyRoundTripper{
				Iter:                endpointIterator,
				RouteServiceEnabled: true,
				Handler:             handler,
				Crypto:              crypto,
				Transport:           transport,
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

			PIt("does not set X-CF-Forwarded-Url to the route service URL", func() {
				endpoint := &route.Endpoint{
					RouteServiceUrl: "https://routeservice.net/",
				}
				endpointIterator.NextReturns(endpoint)
				req := test_util.NewRequest("GET", "myapp.com", "/", nil)
				req.URL.Scheme = "http"
				_, err := proxyRoundTripper.RoundTrip(req)
				Expect(err).NotTo(HaveOccurred())
				Expect(req.Header.Get(proxy.RouteServiceForwardedUrl)).To(Equal("http://myapp.com/"))
			})
		})
	})
})
