package round_tripper_test

import (
	"bytes"
	"crypto/tls"
	"errors"
	"io/ioutil"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"regexp"
	"syscall"
	"time"

	"code.cloudfoundry.org/gorouter/access_log/schema"
	"code.cloudfoundry.org/gorouter/handlers"
	"code.cloudfoundry.org/gorouter/metrics/fakes"
	"code.cloudfoundry.org/gorouter/proxy/handler"
	"code.cloudfoundry.org/gorouter/proxy/round_tripper"
	roundtripperfakes "code.cloudfoundry.org/gorouter/proxy/round_tripper/fakes"
	"code.cloudfoundry.org/gorouter/proxy/utils"
	"code.cloudfoundry.org/gorouter/route"
	"code.cloudfoundry.org/gorouter/test_util"
	"code.cloudfoundry.org/routing-api/models"

	router_http "code.cloudfoundry.org/gorouter/common/http"
	"code.cloudfoundry.org/gorouter/common/uuid"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gbytes"
)

type nullVarz struct{}

type testBody struct {
	bytes.Buffer
	closeCount int
}

func (t *testBody) Close() error {
	t.closeCount++
	return nil
}

var _ = Describe("ProxyRoundTripper", func() {
	Context("RoundTrip", func() {
		var (
			proxyRoundTripper round_tripper.ProxyRoundTripper
			routePool         *route.Pool
			transport         *roundtripperfakes.FakeProxyRoundTripper
			logger            *test_util.TestZapLogger
			req               *http.Request
			reqBody           *testBody
			resp              *httptest.ResponseRecorder
			alr               *schema.AccessLogRecord
			routerIP          string
			combinedReporter  *fakes.FakeCombinedReporter

			reqInfo *handlers.RequestInfo

			endpoint *route.Endpoint

			dialError = &net.OpError{
				Err: errors.New("error"),
				Op:  "dial",
			}
			connResetError = &net.OpError{
				Err: os.NewSyscallError("read", syscall.ECONNRESET),
				Op:  "read",
			}
		)

		BeforeEach(func() {
			routePool = route.NewPool(1*time.Second, "", "")
			resp = httptest.NewRecorder()
			alr = &schema.AccessLogRecord{}
			proxyWriter := utils.NewProxyResponseWriter(resp)
			reqBody = new(testBody)
			req = test_util.NewRequest("GET", "myapp.com", "/", reqBody)
			req.URL.Scheme = "http"

			handlers.NewRequestInfo().ServeHTTP(nil, req, func(_ http.ResponseWriter, transformedReq *http.Request) {
				req = transformedReq
			})

			var err error
			reqInfo, err = handlers.ContextRequestInfo(req)
			Expect(err).ToNot(HaveOccurred())

			reqInfo.RoutePool = routePool
			reqInfo.ProxyResponseWriter = proxyWriter

			logger = test_util.NewTestZapLogger("test")
			transport = new(roundtripperfakes.FakeProxyRoundTripper)
			routerIP = "127.0.0.1"

			endpoint = route.NewEndpoint("appId", "1.1.1.1", uint16(9090), "instanceId", "1",
				map[string]string{}, 0, "", models.ModificationTag{}, "", false)

			added := routePool.Put(endpoint)
			Expect(added).To(BeTrue())

			combinedReporter = new(fakes.FakeCombinedReporter)

			proxyRoundTripper = round_tripper.NewProxyRoundTripper(
				transport, logger, "my_trace_key", routerIP, "",
				combinedReporter, false,
				1234,
			)
		})

		Context("when RequestInfo is not set on the request context", func() {
			BeforeEach(func() {
				req = test_util.NewRequest("GET", "myapp.com", "/", nil)
			})
			It("returns an error", func() {
				_, err := proxyRoundTripper.RoundTrip(req)
				Expect(err.Error()).To(ContainSubstring("RequestInfo not set on context"))
			})
		})

		Context("when route pool is not set on the request context", func() {
			BeforeEach(func() {
				reqInfo.RoutePool = nil
			})
			It("returns an error", func() {
				_, err := proxyRoundTripper.RoundTrip(req)
				Expect(err.Error()).To(ContainSubstring("RoutePool not set on context"))
			})
		})

		Context("when ProxyResponseWriter is not set on the request context", func() {
			BeforeEach(func() {
				reqInfo.ProxyResponseWriter = nil
			})
			It("returns an error", func() {
				_, err := proxyRoundTripper.RoundTrip(req)
				Expect(err.Error()).To(ContainSubstring("ProxyResponseWriter not set on context"))
			})
		})

		Context("HTTP headers", func() {
			BeforeEach(func() {
				transport.RoundTripReturns(resp.Result(), nil)
			})
			It("Sends X-cf headers", func() {
				_, err := proxyRoundTripper.RoundTrip(req)
				Expect(err).ToNot(HaveOccurred())
				Expect(req.Header.Get("X-CF-ApplicationID")).To(Equal("appId"))
				Expect(req.Header.Get("X-CF-InstanceID")).To(Equal("instanceId"))
				Expect(req.Header.Get("X-CF-InstanceIndex")).To(Equal("1"))
			})

			Context("when VcapTraceHeader matches the trace key", func() {
				BeforeEach(func() {
					req.Header.Set(router_http.VcapTraceHeader, "my_trace_key")
				})

				It("sets the trace headers on the response", func() {
					backendResp, err := proxyRoundTripper.RoundTrip(req)
					Expect(err).ToNot(HaveOccurred())

					Expect(backendResp.Header.Get(router_http.VcapRouterHeader)).To(Equal(routerIP))
					Expect(backendResp.Header.Get(router_http.VcapBackendHeader)).To(Equal("1.1.1.1:9090"))
					Expect(backendResp.Header.Get(router_http.VcapBackendHeader)).To(Equal("1.1.1.1:9090"))
				})
			})

			Context("when VcapTraceHeader does not match the trace key", func() {
				BeforeEach(func() {
					req.Header.Set(router_http.VcapTraceHeader, "not_my_trace_key")
				})
				It("does not set the trace headers on the response", func() {
					backendResp, err := proxyRoundTripper.RoundTrip(req)
					Expect(err).ToNot(HaveOccurred())

					Expect(backendResp.Header.Get(router_http.VcapRouterHeader)).To(Equal(""))
					Expect(backendResp.Header.Get(router_http.VcapBackendHeader)).To(Equal(""))
					Expect(backendResp.Header.Get(router_http.VcapBackendHeader)).To(Equal(""))
				})
			})

			Context("when VcapTraceHeader is not set", func() {
				It("does not set the trace headers on the response", func() {
					backendResp, err := proxyRoundTripper.RoundTrip(req)
					Expect(err).ToNot(HaveOccurred())

					Expect(backendResp.Header.Get(router_http.VcapRouterHeader)).To(Equal(""))
					Expect(backendResp.Header.Get(router_http.VcapBackendHeader)).To(Equal(""))
					Expect(backendResp.Header.Get(router_http.VcapBackendHeader)).To(Equal(""))
				})
			})
		})

		Context("when backend is unavailable due to non-retryable error", func() {
			BeforeEach(func() {
				transport.RoundTripReturns(nil, errors.New("error"))
			})

			It("does not retry and returns status bad gateway", func() {
				_, err := proxyRoundTripper.RoundTrip(req)
				Expect(err).To(MatchError(errors.New("error")))
				Expect(transport.RoundTripCallCount()).To(Equal(1))

				Expect(resp.Code).To(Equal(http.StatusBadGateway))
				Expect(resp.Header().Get(router_http.CfRouterError)).To(Equal("endpoint_failure"))
				bodyBytes, err := ioutil.ReadAll(resp.Body)
				Expect(err).ToNot(HaveOccurred())
				Expect(string(bodyBytes)).To(ContainSubstring(round_tripper.BadGatewayMessage))
				Expect(reqInfo.RouteEndpoint).To(Equal(endpoint))
				Expect(reqInfo.StoppedAt).To(BeTemporally("~", time.Now(), 50*time.Millisecond))
			})

			It("captures each routing request to the backend", func() {
				_, err := proxyRoundTripper.RoundTrip(req)
				Expect(err).To(MatchError(errors.New("error")))

				Expect(combinedReporter.CaptureRoutingRequestCallCount()).To(Equal(1))
				Expect(combinedReporter.CaptureRoutingRequestArgsForCall(0)).To(Equal(endpoint))
			})

			It("captures bad gateway response in the metrics reporter", func() {
				_, err := proxyRoundTripper.RoundTrip(req)
				Expect(err).To(MatchError(errors.New("error")))

				Expect(combinedReporter.CaptureBadGatewayCallCount()).To(Equal(1))
			})

			It("does not log anything about route services", func() {
				_, err := proxyRoundTripper.RoundTrip(req)
				Expect(err).To(MatchError(errors.New("error")))

				Expect(logger.Buffer()).ToNot(gbytes.Say(`route-service`))
			})

			It("does not log the error or report the endpoint failure", func() {
				// TODO: Test "iter.EndpointFailed"
				_, err := proxyRoundTripper.RoundTrip(req)
				Expect(err).To(MatchError(errors.New("error")))

				Expect(logger.Buffer()).ToNot(gbytes.Say(`backend-endpoint-failed`))
			})
		})

		Context("when backend is unavailable due to dial error", func() {
			BeforeEach(func() {
				transport.RoundTripReturns(nil, dialError)
			})

			It("retries 3 times and returns status bad gateway", func() {
				_, err := proxyRoundTripper.RoundTrip(req)
				Expect(err).To(MatchError(dialError))
				Expect(transport.RoundTripCallCount()).To(Equal(3))

				Expect(resp.Code).To(Equal(http.StatusBadGateway))
				Expect(resp.Header().Get(router_http.CfRouterError)).To(Equal("endpoint_failure"))
				bodyBytes, err := ioutil.ReadAll(resp.Body)
				Expect(err).ToNot(HaveOccurred())
				Expect(string(bodyBytes)).To(ContainSubstring(round_tripper.BadGatewayMessage))
				Expect(reqInfo.RouteEndpoint).To(Equal(endpoint))
				Expect(reqInfo.StoppedAt).To(BeTemporally("~", time.Now(), 50*time.Millisecond))
			})

			It("captures each routing request to the backend", func() {
				_, err := proxyRoundTripper.RoundTrip(req)
				Expect(err).To(MatchError(dialError))

				Expect(combinedReporter.CaptureRoutingRequestCallCount()).To(Equal(3))
				for i := 0; i < 3; i++ {
					Expect(combinedReporter.CaptureRoutingRequestArgsForCall(i)).To(Equal(endpoint))
				}
			})

			It("captures bad gateway response in the metrics reporter", func() {
				_, err := proxyRoundTripper.RoundTrip(req)
				Expect(err).To(MatchError(dialError))

				Expect(combinedReporter.CaptureBadGatewayCallCount()).To(Equal(1))
			})

			It("does not log anything about route services", func() {
				_, err := proxyRoundTripper.RoundTrip(req)
				Expect(err).To(MatchError(dialError))

				Expect(logger.Buffer()).ToNot(gbytes.Say(`route-service`))
			})

			It("logs the error and reports the endpoint failure", func() {
				// TODO: Test "iter.EndpointFailed"
				_, err := proxyRoundTripper.RoundTrip(req)
				Expect(err).To(MatchError(dialError))

				// Ensures each backend-endpoint-failed message only contains one
				// route endpoint
				logContents := string(logger.Contents())
				reRegexp, err := regexp.Compile(`route-endpoint`)
				Expect(err).ToNot(HaveOccurred())
				Expect(reRegexp.FindAllString(logContents, -1)).To(HaveLen(7))

				for i := 0; i < 3; i++ {
					Expect(logger.Buffer()).To(gbytes.Say(`backend.*route-endpoint`))
					Expect(logger.Buffer()).To(gbytes.Say(`backend-endpoint-failed.*route-endpoint.*dial`))
				}
				Expect(logger.Buffer()).To(gbytes.Say(`endpoint-failed.*route-endpoint.*dial`))
			})
		})

		Context("when backend is unavailable due to connection reset error", func() {
			BeforeEach(func() {
				transport.RoundTripReturns(nil, connResetError)

				added := routePool.Put(endpoint)
				Expect(added).To(BeTrue())
			})

			It("retries 3 times", func() {
				_, err := proxyRoundTripper.RoundTrip(req)
				Expect(err).To(MatchError(connResetError))
				Expect(transport.RoundTripCallCount()).To(Equal(3))

				Expect(reqBody.closeCount).To(Equal(1))

				Expect(resp.Code).To(Equal(http.StatusBadGateway))
				Expect(resp.Header().Get(router_http.CfRouterError)).To(Equal("endpoint_failure"))
				bodyBytes, err := ioutil.ReadAll(resp.Body)
				Expect(err).ToNot(HaveOccurred())
				Expect(string(bodyBytes)).To(ContainSubstring(round_tripper.BadGatewayMessage))

				Expect(reqInfo.RouteEndpoint).To(Equal(endpoint))
				Expect(reqInfo.StoppedAt).To(BeTemporally("~", time.Now(), 50*time.Millisecond))
			})

			It("captures each routing request to the backend", func() {
				_, err := proxyRoundTripper.RoundTrip(req)
				Expect(err).To(MatchError(connResetError))

				Expect(combinedReporter.CaptureRoutingRequestCallCount()).To(Equal(3))
				for i := 0; i < 3; i++ {
					Expect(combinedReporter.CaptureRoutingRequestArgsForCall(i)).To(Equal(endpoint))
				}
			})

			It("captures bad gateway response in the metrics reporter", func() {
				_, err := proxyRoundTripper.RoundTrip(req)
				Expect(err).To(MatchError(connResetError))

				Expect(combinedReporter.CaptureBadGatewayCallCount()).To(Equal(1))
			})

			It("does not log anything about route services", func() {
				_, err := proxyRoundTripper.RoundTrip(req)
				Expect(err).To(MatchError(connResetError))

				Expect(logger.Buffer()).ToNot(gbytes.Say(`route-service`))
			})

			It("logs the error and reports the endpoint failure", func() {
				// TODO: Test "iter.EndpointFailed"
				_, err := proxyRoundTripper.RoundTrip(req)
				Expect(err).To(MatchError(connResetError))

				for i := 0; i < 3; i++ {
					Expect(logger.Buffer()).To(gbytes.Say(`backend-endpoint-failed.*connection reset`))
				}
			})
		})

		Context("when there are no more endpoints available", func() {
			BeforeEach(func() {
				removed := routePool.Remove(endpoint)
				Expect(removed).To(BeTrue())
			})

			It("returns a 502 Bad Gateway response", func() {
				backendRes, err := proxyRoundTripper.RoundTrip(req)
				Expect(err).To(HaveOccurred())
				Expect(backendRes).To(BeNil())
				Expect(err).To(Equal(handler.NoEndpointsAvailable))

				Expect(reqBody.closeCount).To(Equal(1))

				Expect(resp.Code).To(Equal(http.StatusBadGateway))
				Expect(resp.Header().Get(router_http.CfRouterError)).To(Equal("endpoint_failure"))
				bodyBytes, err := ioutil.ReadAll(resp.Body)
				Expect(err).ToNot(HaveOccurred())
				Expect(string(bodyBytes)).To(ContainSubstring(round_tripper.BadGatewayMessage))

				Expect(reqInfo.RouteEndpoint).To(BeNil())
				Expect(reqInfo.StoppedAt).To(BeTemporally("~", time.Now(), 50*time.Millisecond))
			})

			It("does not capture any routing requests to the backend", func() {
				_, err := proxyRoundTripper.RoundTrip(req)
				Expect(err).To(Equal(handler.NoEndpointsAvailable))

				Expect(combinedReporter.CaptureRoutingRequestCallCount()).To(Equal(0))
			})

			It("captures bad gateway response in the metrics reporter", func() {
				_, err := proxyRoundTripper.RoundTrip(req)
				Expect(err).To(Equal(handler.NoEndpointsAvailable))

				Expect(combinedReporter.CaptureBadGatewayCallCount()).To(Equal(1))
			})

			It("does not log anything about route services", func() {
				_, err := proxyRoundTripper.RoundTrip(req)
				Expect(err).To(Equal(handler.NoEndpointsAvailable))

				Expect(logger.Buffer()).ToNot(gbytes.Say(`route-service`))
			})

			It("does not report the endpoint failure", func() {
				// TODO: Test "iter.EndpointFailed"
				_, err := proxyRoundTripper.RoundTrip(req)
				Expect(err).To(MatchError(handler.NoEndpointsAvailable))

				Expect(logger.Buffer()).ToNot(gbytes.Say(`backend-endpoint-failed`))
			})
		})

		Context("when the first request to the backend fails", func() {
			var firstRequest bool
			BeforeEach(func() {
				firstRequest = true
				transport.RoundTripStub = func(req *http.Request) (*http.Response, error) {
					var err error
					err = nil
					if firstRequest {
						err = dialError
						firstRequest = false
					}
					return nil, err
				}
			})

			It("retries 2 times", func() {
				_, err := proxyRoundTripper.RoundTrip(req)
				Expect(err).ToNot(HaveOccurred())
				Expect(transport.RoundTripCallCount()).To(Equal(2))
				Expect(resp.Code).To(Equal(http.StatusOK))

				Expect(combinedReporter.CaptureBadGatewayCallCount()).To(Equal(0))

				Expect(reqBody.closeCount).To(Equal(1))

				Expect(reqInfo.RouteEndpoint).To(Equal(endpoint))
				Expect(reqInfo.StoppedAt).To(BeTemporally("~", time.Now(), 50*time.Millisecond))
			})

			It("logs one error and reports the endpoint failure", func() {
				// TODO: Test "iter.EndpointFailed"
				_, err := proxyRoundTripper.RoundTrip(req)
				Expect(err).ToNot(HaveOccurred())

				Expect(logger.Buffer()).To(gbytes.Say(`backend-endpoint-failed`))
				Expect(logger.Buffer()).ToNot(gbytes.Say(`backend-endpoint-failed`))
			})

			It("captures each routing request to the backend", func() {
				_, err := proxyRoundTripper.RoundTrip(req)
				Expect(err).ToNot(HaveOccurred())

				Expect(combinedReporter.CaptureRoutingRequestCallCount()).To(Equal(2))
				for i := 0; i < 2; i++ {
					Expect(combinedReporter.CaptureRoutingRequestArgsForCall(i)).To(Equal(endpoint))
				}
			})

			It("does not log anything about route services", func() {
				_, err := proxyRoundTripper.RoundTrip(req)
				Expect(err).ToNot(HaveOccurred())

				Expect(logger.Buffer()).ToNot(gbytes.Say(`route-service`))
			})
		})

		Context("when the request succeeds", func() {
			BeforeEach(func() {
				transport.RoundTripReturns(
					&http.Response{StatusCode: http.StatusTeapot}, nil,
				)
			})

			It("returns the exact response received from the backend", func() {
				resp, err := proxyRoundTripper.RoundTrip(req)
				Expect(err).ToNot(HaveOccurred())
				Expect(reqBody.closeCount).To(Equal(1))
				Expect(resp.StatusCode).To(Equal(http.StatusTeapot))
			})

			It("does not log an error or report the endpoint failure", func() {
				// TODO: Test "iter.EndpointFailed"
				_, err := proxyRoundTripper.RoundTrip(req)
				Expect(err).ToNot(HaveOccurred())

				Expect(logger.Buffer()).ToNot(gbytes.Say(`backend-endpoint-failed`))
			})

			It("does not log anything about route services", func() {
				_, err := proxyRoundTripper.RoundTrip(req)
				Expect(err).ToNot(HaveOccurred())

				Expect(logger.Buffer()).ToNot(gbytes.Say(`route-service`))
			})

		})

		Context("when backend is registered with a tls port", func() {
			BeforeEach(func() {
				var oldEndpoints []*route.Endpoint
				routePool.Each(func(endpoint *route.Endpoint) {
					oldEndpoints = append(oldEndpoints, endpoint)
				})
				for _, ep := range oldEndpoints {
					routePool.Remove(ep)
				}
				Expect(routePool.IsEmpty()).To(BeTrue())
				endpoint = route.NewEndpoint("appId", "1.1.1.1", uint16(9090), "instanceId", "1",
					map[string]string{}, 0, "", models.ModificationTag{}, "", true /* use TLS */)
				added := routePool.Put(endpoint)
				Expect(added).To(BeTrue())
				transport.RoundTripReturns(
					&http.Response{StatusCode: http.StatusTeapot}, nil,
				)
			})
			It("should set request URL scheme to https", func() {
				resp, err := proxyRoundTripper.RoundTrip(req)
				Expect(err).ToNot(HaveOccurred())
				Expect(transport.RoundTripCallCount()).To(Equal(1))
				transformedReq := transport.RoundTripArgsForCall(0)
				Expect(transformedReq.URL.Scheme).To(Equal("https"))
				Expect(reqBody.closeCount).To(Equal(1))
				Expect(resp.StatusCode).To(Equal(http.StatusTeapot))
			})
			Context("when backend is listening for non tls conn", func() {
				BeforeEach(func() {
					transport.RoundTripReturns(nil, tls.RecordHeaderError{Msg: "potato"})
				})
				It("should error with 525 status code", func() {
					_, err := proxyRoundTripper.RoundTrip(req)
					Expect(err).To(HaveOccurred())
					Expect(resp.Code).To(Equal(525))
					Expect(resp.Body).To(ContainSubstring("SSL Handshake Failed"))
				})
			})

			Context("when the backend is registered with a non-tls port", func() {
				BeforeEach(func() {
					endpoint = route.NewEndpoint("appId", "1.1.1.1", uint16(9090), "instanceId", "1",
						map[string]string{}, 0, "", models.ModificationTag{}, "", false /* do not use TLS */)

					added := routePool.Put(endpoint)
					Expect(added).To(BeTrue())
					transport.RoundTripReturns(
						&http.Response{StatusCode: http.StatusTeapot}, nil,
					)
				})
				It("should set request URL scheme to http", func() {
					resp, err := proxyRoundTripper.RoundTrip(req)
					Expect(err).ToNot(HaveOccurred())
					Expect(transport.RoundTripCallCount()).To(Equal(1))
					transformedReq := transport.RoundTripArgsForCall(0)
					Expect(transformedReq.URL.Scheme).To(Equal("http"))
					Expect(reqBody.closeCount).To(Equal(1))
					Expect(resp.StatusCode).To(Equal(http.StatusTeapot))
				})
			})
		})

		Context("when the request context contains a Route Service URL", func() {
			var routeServiceURL *url.URL
			BeforeEach(func() {
				var err error
				routeServiceURL, err = url.Parse("https://foo.com")
				Expect(err).ToNot(HaveOccurred())
				reqInfo.RouteServiceURL = routeServiceURL
				transport.RoundTripStub = func(req *http.Request) (*http.Response, error) {
					Expect(req.Host).To(Equal(routeServiceURL.Host))
					Expect(req.URL).To(Equal(routeServiceURL))
					return nil, nil
				}
			})

			It("makes requests to the route service", func() {
				_, err := proxyRoundTripper.RoundTrip(req)
				Expect(err).ToNot(HaveOccurred())
				Expect(reqBody.closeCount).To(Equal(1))
			})

			It("does not capture the routing request in metrics", func() {
				_, err := proxyRoundTripper.RoundTrip(req)
				Expect(err).ToNot(HaveOccurred())

				Expect(combinedReporter.CaptureRoutingRequestCallCount()).To(Equal(0))
			})

			Context("when the route service returns a non-2xx status code", func() {
				BeforeEach(func() {
					transport.RoundTripReturns(
						&http.Response{StatusCode: http.StatusTeapot}, nil,
					)

				})
				It("logs the response error", func() {
					_, err := proxyRoundTripper.RoundTrip(req)
					Expect(err).ToNot(HaveOccurred())

					Expect(logger.Buffer()).To(gbytes.Say(`response.*status-code":418`))
				})
			})

			Context("when the route service is an internal route service", func() {
				BeforeEach(func() {
					reqInfo.IsInternalRouteService = true
					transport.RoundTripStub = nil
					transport.RoundTripReturns(nil, nil)
				})

				It("routes the request to the configured local address", func() {
					_, err := proxyRoundTripper.RoundTrip(req)
					Expect(err).To(BeNil())

					Expect(transport.RoundTripCallCount()).To(Equal(1))
					outReq := transport.RoundTripArgsForCall(0)
					Expect(outReq.URL.Host).To(Equal("localhost:1234"))
					Expect(outReq.Host).To(Equal(routeServiceURL.Host))
				})
			})

			Context("when the route service request fails", func() {
				BeforeEach(func() {
					transport.RoundTripReturns(
						nil, dialError,
					)
				})

				It("retries 3 times and returns status bad gateway", func() {
					_, err := proxyRoundTripper.RoundTrip(req)
					Expect(err).To(MatchError(dialError))
					Expect(transport.RoundTripCallCount()).To(Equal(3))

					Expect(reqBody.closeCount).To(Equal(1))

					Expect(resp.Code).To(Equal(http.StatusBadGateway))
					Expect(resp.Header().Get(router_http.CfRouterError)).To(Equal("endpoint_failure"))

					bodyBytes, err := ioutil.ReadAll(resp.Body)
					Expect(err).ToNot(HaveOccurred())
					Expect(string(bodyBytes)).To(ContainSubstring(round_tripper.BadGatewayMessage))
				})

				It("captures bad gateway response in the metrics reporter", func() {
					_, err := proxyRoundTripper.RoundTrip(req)
					Expect(err).To(MatchError(dialError))

					Expect(combinedReporter.CaptureBadGatewayCallCount()).To(Equal(1))
				})

				It("logs the failure", func() {
					_, err := proxyRoundTripper.RoundTrip(req)
					Expect(err).To(MatchError(dialError))

					Expect(logger.Buffer()).ToNot(gbytes.Say(`backend-endpoint-failed`))
					for i := 0; i < 3; i++ {
						Expect(logger.Buffer()).To(gbytes.Say(`route-service-connection-failed.*dial`))
					}
				})

				Context("when route service is unavailable due to non-retryable error", func() {
					BeforeEach(func() {
						transport.RoundTripReturns(nil, errors.New("error"))
					})

					It("does not retry and returns status bad gateway", func() {
						_, err := proxyRoundTripper.RoundTrip(req)
						Expect(err).To(MatchError(errors.New("error")))
						Expect(transport.RoundTripCallCount()).To(Equal(1))

						Expect(reqBody.closeCount).To(Equal(1))

						Expect(resp.Code).To(Equal(http.StatusBadGateway))
						Expect(resp.Header().Get(router_http.CfRouterError)).To(Equal("endpoint_failure"))
						bodyBytes, err := ioutil.ReadAll(resp.Body)
						Expect(err).ToNot(HaveOccurred())
						Expect(string(bodyBytes)).To(ContainSubstring(round_tripper.BadGatewayMessage))
					})

					It("captures bad gateway response in the metrics reporter", func() {
						_, err := proxyRoundTripper.RoundTrip(req)
						Expect(err).To(MatchError(errors.New("error")))

						Expect(combinedReporter.CaptureBadGatewayCallCount()).To(Equal(1))
					})

					It("does not log the error or report the endpoint failure", func() {
						// TODO: Test "iter.EndpointFailed"
						_, err := proxyRoundTripper.RoundTrip(req)
						Expect(err).To(MatchError(errors.New("error")))

						Expect(logger.Buffer()).ToNot(gbytes.Say(`route-service-connection-failed`))
					})
				})
			})

		})

		Context("when sticky session", func() {
			var (
				sessionCookie *http.Cookie
				endpoint1     *route.Endpoint
				endpoint2     *route.Endpoint
			)
			BeforeEach(func() {
				sessionCookie = &http.Cookie{
					Name: round_tripper.StickyCookieKey,
				}

				transport.RoundTripStub = func(req *http.Request) (*http.Response, error) {
					resp := &http.Response{StatusCode: http.StatusTeapot, Header: make(map[string][]string)}

					if len(req.Cookies()) > 0 {
						//Only attach the JSESSIONID on to the response
						resp.Header.Add(round_tripper.CookieHeader, req.Cookies()[0].String())
						return resp, nil
					}

					sessionCookie.Value, _ = uuid.GenerateUUID()
					resp.Header.Add(round_tripper.CookieHeader, sessionCookie.String())
					return resp, nil
				}

				endpoint1 = route.NewEndpoint("appId", "1.1.1.1", uint16(9091), "id-1", "2",
					map[string]string{}, 0, "route-service.com", models.ModificationTag{}, "", false)
				endpoint2 = route.NewEndpoint("appId", "1.1.1.1", uint16(9092), "id-2", "3",
					map[string]string{}, 0, "route-service.com", models.ModificationTag{}, "", false)

				added := routePool.Put(endpoint1)
				Expect(added).To(BeTrue())
				added = routePool.Put(endpoint2)
				Expect(added).To(BeTrue())
				removed := routePool.Remove(endpoint)
				Expect(removed).To(BeTrue())
			})

			Context("and no previous session", func() {
				It("will select an endpoint and add a cookie header with the privateInstanceId", func() {
					resp, err := proxyRoundTripper.RoundTrip(req)
					Expect(err).ToNot(HaveOccurred())

					cookies := resp.Cookies()
					Expect(cookies).To(HaveLen(2))
					Expect(cookies[0].Raw).To(Equal(sessionCookie.String()))
					Expect(cookies[1].Name).To(Equal(round_tripper.VcapCookieId))
					Expect(cookies[1].Value).To(SatisfyAny(
						Equal("id-1"),
						Equal("id-2")))
				})
			})

			Context("and previous session", func() {
				var cookies []*http.Cookie
				BeforeEach(func() {
					resp, err := proxyRoundTripper.RoundTrip(req)
					Expect(err).ToNot(HaveOccurred())

					cookies = resp.Cookies()
					Expect(cookies).To(HaveLen(2))
					for _, cookie := range cookies {
						req.AddCookie(cookie)
					}
				})

				It("will select the previous backend", func() {
					resp, err := proxyRoundTripper.RoundTrip(req)
					Expect(err).ToNot(HaveOccurred())

					new_cookies := resp.Cookies()
					Expect(new_cookies).To(HaveLen(2))

					//JSESSIONID should be the same
					Expect(new_cookies[0]).To(Equal(cookies[0]))

					Expect(new_cookies[1].Value).To(Equal(cookies[1].Value))
				})

				Context("when the previous endpoints cannot be reached", func() {
					BeforeEach(func() {
						removed := routePool.Remove(endpoint1)
						Expect(removed).To(BeTrue())

						removed = routePool.Remove(endpoint2)
						Expect(removed).To(BeTrue())

						new_endpoint := route.NewEndpoint("appId", "1.1.1.1", uint16(9091), "id-5", "2",
							map[string]string{}, 0, "route-service.com", models.ModificationTag{}, "", false)
						added := routePool.Put(new_endpoint)
						Expect(added).To(BeTrue())
					})

					It("will select a new backend and update the vcap cookie id", func() {
						resp, err := proxyRoundTripper.RoundTrip(req)
						Expect(err).ToNot(HaveOccurred())

						new_cookies := resp.Cookies()
						Expect(new_cookies).To(HaveLen(2))

						//JSESSIONID should be the same
						Expect(new_cookies[0]).To(Equal(cookies[0]))

						Expect(new_cookies[1].Value).To(Equal("id-5"))
					})
				})
			})
		})

		It("can cancel requests", func() {
			proxyRoundTripper.CancelRequest(req)
			Expect(transport.CancelRequestCallCount()).To(Equal(1))
			Expect(transport.CancelRequestArgsForCall(0)).To(Equal(req))
		})
	})
})
