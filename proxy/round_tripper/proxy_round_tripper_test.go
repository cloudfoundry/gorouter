package round_tripper_test

import (
	"errors"
	"net"
	"net/http"

	"code.cloudfoundry.org/gorouter/logger"
	"code.cloudfoundry.org/gorouter/proxy/handler"
	"code.cloudfoundry.org/gorouter/proxy/round_tripper"
	roundtripperfakes "code.cloudfoundry.org/gorouter/proxy/round_tripper/fakes"
	proxyfakes "code.cloudfoundry.org/gorouter/proxy/utils/fakes"
	"code.cloudfoundry.org/gorouter/route"
	routefakes "code.cloudfoundry.org/gorouter/route/fakes"
	"code.cloudfoundry.org/gorouter/routeservice"
	"code.cloudfoundry.org/gorouter/test_util"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

type nullVarz struct{}

var _ = Describe("ProxyRoundTripper", func() {
	Context("RoundTrip", func() {
		var (
			proxyRoundTripper http.RoundTripper
			endpointIterator  *routefakes.FakeEndpointIterator
			transport         *roundtripperfakes.FakeRoundTripper
			logger            logger.Logger
			req               *http.Request
			resp              *proxyfakes.FakeProxyResponseWriter
			dialError         = &net.OpError{
				Err: errors.New("error"),
				Op:  "dial",
			}
		)

		BeforeEach(func() {
			endpointIterator = &routefakes.FakeEndpointIterator{}
			req = test_util.NewRequest("GET", "myapp.com", "/", nil)
			req.URL.Scheme = "http"
			resp = &proxyfakes.FakeProxyResponseWriter{}

			logger = test_util.NewTestZapLogger("test")
			transport = &roundtripperfakes.FakeRoundTripper{}
		})

		Context("backend", func() {
			BeforeEach(func() {
				endpoint := &route.Endpoint{
					Tags: map[string]string{},
				}

				endpointIterator.NextReturns(endpoint)

				var after round_tripper.AfterRoundTrip
				servingBackend := true
				proxyRoundTripper = round_tripper.NewProxyRoundTripper(
					servingBackend, transport, endpointIterator, logger, after)
			})

			Context("when backend is unavailable", func() {
				BeforeEach(func() {
					transport.RoundTripStub = func(req *http.Request) (*http.Response, error) {
						return nil, dialError
					}
				})

				It("retries 3 times", func() {
					resp.HeaderReturns(make(http.Header))
					_, err := proxyRoundTripper.RoundTrip(req)
					Expect(err).To(HaveOccurred())
					Expect(endpointIterator.NextCallCount()).To(Equal(3))
				})
			})

			Context("when there are no more endpoints available", func() {
				BeforeEach(func() {
					endpointIterator.NextReturns(nil)
				})

				It("returns a 502 BadGateway error", func() {
					resp.HeaderReturns(make(http.Header))
					backendRes, err := proxyRoundTripper.RoundTrip(req)
					Expect(err).To(HaveOccurred())
					Expect(backendRes).To(BeNil())
					Expect(err).To(Equal(handler.NoEndpointsAvailable))
				})
			})

			Context("when the first request to the backend fails", func() {
				BeforeEach(func() {
					firstCall := true
					transport.RoundTripStub = func(req *http.Request) (*http.Response, error) {
						var err error
						err = nil
						if firstCall {
							err = dialError
							firstCall = false
						}
						return nil, err
					}
				})

				It("retries 3 times", func() {
					_, err := proxyRoundTripper.RoundTrip(req)
					Expect(err).ToNot(HaveOccurred())
					Expect(endpointIterator.NextCallCount()).To(Equal(2))
				})
			})
		})

		Context("route service", func() {
			BeforeEach(func() {
				endpoint := &route.Endpoint{
					RouteServiceUrl: "https://routeservice.net/",
					Tags:            map[string]string{},
				}
				endpointIterator.NextReturns(endpoint)
				req.Header.Set(routeservice.RouteServiceForwardedURL, "http://myapp.com/")
				servingBackend := false

				after := func(rsp *http.Response, endpoint *route.Endpoint, err error) {
					Expect(endpoint.Tags).ShouldNot(BeNil())
				}
				proxyRoundTripper = round_tripper.NewProxyRoundTripper(
					servingBackend, transport, endpointIterator, logger, after)
			})

			It("does not fetch the next endpoint", func() {
				_, err := proxyRoundTripper.RoundTrip(req)
				Expect(err).NotTo(HaveOccurred())
				Expect(endpointIterator.NextCallCount()).To(Equal(0))
			})

			Context("when the first request to the route service fails", func() {
				BeforeEach(func() {
					firstCall := true

					transport.RoundTripStub = func(req *http.Request) (*http.Response, error) {
						var err error

						err = nil
						if firstCall {
							err = dialError
						}
						firstCall = false

						return nil, err
					}
				})

				It("does not set X-CF-Forwarded-Url to the route service URL", func() {
					_, err := proxyRoundTripper.RoundTrip(req)
					Expect(err).NotTo(HaveOccurred())
					Expect(req.Header.Get(routeservice.RouteServiceForwardedURL)).To(Equal("http://myapp.com/"))
				})

			})

			Context("when the route service is not available", func() {
				var roundTripCallCount int

				BeforeEach(func() {
					transport.RoundTripStub = func(req *http.Request) (*http.Response, error) {
						roundTripCallCount++
						return nil, dialError
					}
				})

				It("retries 3 times", func() {
					_, err := proxyRoundTripper.RoundTrip(req)
					Expect(err).To(HaveOccurred())
					Expect(roundTripCallCount).To(Equal(3))
				})
			})
		})
	})
})
