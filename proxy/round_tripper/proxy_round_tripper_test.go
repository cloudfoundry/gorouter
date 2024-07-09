package round_tripper_test

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/http/httptrace"
	"net/textproto"
	"net/url"
	"strings"
	"sync"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/onsi/gomega/gbytes"
	"github.com/uber-go/zap"

	"code.cloudfoundry.org/gorouter/common/uuid"
	"code.cloudfoundry.org/gorouter/config"
	"code.cloudfoundry.org/gorouter/handlers"
	"code.cloudfoundry.org/gorouter/metrics/fakes"
	"code.cloudfoundry.org/gorouter/proxy/fails"
	"code.cloudfoundry.org/gorouter/proxy/round_tripper"
	"code.cloudfoundry.org/gorouter/proxy/utils"
	"code.cloudfoundry.org/gorouter/route"
	"code.cloudfoundry.org/gorouter/routeservice"
	"code.cloudfoundry.org/gorouter/test_util"

	sharedfakes "code.cloudfoundry.org/gorouter/fakes"
	errorClassifierFakes "code.cloudfoundry.org/gorouter/proxy/fails/fakes"
	roundtripperfakes "code.cloudfoundry.org/gorouter/proxy/round_tripper/fakes"
)

const StickyCookieKey = "JSESSIONID"
const AZ = "meow-zone"
const AZPreference = "none"

type testBody struct {
	bytes.Buffer
	closeCount int
}

func (t *testBody) Close() error {
	t.closeCount++
	return nil
}

type RequestedRoundTripperType struct {
	IsRouteService bool
	IsHttp2        bool
}

type FakeRoundTripperFactory struct {
	ReturnValue                round_tripper.ProxyRoundTripper
	RequestedRoundTripperTypes []RequestedRoundTripperType
}

