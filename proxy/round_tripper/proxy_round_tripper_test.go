package round_tripper_test

import (
	"bytes"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"time"

	"code.cloudfoundry.org/gorouter/routeservice"

	"code.cloudfoundry.org/gorouter/common/uuid"
	"code.cloudfoundry.org/gorouter/config"
	sharedfakes "code.cloudfoundry.org/gorouter/fakes"
	"code.cloudfoundry.org/gorouter/handlers"
	"code.cloudfoundry.org/gorouter/metrics/fakes"
	errorClassifierFakes "code.cloudfoundry.org/gorouter/proxy/fails/fakes"
	"code.cloudfoundry.org/gorouter/proxy/handler"
	"code.cloudfoundry.org/gorouter/proxy/round_tripper"
	roundtripperfakes "code.cloudfoundry.org/gorouter/proxy/round_tripper/fakes"
	"code.cloudfoundry.org/gorouter/proxy/utils"
	"code.cloudfoundry.org/gorouter/route"
	"code.cloudfoundry.org/gorouter/test_util"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gbytes"
	"github.com/uber-go/zap"
)

const StickyCookieKey = "JSESSIONID"

type testBody struct {
	bytes.Buffer
	closeCount int
}

func (t *testBody) Close() error {
	t.closeCount++
	return nil
}

type FakeRoundTripperFactory struct {
	ReturnValue                round_tripper.ProxyRoundTripper
	RequestedRoundTripperTypes []bool
}

func (f *FakeRoundTripperFactory) New(expectedServerName string, isRouteService bool) round_tripper.ProxyRoundTripper {
	f.RequestedRoundTripperTypes = append(f.RequestedRoundTripperTypes, isRouteService)
	return f.ReturnValue
}

