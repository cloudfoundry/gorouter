package round_tripper_test

import (
	"errors"
	"net"
	"net/http"

	"github.com/cloudfoundry/gorouter/access_log/schema"
	reqhandler "github.com/cloudfoundry/gorouter/proxy/handler"
	"github.com/cloudfoundry/gorouter/proxy/round_tripper"
	roundtripperfakes "github.com/cloudfoundry/gorouter/proxy/round_tripper/fakes"
	"github.com/cloudfoundry/gorouter/proxy/test_helpers"
	proxyfakes "github.com/cloudfoundry/gorouter/proxy/utils/fakes"
	"github.com/cloudfoundry/gorouter/route"
	routefakes "github.com/cloudfoundry/gorouter/route/fakes"
	"github.com/cloudfoundry/gorouter/route_service"
	"github.com/cloudfoundry/gorouter/test_util"
	"github.com/pivotal-golang/lager"
	"github.com/pivotal-golang/lager/lagertest"

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
			handler           reqhandler.RequestHandler
			logger            lager.Logger
			req               *http.Request
			resp              *proxyfakes.FakeProxyResponseWriter
			dialError         = &net.OpError{
				Err: errors.New("error"),
				Op:  "dial",
			}
			after round_tripper.AfterRoundTrip
		)

		BeforeEach(func() {
			endpointIterator = &routefakes.FakeEndpointIterator{}
			req = test_util.NewRequest("GET", "myapp.com", "/", nil)
			req.URL.Scheme = "http"
			resp = &proxyfakes.FakeProxyResponseWriter{}
			nullVarz := test_helpers.NullVarz{}
			nullAccessRecord := &schema.AccessLogRecord{}

			logger = lagertest.NewTestLogger("test")
			handler = reqhandler.NewRequestHandler(req, resp, nullVarz, nullAccessRecord, logger)
			transport = &roundtripperfakes.FakeRoundTripper{}

			after = func(rsp *http.Response, endpoint *route.Endpoint, err error) {
				Expect(endpoint.Tags).ShouldNot(BeNil())
			}

		})

		Context("backend", func() {
			BeforeEach(func() {
				endpoint := &route.Endpoint{
					Tags: map[string]string{},
				}

				endpointIterator.NextReturns(endpoint)

				servingBackend := true
				proxyRoundTripper = round_tripper.NewProxyRoundTripper(
					servingBackend, transport, endpointIterator, handler, after)
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
					Expect(resp.WriteHeaderCallCount()).To(Equal(1))
					Expect(resp.WriteHeaderArgsForCall(0)).To(Equal(http.StatusBadGateway))
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
				req.Header.Set(route_service.RouteServiceForwardedUrl, "http://myapp.com/")
				servingBackend := false
				proxyRoundTripper = round_tripper.NewProxyRoundTripper(
					servingBackend, transport, endpointIterator, handler, after)
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
					Expect(req.Header.Get(route_service.RouteServiceForwardedUrl)).To(Equal("http://myapp.com/"))
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