func (f *FakeRoundTripperFactory) New(expectedServerName string, isRouteService bool, isHttp2 bool) round_tripper.ProxyRoundTripper {
	f.RequestedRoundTripperTypes = append(f.RequestedRoundTripperTypes, RequestedRoundTripperType{
		IsRouteService: isRouteService,
		IsHttp2:        isHttp2,
	})
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
			combinedReporter       *fakes.FakeProxyReporter
			roundTripperFactory    *FakeRoundTripperFactory
			routeServicesTransport *sharedfakes.RoundTripper
			retriableClassifier    *errorClassifierFakes.Classifier
			errorHandler           *roundtripperfakes.ErrorHandler
			cfg                    *config.Config

			reqInfo *handlers.RequestInfo

			numEndpoints int
			endpoint     *route.Endpoint

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
			numEndpoints = 1
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
			combinedReporter = new(fakes.FakeProxyReporter)
			errorHandler = &roundtripperfakes.ErrorHandler{}
			roundTripperFactory = &FakeRoundTripperFactory{ReturnValue: transport}
			retriableClassifier = &errorClassifierFakes.Classifier{}
			retriableClassifier.ClassifyReturns(false)
			routeServicesTransport = &sharedfakes.RoundTripper{}

			cfg, err = config.DefaultConfig()
			Expect(err).ToNot(HaveOccurred())
			cfg.EndpointTimeout = 0 * time.Millisecond
			cfg.Backends.MaxAttempts = 3
			cfg.RouteServiceConfig.MaxAttempts = 3
			cfg.StickySessionsForAuthNegotiate = true
		})

		JustBeforeEach(func() {
			for i := 1; i <= numEndpoints; i++ {
				endpoint = route.NewEndpoint(&route.EndpointOpts{
					AppId:                fmt.Sprintf("appID%d", i),
					Host:                 fmt.Sprintf("%d.%d.%d.%d", i, i, i, i),
					Port:                 9090,
					PrivateInstanceId:    fmt.Sprintf("instanceID%d", i),
					PrivateInstanceIndex: fmt.Sprintf("%d", i),
					AvailabilityZone:     AZ,
				})

				added := routePool.Put(endpoint)
				Expect(added).To(Equal(route.ADDED))
			}

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
					Expect(outreq.Header.Get("X-CF-ApplicationID")).To(Equal("appID1"))
					Expect(outreq.Header.Get("X-CF-InstanceID")).To(Equal("instanceID1"))
					Expect(outreq.Header.Get("X-CF-InstanceIndex")).To(Equal("1"))
				})
			})

			Context("when some backends fail", func() {
				BeforeEach(func() {
					numEndpoints = 3
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

					Expect(reqInfo.RoundTripSuccessful).To(BeTrue())
					Expect(res.StatusCode).To(Equal(http.StatusTeapot))
				})

				It("captures each routing request to the backend", func() {
					_, err := proxyRoundTripper.RoundTrip(req)
					Expect(err).NotTo(HaveOccurred())

					Expect(combinedReporter.CaptureRoutingRequestCallCount()).To(Equal(3))
					// Test if each endpoint has been used
					routePool.Each(func(endpoint *route.Endpoint) {
						found := false
						for i := 0; i < 3; i++ {
							if combinedReporter.CaptureRoutingRequestArgsForCall(i) == endpoint {
								found = true
							}
						}
						Expect(found).To(BeTrue())
					})
				})

				It("logs the error and removes offending backend", func() {
					res, err := proxyRoundTripper.RoundTrip(req)
					Expect(err).NotTo(HaveOccurred())

					iter := routePool.Endpoints(logger, "", false, AZPreference, AZ)
					ep1 := iter.Next(0)
					ep2 := iter.Next(1)
					Expect(ep1.PrivateInstanceId).To(Equal(ep2.PrivateInstanceId))

					errorLogs := logger.Lines(zap.ErrorLevel)
					Expect(len(errorLogs)).To(BeNumerically(">=", 2))
					count := 0
					for i := 0; i < len(errorLogs); i++ {
						if strings.Contains(errorLogs[i], "backend-endpoint-failed") {
							count++
						}
						Expect(errorLogs[i]).To(ContainSubstring(AZ))
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

			Context("with 5 backends, 4 of them failing", func() {
				BeforeEach(func() {
					numEndpoints = 5
					transport.RoundTripStub = func(*http.Request) (*http.Response, error) {
						switch transport.RoundTripCallCount() {
						case 1:
							return nil, &net.OpError{Op: "dial", Err: errors.New("connection refused")}
						case 2:
							return nil, &net.OpError{Op: "dial", Err: errors.New("connection refused")}
						case 3:
							return nil, &net.OpError{Op: "dial", Err: errors.New("connection refused")}
						case 4:
							return nil, &net.OpError{Op: "dial", Err: errors.New("connection refused")}
						case 5:
							return &http.Response{StatusCode: http.StatusTeapot}, nil
						default:
							return nil, nil
						}
					}

					retriableClassifier.ClassifyReturns(true)
				})

				Context("when MaxAttempts is set to 4", func() {
					BeforeEach(func() {
						cfg.Backends.MaxAttempts = 4
					})

					It("stops after 4 tries, returning an error", func() {
						_, err := proxyRoundTripper.RoundTrip(req)
						Expect(err).To(MatchError(ContainSubstring("connection refused")))
						Expect(transport.RoundTripCallCount()).To(Equal(4))
						Expect(retriableClassifier.ClassifyCallCount()).To(Equal(4))
						Expect(reqInfo.RoundTripSuccessful).To(BeFalse())
					})
				})

				Context("when MaxAttempts is set to 10", func() {
					BeforeEach(func() {
						cfg.Backends.MaxAttempts = 10
					})

					It("retries until success", func() {
						res, err := proxyRoundTripper.RoundTrip(req)
						Expect(err).NotTo(HaveOccurred())
						Expect(transport.RoundTripCallCount()).To(Equal(5))
						Expect(retriableClassifier.ClassifyCallCount()).To(Equal(4))
						Expect(reqInfo.RoundTripSuccessful).To(BeTrue())
						Expect(res.StatusCode).To(Equal(http.StatusTeapot))
					})
				})
			})

			Context("with 5 backends, all of them failing", func() {
				BeforeEach(func() {
					numEndpoints = 5
					transport.RoundTripStub = func(*http.Request) (*http.Response, error) {
						return nil, &net.OpError{Op: "dial", Err: errors.New("connection refused")}
					}

					retriableClassifier.ClassifyReturns(true)
				})

				Context("when MaxAttempts is set to 4", func() {
					BeforeEach(func() {
						cfg.Backends.MaxAttempts = 4
					})

					It("stops after 4 tries, returning an error", func() {
						_, err := proxyRoundTripper.RoundTrip(req)
						Expect(err).To(MatchError(ContainSubstring("connection refused")))
						Expect(transport.RoundTripCallCount()).To(Equal(4))
						Expect(retriableClassifier.ClassifyCallCount()).To(Equal(4))
						Expect(reqInfo.RoundTripSuccessful).To(BeFalse())
					})
				})

				Context("when MaxAttempts is set to 10", func() {
					BeforeEach(func() {
						cfg.Backends.MaxAttempts = 10
					})

					It("still stops after 5 tries when all backends have been tried, returning an error", func() {
						_, err := proxyRoundTripper.RoundTrip(req)
						Expect(err).To(MatchError(ContainSubstring("connection refused")))
						Expect(transport.RoundTripCallCount()).To(Equal(5))
						Expect(retriableClassifier.ClassifyCallCount()).To(Equal(5))
						Expect(reqInfo.RoundTripSuccessful).To(BeFalse())
					})
				})

				Context("when MaxAttempts is set to 0 (illegal value)", func() {
					BeforeEach(func() {
						cfg.Backends.MaxAttempts = 0
					})

					It("still tries once, returning an error", func() {
						_, err := proxyRoundTripper.RoundTrip(req)
						Expect(err).To(MatchError(ContainSubstring("connection refused")))
						Expect(transport.RoundTripCallCount()).To(Equal(1))
						Expect(retriableClassifier.ClassifyCallCount()).To(Equal(1))
						Expect(reqInfo.RoundTripSuccessful).To(BeFalse())
					})
				})

				Context("when MaxAttempts is set to < 0 (illegal value)", func() {
					BeforeEach(func() {
						cfg.Backends.MaxAttempts = -1
					})

					It("still tries once, returning an error", func() {
						_, err := proxyRoundTripper.RoundTrip(req)
						Expect(err).To(MatchError(ContainSubstring("connection refused")))
						Expect(transport.RoundTripCallCount()).To(Equal(1))
						Expect(retriableClassifier.ClassifyCallCount()).To(Equal(1))
						Expect(reqInfo.RoundTripSuccessful).To(BeFalse())
					})
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
					Expect(reqInfo.RoundTripSuccessful).To(BeFalse())
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

					iter := routePool.Endpoints(logger, "", false, AZPreference, AZ)
					ep1 := iter.Next(0)
					ep2 := iter.Next(1)
					Expect(ep1).To(Equal(ep2))

					logOutput := logger.Buffer()
					Expect(logOutput).To(gbytes.Say(`backend-endpoint-failed`))
					Expect(logOutput).To(gbytes.Say(`vcap_request_id`))
				})
			})

			Context("when backend writes 1xx response but fails eventually", func() {
				var events chan string
				// This situation is causing data race in ReverseProxy
				// See an issue https://github.com/golang/go/issues/65123

				BeforeEach(func() {
					events = make(chan string, 4)

					trace := &httptrace.ClientTrace{
						Got1xxResponse: func(code int, header textproto.MIMEHeader) error {
							events <- "callback started"
							defer func() {
								events <- "callback finished"
							}()

							for i := 0; i < 1000000; i++ {
								resp.Header().Set("X-Something", "Hello")
							}
							return nil
						},
					}
					req = req.WithContext(httptrace.WithClientTrace(req.Context(), trace))
					transport.RoundTripStub = func(req *http.Request) (*http.Response, error) {
						go func() {
							// emulating readLoop running after the RoundTrip and modifying response headers
							trace := httptrace.ContextClientTrace(req.Context())
							if trace != nil && trace.Got1xxResponse != nil {
								trace.Got1xxResponse(http.StatusContinue, textproto.MIMEHeader{})
							}
						}()
						return nil, errors.New("failed-roundtrip")
					}

					errorHandler.HandleErrorStub = func(rw utils.ProxyResponseWriter, err error) {
						events <- "error handler started"
						defer func() {
							events <- "error handler finished"
						}()

						for i := 0; i < 1000000; i++ {
							rw.Header().Set("X-From-Error-Handler", "Hello")
						}
					}
				})

				It("ensures that the Got1xxResponse callback and the error handler are not called concurrently", func() {
					_, err := proxyRoundTripper.RoundTrip(req)
					Expect(err).To(HaveOccurred())

					eventsList := []string{}
					for i := 0; i < 4; i++ {
						eventsList = append(eventsList, <-events)
					}

					Expect(eventsList).To(Or(
						Equal([]string{"callback started", "callback finished", "error handler started", "error handler finished"}),
						Equal([]string{"error handler started", "error handler finished", "callback started", "callback finished"}),
					))
				})
			})

			Context("with two endpoints, one of them failing", func() {
				BeforeEach(func() {
					numEndpoints = 2
				})

				DescribeTable("when the backend fails with an empty response error (io.EOF)",
					func(reqBody io.ReadCloser, getBodyIsNil bool, reqMethod string, headers map[string]string, classify fails.ClassifierFunc, expectRetry bool) {
						badResponse := &http.Response{
							Header: make(map[string][]string),
						}
						badResponse.Header.Add(handlers.VcapRequestIdHeader, "some-request-id")

						// The first request fails with io.EOF, the second (if retried) succeeds
						transport.RoundTripStub = func(*http.Request) (*http.Response, error) {
							switch transport.RoundTripCallCount() {
							case 1:
								return nil, io.EOF
							case 2:
								return &http.Response{StatusCode: http.StatusTeapot}, nil
							default:
								return nil, nil
							}
						}

						retriableClassifier.ClassifyStub = classify
						req.Method = reqMethod
						req.Body = reqBody
						if !getBodyIsNil {
							req.GetBody = func() (io.ReadCloser, error) {
								return new(testBody), nil
							}
						}
						for key, value := range headers {
							req.Header.Add(key, value)
						}

						res, err := proxyRoundTripper.RoundTrip(req)

						if expectRetry {
							Expect(err).NotTo(HaveOccurred())
							Expect(transport.RoundTripCallCount()).To(Equal(2))
							Expect(retriableClassifier.ClassifyCallCount()).To(Equal(1))
							Expect(res.StatusCode).To(Equal(http.StatusTeapot))
						} else {
							Expect(errors.Is(err, io.EOF)).To(BeTrue())
							Expect(transport.RoundTripCallCount()).To(Equal(1))
							Expect(retriableClassifier.ClassifyCallCount()).To(Equal(1))
						}
					},

					Entry("POST, body is empty: does not retry", nil, true, "POST", nil, fails.IdempotentRequestEOF, false),
					Entry("POST, body is not empty and GetBody is non-nil: does not retry", reqBody, false, "POST", nil, fails.IdempotentRequestEOF, false),
					Entry("POST, body is not empty: does not retry", reqBody, true, "POST", nil, fails.IdempotentRequestEOF, false),
					Entry("POST, body is http.NoBody: does not retry", http.NoBody, true, "POST", nil, fails.IdempotentRequestEOF, false),

					Entry("POST, body is empty, X-Idempotency-Key header: attempts retry", nil, true, "POST", map[string]string{"X-Idempotency-Key": "abc123"}, fails.IncompleteRequest, true),
					Entry("POST, body is not empty and GetBody is non-nil, X-Idempotency-Key header: attempts retry", reqBody, false, "POST", map[string]string{"X-Idempotency-Key": "abc123"}, fails.IncompleteRequest, true),
					Entry("POST, body is not empty, X-Idempotency-Key header: does not retry", reqBody, true, "POST", map[string]string{"X-Idempotency-Key": "abc123"}, fails.IdempotentRequestEOF, false),
					Entry("POST, body is http.NoBody, X-Idempotency-Key header: does not retry", http.NoBody, true, "POST", map[string]string{"X-Idempotency-Key": "abc123"}, fails.IdempotentRequestEOF, false),

					Entry("POST, body is empty, Idempotency-Key header: attempts retry", nil, true, "POST", map[string]string{"Idempotency-Key": "abc123"}, fails.IncompleteRequest, true),
					Entry("POST, body is not empty and GetBody is non-nil, Idempotency-Key header: attempts retry", reqBody, false, "POST", map[string]string{"Idempotency-Key": "abc123"}, fails.IncompleteRequest, true),
					Entry("POST, body is not empty, Idempotency-Key header: does not retry", reqBody, true, "POST", map[string]string{"Idempotency-Key": "abc123"}, fails.IdempotentRequestEOF, false),
					Entry("POST, body is http.NoBody, Idempotency-Key header: does not retry", http.NoBody, true, "POST", map[string]string{"Idempotency-Key": "abc123"}, fails.IdempotentRequestEOF, false),

					Entry("GET, body is empty: attempts retry", nil, true, "GET", nil, fails.IncompleteRequest, true),
					Entry("GET, body is not empty and GetBody is non-nil: attempts retry", reqBody, false, "GET", nil, fails.IncompleteRequest, true),
					Entry("GET, body is not empty: does not retry", reqBody, true, "GET", nil, fails.IdempotentRequestEOF, false),
					Entry("GET, body is http.NoBody: does not retry", http.NoBody, true, "GET", nil, fails.IdempotentRequestEOF, false),

					Entry("TRACE, body is empty: attempts retry", nil, true, "TRACE", nil, fails.IncompleteRequest, true),
					Entry("TRACE, body is not empty: does not retry", reqBody, true, "TRACE", nil, fails.IdempotentRequestEOF, false),
					Entry("TRACE, body is http.NoBody: does not retry", http.NoBody, true, "TRACE", nil, fails.IdempotentRequestEOF, false),
					Entry("TRACE, body is not empty and GetBody is non-nil: attempts retry", reqBody, false, "TRACE", nil, fails.IncompleteRequest, true),

					Entry("HEAD, body is empty: attempts retry", nil, true, "HEAD", nil, fails.IncompleteRequest, true),
					Entry("HEAD, body is not empty: does not retry", reqBody, true, "HEAD", nil, fails.IdempotentRequestEOF, false),
					Entry("HEAD, body is http.NoBody: does not retry", http.NoBody, true, "HEAD", nil, fails.IdempotentRequestEOF, false),
					Entry("HEAD, body is not empty and GetBody is non-nil: attempts retry", reqBody, false, "HEAD", nil, fails.IncompleteRequest, true),

					Entry("OPTIONS, body is empty: attempts retry", nil, true, "OPTIONS", nil, fails.IncompleteRequest, true),
					Entry("OPTIONS, body is not empty and GetBody is non-nil: attempts retry", reqBody, false, "OPTIONS", nil, fails.IncompleteRequest, true),
					Entry("OPTIONS, body is not empty: does not retry", reqBody, true, "OPTIONS", nil, fails.IdempotentRequestEOF, false),
					Entry("OPTIONS, body is http.NoBody: does not retry", http.NoBody, true, "OPTIONS", nil, fails.IdempotentRequestEOF, false),

					Entry("<empty method>, body is empty: attempts retry", nil, true, "", nil, fails.IncompleteRequest, true),
					Entry("<empty method>, body is not empty and GetBody is non-nil: attempts retry", reqBody, false, "", nil, fails.IncompleteRequest, true),
					Entry("<empty method>, body is not empty: does not retry", reqBody, true, "", nil, fails.IdempotentRequestEOF, false),
					Entry("<empty method>, body is http.NoBody: does not retry", http.NoBody, true, "", nil, fails.IdempotentRequestEOF, false),
				)
			})

			Context("when there are no more endpoints available", func() {
				JustBeforeEach(func() {
					removed := routePool.Remove(endpoint)
					Expect(removed).To(BeTrue())
				})

				It("returns a 502 Bad Gateway response", func() {
					backendRes, err := proxyRoundTripper.RoundTrip(req)
					Expect(backendRes).To(BeNil())
					Expect(err).To(Equal(round_tripper.NoEndpointsAvailable))

					Expect(reqInfo.RouteEndpoint).To(BeNil())
					Expect(reqInfo.RoundTripSuccessful).To(BeFalse())
				})

				It("calls the error handler", func() {
					proxyRoundTripper.RoundTrip(req)
					Expect(errorHandler.HandleErrorCallCount()).To(Equal(1))
					_, err := errorHandler.HandleErrorArgsForCall(0)
					Expect(err).To(Equal(round_tripper.NoEndpointsAvailable))
				})

				It("logs a message with `select-endpoint-failed`", func() {
					proxyRoundTripper.RoundTrip(req)
					logOutput := logger.Buffer()
					Expect(logOutput).To(gbytes.Say(`select-endpoint-failed`))
					Expect(logOutput).To(gbytes.Say(`myapp.com`))
				})

				It("does not capture any routing requests to the backend", func() {
					_, err := proxyRoundTripper.RoundTrip(req)
					Expect(err).To(Equal(round_tripper.NoEndpointsAvailable))

					Expect(combinedReporter.CaptureRoutingRequestCallCount()).To(Equal(0))
				})

				It("does not log anything about route services", func() {
					_, err := proxyRoundTripper.RoundTrip(req)
					Expect(err).To(Equal(round_tripper.NoEndpointsAvailable))

					Expect(logger.Buffer()).ToNot(gbytes.Say(`route-service`))
				})

				It("does not report the endpoint failure", func() {
					_, err := proxyRoundTripper.RoundTrip(req)
					Expect(err).To(MatchError(round_tripper.NoEndpointsAvailable))

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
				JustBeforeEach(func() {
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
					JustBeforeEach(func() {
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
					Expect(roundTripperFactory.RequestedRoundTripperTypes).To(Equal([]RequestedRoundTripperType{
						{IsRouteService: false, IsHttp2: false},
					}))

					_, err = proxyRoundTripper.RoundTrip(req)
					Expect(err).ToNot(HaveOccurred())
					Expect(roundTripperFactory.RequestedRoundTripperTypes).To(Equal([]RequestedRoundTripperType{
						{IsRouteService: false, IsHttp2: false},
					}))
				})

				It("does not re-use transports between endpoints", func() {
					endpoint = route.NewEndpoint(&route.EndpointOpts{
						Host: "1.1.1.1", Port: 9091, UseTLS: true, PrivateInstanceId: "instanceId-2",
					})
					added := routePool.Put(endpoint)
					Expect(added).To(Equal(route.ADDED))

					_, err := proxyRoundTripper.RoundTrip(req)
					Expect(err).ToNot(HaveOccurred())
					Expect(roundTripperFactory.RequestedRoundTripperTypes).To(Equal([]RequestedRoundTripperType{
						{IsRouteService: false, IsHttp2: false},
					}))

					_, err = proxyRoundTripper.RoundTrip(req)
					Expect(err).ToNot(HaveOccurred())
					Expect(roundTripperFactory.RequestedRoundTripperTypes).To(Equal([]RequestedRoundTripperType{
						{IsRouteService: false, IsHttp2: false},
						{IsRouteService: false, IsHttp2: false},
					}))

					_, err = proxyRoundTripper.RoundTrip(req)
					Expect(err).ToNot(HaveOccurred())
					Expect(roundTripperFactory.RequestedRoundTripperTypes).To(Equal([]RequestedRoundTripperType{
						{IsRouteService: false, IsHttp2: false},
						{IsRouteService: false, IsHttp2: false},
					}))
				})
			})

			Context("using HTTP/2", func() {
				Context("when HTTP/2 is enabled", func() {
					BeforeEach(func() {
						cfg.EnableHTTP2 = true
					})
					It("uses HTTP/2 when endpoint's Protocol is set to http2", func() {
						endpoint.Protocol = "http2"
						_, err := proxyRoundTripper.RoundTrip(req)
						Expect(err).ToNot(HaveOccurred())
						Expect(roundTripperFactory.RequestedRoundTripperTypes).To(Equal([]RequestedRoundTripperType{
							{IsRouteService: false, IsHttp2: true},
						}))
					})

					It("does not use HTTP/2 when endpoint's Protocol is not set to http2", func() {
						endpoint.Protocol = ""
						_, err := proxyRoundTripper.RoundTrip(req)
						Expect(err).ToNot(HaveOccurred())
						Expect(roundTripperFactory.RequestedRoundTripperTypes).To(Equal([]RequestedRoundTripperType{
							{IsRouteService: false, IsHttp2: false},
						}))
					})
				})

				Context("when HTTP/2 is disabled", func() {
					BeforeEach(func() {
						cfg.EnableHTTP2 = false
					})

					It("does not use HTTP/2, regardless of the endpoint's protocol", func() {
						endpoint.Protocol = "http2"
						_, err := proxyRoundTripper.RoundTrip(req)
						Expect(err).ToNot(HaveOccurred())
						Expect(roundTripperFactory.RequestedRoundTripperTypes).To(Equal([]RequestedRoundTripperType{
							{IsRouteService: false, IsHttp2: false},
						}))
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
							logOutput := logger.Buffer()
							Expect(logOutput).To(gbytes.Say(`route-service-connection-failed`))
							Expect(logOutput).To(gbytes.Say(`foo.com`))
						}
					})

					Context("when MaxAttempts is set to 5", func() {
						BeforeEach(func() {
							cfg.RouteServiceConfig.MaxAttempts = 5
						})

						It("tries for 5 times before giving up", func() {
							_, err := proxyRoundTripper.RoundTrip(req)
							Expect(err).To(MatchError(dialError))
							Expect(transport.RoundTripCallCount()).To(Equal(5))
						})
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
							logOutput := logger.Buffer()
							Expect(logOutput).To(gbytes.Say(`route-service-connection-failed`))
							Expect(logOutput).To(gbytes.Say(`foo.com`))
						})
					})
				})
			})

			Context("when using sticky sessions", func() {
				var (
					sessionCookie *round_tripper.Cookie
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
						sessionCookie.Partitioned = true
					}

					sessionCookie.Value, _ = uuid.GenerateUUID()
					resp.Header.Add(round_tripper.CookieHeader, sessionCookie.String())
					return resp
				}

				setVCAPID := func(resp *http.Response) (response *http.Response) {
					vcapCookie := round_tripper.Cookie{
						Cookie: http.Cookie{
							Name:  round_tripper.VcapCookieId,
							Value: "vcap-id-property-already-on-the-response",
						},
					}

					if c := vcapCookie.String(); c != "" {
						resp.Header.Add(round_tripper.CookieHeader, c)
					}

					return resp
				}

				setAuthorizationNegotiateHeader := func(resp *http.Response) (response *http.Response) {
					resp.Header.Add("WWW-Authenticate", "Negotiate SOME-TOKEN")
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
					setVCAPID(resp)
					return resp, nil
				}
				responseContainsJSESSIONIDAndVCAPID = func(req *http.Request) (*http.Response, error) {
					resp := &http.Response{StatusCode: http.StatusTeapot, Header: make(map[string][]string)}
					setJSESSIONID(req, resp, false)
					setVCAPID(resp)
					return resp, nil
				}
				responseContainsJSESSIONIDWithExtraProperties = func(req *http.Request) (*http.Response, error) {
					resp := &http.Response{StatusCode: http.StatusTeapot, Header: make(map[string][]string)}
					setJSESSIONID(req, resp, true)
					return resp, nil
				}

				JustBeforeEach(func() {
					sessionCookie = &round_tripper.Cookie{
						Cookie: http.Cookie{
							Name: StickyCookieKey, //JSESSIONID
						},
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

					Context("when there is an 'WWW-Authenticate: Negotiate ...' header set on the response", func() {
						BeforeEach(func() {
							transport.RoundTripStub = func(req *http.Request) (*http.Response, error) {
								resp := &http.Response{StatusCode: http.StatusTeapot, Header: make(map[string][]string)}
								setAuthorizationNegotiateHeader(resp)
								return resp, nil
							}
						})

						It("will select an endpoint and set the VCAP_ID to the privateInstanceId", func() {
							resp, err := proxyRoundTripper.RoundTrip(req)
							Expect(err).ToNot(HaveOccurred())

							cookies := resp.Cookies()
							Expect(cookies).To(HaveLen(1))
							Expect(cookies[0].Name).To(Equal(round_tripper.VcapCookieId))
							Expect(cookies[0].Value).To(SatisfyAny(
								Equal("id-1"),
								Equal("id-2")))
							Expect(cookies[0].MaxAge).To(Equal(60))
							Expect(cookies[0].Expires).To(Equal(time.Time{}))
							Expect(cookies[0].Secure).To(Equal(cfg.SecureCookies))
							Expect(cookies[0].SameSite).To(Equal(http.SameSiteStrictMode))
						})

						Context("when there is also a VCAP_ID set on the response", func() {
							BeforeEach(func() {
								transport.RoundTripStub = func(req *http.Request) (*http.Response, error) {
									resp := &http.Response{StatusCode: http.StatusTeapot, Header: make(map[string][]string)}
									setAuthorizationNegotiateHeader(resp)
									setVCAPID(resp)
									return resp, nil
								}
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

						Context("when there is also a JSESSIONID and VCAP_ID set on the response", func() {
							BeforeEach(func() {
								transport.RoundTripStub = func(req *http.Request) (*http.Response, error) {
									resp := &http.Response{StatusCode: http.StatusTeapot, Header: make(map[string][]string)}
									setAuthorizationNegotiateHeader(resp)
									setJSESSIONID(req, resp, false)
									setVCAPID(resp)
									return resp, nil
								}
							})

							It("does not overwrite JSESSIONID and VCAP_ID", func() {
								resp, err := proxyRoundTripper.RoundTrip(req)
								Expect(err).ToNot(HaveOccurred())

								cookies := resp.Cookies()
								Expect(cookies).To(HaveLen(2))
								Expect(cookies[0].Raw).To(Equal(sessionCookie.String()))
								Expect(cookies[1].Name).To(Equal(round_tripper.VcapCookieId))
								Expect(cookies[1].Value).To(Equal("vcap-id-property-already-on-the-response"))
							})
						})

						Context("when there is also JSESSIONID cookie with extra properties set", func() {
							BeforeEach(func() {
								transport.RoundTripStub = func(req *http.Request) (*http.Response, error) {
									resp := &http.Response{StatusCode: http.StatusTeapot, Header: make(map[string][]string)}
									setAuthorizationNegotiateHeader(resp)
									setJSESSIONID(req, resp, true)
									return resp, nil
								}
							})

							It("sets the auth negotiate default properties on the VCAP_ID", func() {
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
								Expect(cookies[1].Raw).To(ContainSubstring("Max-Age=60; HttpOnly; SameSite=Strict"))
							})

							Context("when config requires secure cookies", func() {
								BeforeEach(func() {
									cfg.SecureCookies = true
								})

								It("sets the auth negotiate default properties with Secure on the VCAP_ID", func() {
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
									Expect(cookies[1].Raw).To(ContainSubstring("Max-Age=60; HttpOnly; Secure; SameSite=Strict"))
								})
							})
						})
						Context("when sticky sessions for 'Authorization: Negotiate' is disabled", func() {
							BeforeEach(func() {
								cfg.StickySessionsForAuthNegotiate = false
							})
							It("does not set the VCAP_ID cookie", func() {
								resp, err := proxyRoundTripper.RoundTrip(req)
								Expect(err).ToNot(HaveOccurred())

								cookies := resp.Cookies()
								Expect(cookies).To(HaveLen(0))
							})
							Context("when there is also a JSESSIONID cookie with extra properties", func() {
								BeforeEach(func() {
									transport.RoundTripStub = func(req *http.Request) (*http.Response, error) {
										resp := &http.Response{StatusCode: http.StatusTeapot, Header: make(map[string][]string)}
										setAuthorizationNegotiateHeader(resp)
										setJSESSIONID(req, resp, true)
										return resp, nil
									}
								})
								It("sets the VCAP_ID cookie with JSESSION_ID properties", func() {
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

								// This should fail when Golang introduces parsing for the Partitioned flag on cookies.
								// see https://github.com/golang/go/issues/62490
								Expect(newCookies[0].Unparsed).To(Equal([]string{"Partitioned"}))

								Expect(newCookies[1].Name).To(Equal(round_tripper.VcapCookieId))
								Expect(newCookies[1].Value).To(Equal(cookies[1].Value)) // still pointing to the same app
								Expect(sessionCookie.String()).To(ContainSubstring("Expires=Wed, 01 Jan 2020 01:00:00 GMT; HttpOnly; Secure; SameSite=Strict; Partitioned"))
								Expect(newCookies[1].Raw).To(ContainSubstring("Expires=Wed, 01 Jan 2020 01:00:00 GMT; HttpOnly; Secure; SameSite=Strict; Partitioned"))
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
									Expect(cookies[0].MaxAge).To(Equal(0))
									Expect(cookies[0].Expires).To(Equal(time.Time{}))
									Expect(cookies[0].Secure).To(Equal(cfg.SecureCookies))
									Expect(cookies[0].SameSite).To(Equal(http.SameSite(0)))
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

				Context("when VCAP_ID cookie and 'Authorization: Negotiate ...' header are on the request", func() {
					BeforeEach(func() {
						req.AddCookie(&http.Cookie{
							Name:  round_tripper.VcapCookieId,
							Value: "id-2",
						})
						req.Header.Add("Authorization", "Negotiate SOME-TOKEN")
						transport.RoundTripStub = func(req *http.Request) (*http.Response, error) {
							Expect(req.URL.Host).To(Equal("1.1.1.1:9092"))
							resp := &http.Response{StatusCode: http.StatusTeapot, Header: make(map[string][]string)}
							return resp, nil
						}
					})

					It("will select the previous backend and VCAP_ID is set on the response", func() {
						Consistently(func() error {
							_, err := proxyRoundTripper.RoundTrip(req)
							return err
						}).ShouldNot(HaveOccurred())
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