var _ = Describe("ProxyRoundTripper", func() {
	Context("RoundTrip", func() {
		var (
			proxyRoundTripper      round_tripper.ProxyRoundTripper
			routePool              *route.EndpointPool
			transport              *roundtripperfakes.FakeProxyRoundTripper
			logger                 *test_util.TestZapLogger
			req                    *http.Request
			reqBody                *testBody
			resp                   *httptest.ResponseRecorder
			combinedReporter       *fakes.FakeCombinedReporter
			roundTripperFactory    *FakeRoundTripperFactory
			routeServicesTransport *sharedfakes.RoundTripper
			retriableClassifier    *errorClassifierFakes.Classifier
			errorHandler           *roundtripperfakes.ErrorHandler
			cfg                    *config.Config

			reqInfo *handlers.RequestInfo

			endpoint *route.Endpoint

			dialError = &net.OpError{
				Err: errors.New("error"),
				Op:  "dial",
			}
		)

		BeforeEach(func() {
			logger = test_util.NewTestZapLogger("test")
			routePool = route.NewPool(&route.PoolOpts{
				Logger:             logger,
				RetryAfterFailure:  1 * time.Second,
				Host:               "myapp.com",
				ContextPath:        "",
				MaxConnsPerBackend: 0,
			})
			resp = httptest.NewRecorder()
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

			transport = new(roundtripperfakes.FakeProxyRoundTripper)

			endpoint = route.NewEndpoint(&route.EndpointOpts{
				AppId:                "appId",
				Host:                 "1.1.1.1",
				Port:                 9090,
				PrivateInstanceId:    "instanceId",
				PrivateInstanceIndex: "1",
			})

			added := routePool.Put(endpoint)
			Expect(added).To(Equal(route.ADDED))

			combinedReporter = new(fakes.FakeCombinedReporter)

			errorHandler = &roundtripperfakes.ErrorHandler{}

			roundTripperFactory = &FakeRoundTripperFactory{ReturnValue: transport}
			retriableClassifier = &errorClassifierFakes.Classifier{}
			retriableClassifier.ClassifyReturns(false)
			routeServicesTransport = &sharedfakes.RoundTripper{}

			cfg, err = config.DefaultConfig()
			Expect(err).ToNot(HaveOccurred())
			cfg.EndpointTimeout = 0 * time.Millisecond
		})

		JustBeforeEach(func() {
			proxyRoundTripper = round_tripper.NewProxyRoundTripper(
				roundTripperFactory,
				retriableClassifier,
				logger,
				combinedReporter,
				errorHandler,
				routeServicesTransport,
				cfg,
			)
		})

		Context("RoundTrip", func() {
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

				It("sends X-cf headers", func() {
					_, err := proxyRoundTripper.RoundTrip(req)
					Expect(err).ToNot(HaveOccurred())
					Expect(transport.RoundTripCallCount()).To(Equal(1))
					outreq := transport.RoundTripArgsForCall(0)
					Expect(outreq.Header.Get("X-CF-ApplicationID")).To(Equal("appId"))
					Expect(outreq.Header.Get("X-CF-InstanceID")).To(Equal("instanceId"))
					Expect(outreq.Header.Get("X-CF-InstanceIndex")).To(Equal("1"))
				})
			})

			Context("when some backends fail", func() {
				BeforeEach(func() {
					transport.RoundTripStub = func(*http.Request) (*http.Response, error) {
						switch transport.RoundTripCallCount() {
						case 1:
							return nil, &net.OpError{Op: "dial", Err: errors.New("something")}
						case 2:
							return nil, &net.OpError{Op: "dial", Err: errors.New("something")}
						case 3:
							return &http.Response{StatusCode: http.StatusTeapot}, nil
						default:
							return nil, nil
						}
					}

					retriableClassifier.ClassifyReturns(true)
				})

				It("retries until success", func() {
					res, err := proxyRoundTripper.RoundTrip(req)
					Expect(err).NotTo(HaveOccurred())
					Expect(transport.RoundTripCallCount()).To(Equal(3))
					Expect(retriableClassifier.ClassifyCallCount()).To(Equal(2))

					Expect(reqInfo.RouteEndpoint).To(Equal(endpoint))
					Expect(reqInfo.StoppedAt).To(BeTemporally("~", time.Now(), 50*time.Millisecond))
					Expect(res.StatusCode).To(Equal(http.StatusTeapot))
				})

				It("captures each routing request to the backend", func() {
					_, err := proxyRoundTripper.RoundTrip(req)
					Expect(err).NotTo(HaveOccurred())

					Expect(combinedReporter.CaptureRoutingRequestCallCount()).To(Equal(3))
					for i := 0; i < 3; i++ {
						Expect(combinedReporter.CaptureRoutingRequestArgsForCall(i)).To(Equal(endpoint))
					}
				})

				It("logs the error and removes offending backend", func() {
					for i := 0; i < 2; i++ {
						endpoint = route.NewEndpoint(&route.EndpointOpts{
							AppId:                fmt.Sprintf("appID%d", i),
							Host:                 fmt.Sprintf("%d, %d, %d, %d", i, i, i, i),
							Port:                 9090,
							PrivateInstanceId:    fmt.Sprintf("instanceID%d", i),
							PrivateInstanceIndex: fmt.Sprintf("%d", i),
						})

						Expect(routePool.Put(endpoint)).To(Equal(route.ADDED))
					}

					res, err := proxyRoundTripper.RoundTrip(req)
					Expect(err).NotTo(HaveOccurred())

					iter := routePool.Endpoints("", "")
					ep1 := iter.Next()
					ep2 := iter.Next()
					Expect(ep1.PrivateInstanceId).To(Equal(ep2.PrivateInstanceId))

					errorLogs := logger.Lines(zap.ErrorLevel)
					Expect(len(errorLogs)).To(BeNumerically(">=", 2))
					count := 0
					for i := 0; i < len(errorLogs); i++ {
						if strings.Contains(errorLogs[i], "backend-endpoint-failed") {
							count++
						}
					}
					Expect(count).To(Equal(2))
					Expect(res.StatusCode).To(Equal(http.StatusTeapot))
				})

				It("logs the attempt number", func() {
					res, err := proxyRoundTripper.RoundTrip(req)
					Expect(err).NotTo(HaveOccurred())
					Expect(res.StatusCode).To(Equal(http.StatusTeapot))

					errorLogs := logger.Lines(zap.ErrorLevel)
					Expect(len(errorLogs)).To(BeNumerically(">=", 3))
					count := 0
					for i := 0; i < len(errorLogs); i++ {
						if strings.Contains(errorLogs[i], fmt.Sprintf("\"attempt\":%d", count+1)) {
							count++
						}
					}
					Expect(count).To(Equal(2))
				})

				It("does not call the error handler", func() {
					_, err := proxyRoundTripper.RoundTrip(req)
					Expect(err).NotTo(HaveOccurred())
					Expect(errorHandler.HandleErrorCallCount()).To(Equal(0))
				})

				It("does not log anything about route services", func() {
					_, err := proxyRoundTripper.RoundTrip(req)
					Expect(err).NotTo(HaveOccurred())
				})
			})

			Context("when backend is unavailable due to non-retriable error", func() {
				BeforeEach(func() {
					badResponse := &http.Response{
						Header: make(map[string][]string),
					}
					badResponse.Header.Add(handlers.VcapRequestIdHeader, "some-request-id")
					transport.RoundTripReturns(badResponse, &net.OpError{Op: "remote error", Err: errors.New("tls: handshake failure")})
					retriableClassifier.ClassifyReturns(false)
				})

				It("does not retry", func() {
					_, err := proxyRoundTripper.RoundTrip(req)
					Expect(err).To(MatchError(ContainSubstring("tls: handshake failure")))
					Expect(transport.RoundTripCallCount()).To(Equal(1))

					Expect(reqInfo.RouteEndpoint).To(Equal(endpoint))
					Expect(reqInfo.StoppedAt).To(BeTemporally("~", time.Now(), 50*time.Millisecond))
				})

				It("captures each routing request to the backend", func() {
					_, err := proxyRoundTripper.RoundTrip(req)
					Expect(err).To(MatchError(ContainSubstring("tls: handshake failure")))

					Expect(combinedReporter.CaptureRoutingRequestCallCount()).To(Equal(1))
					Expect(combinedReporter.CaptureRoutingRequestArgsForCall(0)).To(Equal(endpoint))
				})

				It("calls the error handler", func() {
					_, err := proxyRoundTripper.RoundTrip(req)
					Expect(err).To(HaveOccurred())
					Expect(errorHandler.HandleErrorCallCount()).To(Equal(1))
					_, err = errorHandler.HandleErrorArgsForCall(0)
					Expect(err).To(MatchError(ContainSubstring("tls: handshake failure")))
				})

				It("does not log anything about route services", func() {
					_, err := proxyRoundTripper.RoundTrip(req)
					Expect(err).To(MatchError(ContainSubstring("tls: handshake failure")))

					Expect(logger.Buffer()).ToNot(gbytes.Say(`route-service`))
				})

				It("does log the error and reports the endpoint failure", func() {
					endpoint = route.NewEndpoint(&route.EndpointOpts{
						AppId:                "appId2",
						Host:                 "2.2.2.2",
						Port:                 8080,
						PrivateInstanceId:    "instanceId2",
						PrivateInstanceIndex: "2",
					})

					added := routePool.Put(endpoint)
					Expect(added).To(Equal(route.ADDED))

					_, err := proxyRoundTripper.RoundTrip(req)
					Expect(err).To(MatchError(ContainSubstring("tls: handshake failure")))

					iter := routePool.Endpoints("", "")
					ep1 := iter.Next()
					ep2 := iter.Next()
					Expect(ep1).To(Equal(ep2))

					logOutput := logger.Buffer()
					Expect(logOutput).To(gbytes.Say(`backend-endpoint-failed`))
					Expect(logOutput).To(gbytes.Say(`vcap_request_id`))
				})
			})

			Context("when there are no more endpoints available", func() {
				BeforeEach(func() {
					removed := routePool.Remove(endpoint)
					Expect(removed).To(BeTrue())
				})

				It("returns a 502 Bad Gateway response", func() {
					backendRes, err := proxyRoundTripper.RoundTrip(req)
					Expect(backendRes).To(BeNil())
					Expect(err).To(Equal(handler.NoEndpointsAvailable))

					Expect(reqInfo.RouteEndpoint).To(BeNil())
					Expect(reqInfo.StoppedAt).To(BeTemporally("~", time.Now(), 50*time.Millisecond))
				})

				It("calls the error handler", func() {
					proxyRoundTripper.RoundTrip(req)
					Expect(errorHandler.HandleErrorCallCount()).To(Equal(1))
					_, err := errorHandler.HandleErrorArgsForCall(0)
					Expect(err).To(Equal(handler.NoEndpointsAvailable))
				})

				It("logs a message with `select-endpoint-failed`", func() {
					proxyRoundTripper.RoundTrip(req)
					logOutput := logger.Buffer()
					Expect(logOutput).To(gbytes.Say(`select-endpoint-failed`))
					Expect(logOutput).To(gbytes.Say(`myapp.com`))
				})

				It("does not capture any routing requests to the backend", func() {
					_, err := proxyRoundTripper.RoundTrip(req)
					Expect(err).To(Equal(handler.NoEndpointsAvailable))

					Expect(combinedReporter.CaptureRoutingRequestCallCount()).To(Equal(0))
				})

				It("does not log anything about route services", func() {
					_, err := proxyRoundTripper.RoundTrip(req)
					Expect(err).To(Equal(handler.NoEndpointsAvailable))

					Expect(logger.Buffer()).ToNot(gbytes.Say(`route-service`))
				})

				It("does not report the endpoint failure", func() {
					_, err := proxyRoundTripper.RoundTrip(req)
					Expect(err).To(MatchError(handler.NoEndpointsAvailable))

					Expect(logger.Buffer()).ToNot(gbytes.Say(`backend-endpoint-failed`))
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
					Expect(resp.StatusCode).To(Equal(http.StatusTeapot))
				})

				It("does not log an error or report the endpoint failure", func() {
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

			Context("when there are a mixture of tls and non-tls backends", func() {
				BeforeEach(func() {
					tlsEndpoint := route.NewEndpoint(&route.EndpointOpts{
						Host:   "2.2.2.2",
						Port:   20222,
						UseTLS: true,
					})
					Expect(routePool.Put(tlsEndpoint)).To(Equal(route.ADDED))

					nonTLSEndpoint := route.NewEndpoint(&route.EndpointOpts{
						Host:   "3.3.3.3",
						Port:   30333,
						UseTLS: false,
					})
					Expect(routePool.Put(nonTLSEndpoint)).To(Equal(route.ADDED))
				})

				Context("when retrying different backends", func() {
					var (
						recordedRequests map[string]string
						mutex            sync.Mutex
					)

					BeforeEach(func() {
						recordedRequests = map[string]string{}
						transport.RoundTripStub = func(r *http.Request) (*http.Response, error) {
							mutex.Lock()
							defer mutex.Unlock()
							recordedRequests[r.URL.Host] = r.URL.Scheme
							return nil, errors.New("potato")
						}
						retriableClassifier.ClassifyReturns(true)
					})

					It("uses the correct url scheme (protocol) for each backend", func() {
						_, err := proxyRoundTripper.RoundTrip(req)
						Expect(err).To(HaveOccurred())
						Expect(transport.RoundTripCallCount()).To(Equal(3))
						Expect(retriableClassifier.ClassifyCallCount()).To(Equal(3))

						Expect(recordedRequests).To(Equal(map[string]string{
							"1.1.1.1:9090":  "http",
							"2.2.2.2:20222": "https",
							"3.3.3.3:30333": "http",
						}))
					})
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
					endpoint = route.NewEndpoint(&route.EndpointOpts{
						Host:   "1.1.1.1",
						Port:   9090,
						UseTLS: true,
					})

					added := routePool.Put(endpoint)
					Expect(added).To(Equal(route.ADDED))
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
					Expect(resp.StatusCode).To(Equal(http.StatusTeapot))
				})

				Context("when the backend is registered with a non-tls port", func() {
					BeforeEach(func() {
						endpoint = route.NewEndpoint(&route.EndpointOpts{
							Host: "1.1.1.1",
							Port: 9090,
						})

						added := routePool.Put(endpoint)
						Expect(added).To(Equal(route.UPDATED))
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
						Expect(resp.StatusCode).To(Equal(http.StatusTeapot))
					})
				})
			})

			Context("transport re-use", func() {
				It("re-uses transports for the same endpoint", func() {
					_, err := proxyRoundTripper.RoundTrip(req)
					Expect(err).ToNot(HaveOccurred())
					Expect(roundTripperFactory.RequestedRoundTripperTypes).To(Equal([]bool{false}))

					_, err = proxyRoundTripper.RoundTrip(req)
					Expect(err).ToNot(HaveOccurred())
					Expect(roundTripperFactory.RequestedRoundTripperTypes).To(Equal([]bool{false}))
				})

				It("does not re-use transports between endpoints", func() {
					endpoint = route.NewEndpoint(&route.EndpointOpts{
						Host: "1.1.1.1", Port: 9091, UseTLS: true, PrivateInstanceId: "instanceId-2",
					})
					added := routePool.Put(endpoint)
					Expect(added).To(Equal(route.ADDED))

					_, err := proxyRoundTripper.RoundTrip(req)
					Expect(err).ToNot(HaveOccurred())
					Expect(roundTripperFactory.RequestedRoundTripperTypes).To(Equal([]bool{false}))

					_, err = proxyRoundTripper.RoundTrip(req)
					Expect(err).ToNot(HaveOccurred())
					Expect(roundTripperFactory.RequestedRoundTripperTypes).To(Equal([]bool{false, false}))

					_, err = proxyRoundTripper.RoundTrip(req)
					Expect(err).ToNot(HaveOccurred())
					Expect(roundTripperFactory.RequestedRoundTripperTypes).To(Equal([]bool{false, false}))
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
						reqInfo.ShouldRouteToInternalRouteService = true
						transport.RoundTripStub = nil
						transport.RoundTripReturns(nil, nil)
					})

					It("uses the route services round tripper to make the request", func() {
						_, err := proxyRoundTripper.RoundTrip(req)
						Expect(err).To(BeNil())
						Expect(transport.RoundTripCallCount()).To(Equal(0))
						Expect(routeServicesTransport.RoundTripCallCount()).To(Equal(1))

						outReq := routeServicesTransport.RoundTripArgsForCall(0)
						Expect(outReq.Host).To(Equal(routeServiceURL.Host))
					})
				})

				Context("when the route service request fails", func() {
					BeforeEach(func() {
						transport.RoundTripReturns(
							nil, dialError,
						)
						retriableClassifier.ClassifyReturns(true)
					})

					It("calls the error handler", func() {
						proxyRoundTripper.RoundTrip(req)
						Expect(errorHandler.HandleErrorCallCount()).To(Equal(1))

						_, err := errorHandler.HandleErrorArgsForCall(0)
						Expect(err).To(Equal(dialError))
					})

					It("logs the failure", func() {
						_, err := proxyRoundTripper.RoundTrip(req)
						Expect(err).To(MatchError(dialError))

						Expect(logger.Buffer()).ToNot(gbytes.Say(`backend-endpoint-failed`))
						for i := 0; i < 3; i++ {
							Expect(logger.Buffer()).To(gbytes.Say(`route-service-connection-failed`))
						}
					})

					Context("when route service is unavailable due to non-retriable error", func() {
						BeforeEach(func() {
							transport.RoundTripReturns(nil, errors.New("banana"))
							retriableClassifier.ClassifyReturns(false)
						})

						It("does not retry and returns status bad gateway", func() {
							_, err := proxyRoundTripper.RoundTrip(req)
							Expect(err).To(MatchError(errors.New("banana")))
							Expect(transport.RoundTripCallCount()).To(Equal(1))
						})

						It("calls the error handler", func() {
							proxyRoundTripper.RoundTrip(req)
							Expect(errorHandler.HandleErrorCallCount()).To(Equal(1))
							_, err := errorHandler.HandleErrorArgsForCall(0)
							Expect(err).To(MatchError("banana"))
						})

						It("logs the error", func() {
							_, err := proxyRoundTripper.RoundTrip(req)
							Expect(err).To(MatchError("banana"))

							Expect(logger.Buffer()).To(gbytes.Say(`route-service-connection-failed`))
						})
					})
				})
			})

			Context("when using sticky sessions", func() {
				var (
					sessionCookie *http.Cookie
					endpoint1     *route.Endpoint
					endpoint2     *route.Endpoint

					// options for transport.RoundTripStub
					responseContainsNoCookies                     func(req *http.Request) (*http.Response, error)
					responseContainsJSESSIONID                    func(req *http.Request) (*http.Response, error)
					responseContainsJSESSIONIDWithExtraProperties func(req *http.Request) (*http.Response, error)
					responseContainsVCAPID                        func(req *http.Request) (*http.Response, error)
					responseContainsJSESSIONIDAndVCAPID           func(req *http.Request) (*http.Response, error)
				)

				setJSESSIONID := func(req *http.Request, resp *http.Response, setExtraProperties bool) (response *http.Response) {

					//Attach the same JSESSIONID on to the response if it exists on the request
					if len(req.Cookies()) > 0 && !setExtraProperties {
						resp.Header.Add(round_tripper.CookieHeader, req.Cookies()[0].String())
						return resp
					}

					if setExtraProperties {
						sessionCookie.SameSite = http.SameSiteStrictMode
						sessionCookie.Expires = time.Date(2020, 1, 1, 1, 0, 0, 0, time.UTC)
						sessionCookie.Secure = true
						sessionCookie.HttpOnly = true
					}

					sessionCookie.Value, _ = uuid.GenerateUUID()
					resp.Header.Add(round_tripper.CookieHeader, sessionCookie.String())
					return resp
				}

				setVCAPID := func(req *http.Request, resp *http.Response) (response *http.Response) {
					vcapCookie := http.Cookie{
						Name:  round_tripper.VcapCookieId,
						Value: "vcap-id-property-already-on-the-response",
					}

					if c := vcapCookie.String(); c != "" {
						resp.Header.Add(round_tripper.CookieHeader, c)
					}

					return resp
				}

				responseContainsNoCookies = func(req *http.Request) (*http.Response, error) {
					resp := &http.Response{StatusCode: http.StatusTeapot, Header: make(map[string][]string)}
					return resp, nil
				}

				responseContainsJSESSIONID = func(req *http.Request) (*http.Response, error) {
					resp := &http.Response{StatusCode: http.StatusTeapot, Header: make(map[string][]string)}
					setJSESSIONID(req, resp, false)
					return resp, nil
				}

				responseContainsVCAPID = func(req *http.Request) (*http.Response, error) {
					resp := &http.Response{StatusCode: http.StatusTeapot, Header: make(map[string][]string)}
					setVCAPID(req, resp)
					return resp, nil
				}
				responseContainsJSESSIONIDAndVCAPID = func(req *http.Request) (*http.Response, error) {
					resp := &http.Response{StatusCode: http.StatusTeapot, Header: make(map[string][]string)}
					setJSESSIONID(req, resp, false)
					setVCAPID(req, resp)
					return resp, nil
				}
				responseContainsJSESSIONIDWithExtraProperties = func(req *http.Request) (*http.Response, error) {
					resp := &http.Response{StatusCode: http.StatusTeapot, Header: make(map[string][]string)}
					setJSESSIONID(req, resp, true)
					return resp, nil
				}

				BeforeEach(func() {
					sessionCookie = &http.Cookie{
						Name: StickyCookieKey, //JSESSIONID
					}

					endpoint1 = route.NewEndpoint(&route.EndpointOpts{
						Host: "1.1.1.1", Port: 9091, PrivateInstanceId: "id-1",
					})
					endpoint2 = route.NewEndpoint(&route.EndpointOpts{
						Host: "1.1.1.1", Port: 9092, PrivateInstanceId: "id-2",
					})

					added := routePool.Put(endpoint1)
					Expect(added).To(Equal(route.ADDED))
					added = routePool.Put(endpoint2)
					Expect(added).To(Equal(route.ADDED))
					removed := routePool.Remove(endpoint)
					Expect(removed).To(BeTrue())

				})

				Context("when there are no cookies on the request", func() {
					Context("when there is a JSESSIONID set on the response", func() {
						BeforeEach(func() {
							transport.RoundTripStub = responseContainsJSESSIONID
						})

						It("will select an endpoint and set the VCAP_ID to the privateInstanceId", func() {
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
						Context("when the JSESSIONID cookie has properties set,", func() {
							BeforeEach(func() {
								transport.RoundTripStub = responseContainsJSESSIONIDWithExtraProperties
							})

							It("sets the same properties on the VCAP_ID", func() {
								resp, err := proxyRoundTripper.RoundTrip(req)
								Expect(err).ToNot(HaveOccurred())

								cookies := resp.Cookies()
								Expect(cookies).To(HaveLen(2))
								Expect(cookies[0].Raw).To(Equal(sessionCookie.String()))
								Expect(sessionCookie.String()).To(ContainSubstring("Expires=Wed, 01 Jan 2020 01:00:00 GMT; HttpOnly; Secure; SameSite=Strict"))

								Expect(cookies[1].Name).To(Equal(round_tripper.VcapCookieId))
								Expect(cookies[1].Value).To(SatisfyAny(
									Equal("id-1"),
									Equal("id-2")))
								Expect(cookies[1].Raw).To(ContainSubstring("Expires=Wed, 01 Jan 2020 01:00:00 GMT; HttpOnly; Secure; SameSite=Strict"))
							})
						})
					})
					Context("when there is a JSESSION_ID and a VCAP_ID on the response", func() {
						BeforeEach(func() {
							transport.RoundTripStub = responseContainsJSESSIONIDAndVCAPID
						})

						It("leaves the VCAP_ID alone and does not overwrite it", func() {
							resp, err := proxyRoundTripper.RoundTrip(req)
							Expect(err).ToNot(HaveOccurred())

							cookies := resp.Cookies()
							Expect(cookies).To(HaveLen(2))
							Expect(cookies[0].Raw).To(Equal(sessionCookie.String()))
							Expect(cookies[1].Name).To(Equal(round_tripper.VcapCookieId))
							Expect(cookies[1].Value).To(Equal("vcap-id-property-already-on-the-response"))
						})
					})
					Context("when there is only a VCAP_ID set on the response", func() {
						BeforeEach(func() {
							transport.RoundTripStub = responseContainsVCAPID
						})

						It("leaves the VCAP_ID alone and does not overwrite it", func() {
							resp, err := proxyRoundTripper.RoundTrip(req)
							Expect(err).ToNot(HaveOccurred())

							cookies := resp.Cookies()
							Expect(cookies).To(HaveLen(1))
							Expect(cookies[0].Name).To(Equal(round_tripper.VcapCookieId))
							Expect(cookies[0].Value).To(Equal("vcap-id-property-already-on-the-response"))
						})
					})
				})

				Context("when sticky session cookies (JSESSIONID and VCAP_ID) are on the request", func() {
					var cookies []*http.Cookie
					JustBeforeEach(func() {
						transport.RoundTripStub = responseContainsJSESSIONID
						resp, err := proxyRoundTripper.RoundTrip(req)
						Expect(err).ToNot(HaveOccurred())

						cookies = resp.Cookies()
						Expect(cookies).To(HaveLen(2))
						for _, cookie := range cookies {
							req.AddCookie(cookie)
						}
					})

					Context("when there is a JSESSIONID set on the response", func() {
						JustBeforeEach(func() {
							transport.RoundTripStub = responseContainsJSESSIONID
						})

						It("will select the previous backend and VCAP_ID is set on the response", func() {
							resp, err := proxyRoundTripper.RoundTrip(req)
							Expect(err).ToNot(HaveOccurred())

							new_cookies := resp.Cookies()
							Expect(new_cookies).To(HaveLen(2))
							Expect(new_cookies[0]).To(Equal(cookies[0]))
							Expect(new_cookies[0].Raw).To(Equal(sessionCookie.String()))
							Expect(new_cookies[1].Value).To(Equal(cookies[1].Value))
							Expect(new_cookies[1].Name).To(Equal(round_tripper.VcapCookieId))
						})

						Context("when the JSESSIONID on the response has new properties", func() {
							JustBeforeEach(func() {
								transport.RoundTripStub = responseContainsJSESSIONIDWithExtraProperties
							})

							It("the VCAP_ID on the response is set with the same new values", func() {
								resp, err := proxyRoundTripper.RoundTrip(req)
								Expect(err).ToNot(HaveOccurred())

								newCookies := resp.Cookies()
								Expect(newCookies).To(HaveLen(2))
								Expect(newCookies[0].Raw).To(Equal(sessionCookie.String()))
								Expect(newCookies[1].Name).To(Equal(round_tripper.VcapCookieId))
								Expect(newCookies[1].Value).To(Equal(cookies[1].Value)) // still pointing to the same app
								Expect(sessionCookie.String()).To(ContainSubstring("Expires=Wed, 01 Jan 2020 01:00:00 GMT; HttpOnly; Secure; SameSite=Strict"))
								Expect(newCookies[1].Raw).To(ContainSubstring("Expires=Wed, 01 Jan 2020 01:00:00 GMT; HttpOnly; Secure; SameSite=Strict"))
							})
						})

						Context("when the VCAP_ID on the request doesn't match the instance id of the chosen backend", func() {
							// This happens when the requested VCAP_ID does not exist or errored.
							// This can also happen with route services

							JustBeforeEach(func() {
								removed := routePool.Remove(endpoint1)
								Expect(removed).To(BeTrue())

								removed = routePool.Remove(endpoint2)
								Expect(removed).To(BeTrue())

								new_endpoint := route.NewEndpoint(&route.EndpointOpts{PrivateInstanceId: "id-5"})
								added := routePool.Put(new_endpoint)
								Expect(added).To(Equal(route.ADDED))
							})

							Context("when route service headers are not on the request", func() {
								It("will select a new backend and update the VCAP_ID", func() {
									resp, err := proxyRoundTripper.RoundTrip(req)
									Expect(err).ToNot(HaveOccurred())

									newCookies := resp.Cookies()
									Expect(newCookies).To(HaveLen(2))
									Expect(newCookies[0].Raw).To(Equal(sessionCookie.String()))
									Expect(newCookies[1].Name).To(Equal(round_tripper.VcapCookieId))
									Expect(newCookies[1].Value).To(Equal("id-5"))
								})
							})

							Context("when route service headers are on the request", func() {
								// This case explicitly disallows sticky sessions to route services
								JustBeforeEach(func() {
									req.Header.Set(routeservice.HeaderKeySignature, "foo")
									req.Header.Set(routeservice.HeaderKeyForwardedURL, "bar")
								})

								It("it will not set VCAP_ID", func() {
									resp, err := proxyRoundTripper.RoundTrip(req)
									Expect(err).ToNot(HaveOccurred())

									newCookies := resp.Cookies()
									Expect(newCookies).To(HaveLen(1))
									Expect(newCookies[0].Raw).To(Equal(sessionCookie.String()))
								})
							})
						})

					})

					Context("when no cookies are set on the response", func() {
						JustBeforeEach(func() {
							transport.RoundTripStub = responseContainsNoCookies
						})

						It("no cookies are set on the response", func() {
							transport.RoundTripStub = responseContainsNoCookies
							resp, err := proxyRoundTripper.RoundTrip(req)
							Expect(err).ToNot(HaveOccurred())

							new_cookies := resp.Cookies()
							Expect(new_cookies).To(HaveLen(0))
						})

						Context("when the VCAP_ID on the request doesn't match the instance id of the chosen backend", func() {
							// This happens when the requested VCAP_ID does not exist or errored.
							// This can also happen with route services

							JustBeforeEach(func() {
								removed := routePool.Remove(endpoint1)
								Expect(removed).To(BeTrue())

								removed = routePool.Remove(endpoint2)
								Expect(removed).To(BeTrue())

								new_endpoint := route.NewEndpoint(&route.EndpointOpts{PrivateInstanceId: "id-5"})
								added := routePool.Put(new_endpoint)
								Expect(added).To(Equal(route.ADDED))
							})

							Context("when route service headers are not on the request", func() {
								It("will select a new backend and update the VCAP_ID", func() {
									resp, err := proxyRoundTripper.RoundTrip(req)
									Expect(err).ToNot(HaveOccurred())

									newCookies := resp.Cookies()
									Expect(newCookies).To(HaveLen(1))
									Expect(newCookies[0].Name).To(Equal(round_tripper.VcapCookieId))
									Expect(newCookies[0].Value).To(Equal("id-5"))
								})
							})

							Context("when route service headers are on the request", func() {
								JustBeforeEach(func() {
									req.Header.Set(routeservice.HeaderKeySignature, "foo")
									req.Header.Set(routeservice.HeaderKeyForwardedURL, "bar")
								})

								It("it will not set VCAP_ID", func() {
									resp, err := proxyRoundTripper.RoundTrip(req)
									Expect(err).ToNot(HaveOccurred())

									newCookies := resp.Cookies()
									Expect(newCookies).To(HaveLen(0))
								})
							})
						})
					})

					Context("when there is a VCAP_ID set on the response", func() {
						JustBeforeEach(func() {
							transport.RoundTripStub = responseContainsVCAPID
						})

						It("leaves it alone and does not overwrite it", func() {
							transport.RoundTripStub = responseContainsVCAPID
							resp, err := proxyRoundTripper.RoundTrip(req)
							Expect(err).ToNot(HaveOccurred())

							newCookies := resp.Cookies()
							Expect(newCookies).To(HaveLen(1))

							Expect(newCookies[0].Name).To(Equal(round_tripper.VcapCookieId))
							Expect(newCookies[0].Value).To(Equal("vcap-id-property-already-on-the-response"))
						})

						Context("when the VCAP_ID on the request doesn't match the instance id of the chosen backend", func() {
							// This happens when the requested VCAP_ID does not exist or errored.
							// This can also happen with route services

							JustBeforeEach(func() {
								removed := routePool.Remove(endpoint1)
								Expect(removed).To(BeTrue())

								removed = routePool.Remove(endpoint2)
								Expect(removed).To(BeTrue())

								new_endpoint := route.NewEndpoint(&route.EndpointOpts{PrivateInstanceId: "id-5"})
								added := routePool.Put(new_endpoint)
								Expect(added).To(Equal(route.ADDED))
							})

							Context("when route service headers are not on the request", func() {
								JustBeforeEach(func() {
									transport.RoundTripStub = responseContainsVCAPID
								})

								It("leaves it alone and does not overwrite it", func() {
									transport.RoundTripStub = responseContainsVCAPID
									resp, err := proxyRoundTripper.RoundTrip(req)
									Expect(err).ToNot(HaveOccurred())
									newCookies := resp.Cookies()
									Expect(newCookies).To(HaveLen(1))

									Expect(newCookies[0].Name).To(Equal(round_tripper.VcapCookieId))
									Expect(newCookies[0].Value).To(Equal("vcap-id-property-already-on-the-response"))
								})
							})

							Context("when route service headers are on the request", func() {
								JustBeforeEach(func() {
									req.Header.Set(routeservice.HeaderKeySignature, "foo")
									req.Header.Set(routeservice.HeaderKeyForwardedURL, "bar")
									transport.RoundTripStub = responseContainsVCAPID
								})

								It("leaves it alone and does not overwrite it", func() {
									transport.RoundTripStub = responseContainsVCAPID
									resp, err := proxyRoundTripper.RoundTrip(req)
									Expect(err).ToNot(HaveOccurred())

									newCookies := resp.Cookies()
									Expect(newCookies).To(HaveLen(1))

									Expect(newCookies[0].Name).To(Equal(round_tripper.VcapCookieId))
									Expect(newCookies[0].Value).To(Equal("vcap-id-property-already-on-the-response"))
								})
							})
						})
					})
				})

			})
			Context("when endpoint timeout is not 0", func() {
				var reqCh chan *http.Request
				BeforeEach(func() {
					cfg.EndpointTimeout = 10 * time.Millisecond
					reqCh = make(chan *http.Request, 1)

					transport.RoundTripStub = func(req *http.Request) (*http.Response, error) {
						reqCh <- req
						return &http.Response{}, nil
					}
				})

				It("sets a timeout on the request context", func() {
					proxyRoundTripper.RoundTrip(req)
					var request *http.Request
					Eventually(reqCh).Should(Receive(&request))

					_, deadlineSet := request.Context().Deadline()
					Expect(deadlineSet).To(BeTrue())
					Eventually(func() string {
						err := request.Context().Err()
						if err != nil {
							return err.Error()
						}
						return ""
					}).Should(ContainSubstring("deadline exceeded"))
				})

				Context("when the round trip errors the deadline is cancelled", func() {
					BeforeEach(func() {
						transport.RoundTripStub = func(req *http.Request) (*http.Response, error) {
							reqCh <- req
							return &http.Response{}, errors.New("boom!")
						}
					})

					It("sets a timeout on the request context", func() {
						_, err := proxyRoundTripper.RoundTrip(req)
						Expect(err).To(HaveOccurred())
						var request *http.Request
						Eventually(reqCh).Should(Receive(&request))

						err = request.Context().Err()
						Expect(err).NotTo(BeNil())
						Expect(err.Error()).To(ContainSubstring("canceled"))
					})
				})

				Context("when route service url is not nil", func() {
					var routeServiceURL *url.URL
					BeforeEach(func() {
						var err error
						routeServiceURL, err = url.Parse("https://foo.com")
						Expect(err).ToNot(HaveOccurred())
						reqInfo.RouteServiceURL = routeServiceURL
						transport.RoundTripStub = func(req *http.Request) (*http.Response, error) {
							reqCh <- req
							Expect(req.Host).To(Equal(routeServiceURL.Host))
							Expect(req.URL).To(Equal(routeServiceURL))
							return nil, nil
						}
					})

					It("sets a timeout on the request context", func() {
						proxyRoundTripper.RoundTrip(req)
						var request *http.Request
						Eventually(reqCh).Should(Receive(&request))

						_, deadlineSet := request.Context().Deadline()
						Expect(deadlineSet).To(BeTrue())
						Eventually(func() string {
							err := request.Context().Err()
							if err != nil {
								return err.Error()
							}
							return ""
						}).Should(ContainSubstring("deadline exceeded"))
					})

					Context("when the round trip errors the deadline is cancelled", func() {
						BeforeEach(func() {
							transport.RoundTripStub = func(req *http.Request) (*http.Response, error) {
								reqCh <- req
								Expect(req.Host).To(Equal(routeServiceURL.Host))
								Expect(req.URL).To(Equal(routeServiceURL))
								return &http.Response{}, errors.New("boom!")
							}
						})

						It("sets a timeout on the request context", func() {
							_, err := proxyRoundTripper.RoundTrip(req)
							Expect(err).To(HaveOccurred())
							var request *http.Request
							Eventually(reqCh).Should(Receive(&request))

							err = request.Context().Err()
							Expect(err).NotTo(BeNil())
							Expect(err.Error()).To(ContainSubstring("canceled"))
						})
					})

				})
			})

			Context("CancelRequest", func() {
				It("can cancel requests", func() {
					reqInfo.RouteEndpoint = endpoint
					proxyRoundTripper.CancelRequest(req)
					Expect(transport.CancelRequestCallCount()).To(Equal(1))
					Expect(transport.CancelRequestArgsForCall(0)).To(Equal(req))
				})
			})
		})
	})
})
