package proxy_test

import (
	"bytes"
	"crypto/tls"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	. "github.com/onsi/gomega/gbytes"

	fakelogger "code.cloudfoundry.org/gorouter/accesslog/fakes"
	"code.cloudfoundry.org/gorouter/common/health"
	"code.cloudfoundry.org/gorouter/errorwriter"
	sharedfakes "code.cloudfoundry.org/gorouter/fakes"
	"code.cloudfoundry.org/gorouter/metrics"
	"code.cloudfoundry.org/gorouter/metrics/fakes"
	"code.cloudfoundry.org/gorouter/proxy"
	"code.cloudfoundry.org/gorouter/proxy/test_helpers"
	"code.cloudfoundry.org/gorouter/proxy/utils"
	"code.cloudfoundry.org/gorouter/registry"
	"code.cloudfoundry.org/gorouter/route"
	"code.cloudfoundry.org/gorouter/routeservice"
	"code.cloudfoundry.org/gorouter/test_util"
)

var _ = Describe("Proxy Unit tests", func() {
	var (
		proxyObj           http.Handler
		fakeAccessLogger   *fakelogger.FakeAccessLogger
		logger             *test_util.TestLogger
		resp               utils.ProxyResponseWriter
		combinedReporter   metrics.ProxyReporter
		routeServiceConfig *routeservice.RouteServiceConfig
		rt                 *sharedfakes.RoundTripper
		tlsConfig          *tls.Config
		ew                 = errorwriter.NewPlaintextErrorWriter()
		responseRecorder   *ResponseRecorderWithFullDuplex
	)

	Describe("ServeHTTP", func() {
		BeforeEach(func() {
			tlsConfig = &tls.Config{
				CipherSuites:       conf.CipherSuites,
				InsecureSkipVerify: conf.SkipSSLValidation,
			}

			fakeAccessLogger = &fakelogger.FakeAccessLogger{}

			logger = test_util.NewTestLogger("test")
			r = registry.NewRouteRegistry(logger.Logger, conf, new(fakes.FakeRouteRegistryReporter))

			routeServiceConfig = routeservice.NewRouteServiceConfig(
				logger.Logger,
				conf.RouteServiceEnabled,
				conf.RouteServicesHairpinning,
				conf.RouteServicesHairpinningAllowlist,
				conf.RouteServiceTimeout,
				crypto,
				cryptoPrev,
				false,
				false,
			)
			varz := test_helpers.NullVarz{}
			sender := new(fakes.MetricSender)
			batcher := new(fakes.MetricBatcher)
			proxyReporter := &metrics.MetricsReporter{Sender: sender, Batcher: batcher}
			combinedReporter = &metrics.CompositeReporter{VarzReporter: varz, ProxyReporter: proxyReporter}

			rt = &sharedfakes.RoundTripper{}
			conf.HealthCheckUserAgent = "HTTP-Monitor/1.1"

			skipSanitization = func(req *http.Request) bool { return false }
			proxyObj = proxy.NewProxy(logger.Logger, fakeAccessLogger, fakeRegistry, ew, conf, r, combinedReporter,
				routeServiceConfig, tlsConfig, tlsConfig, &health.Health{}, rt)

			r.Register(route.Uri("some-app"), &route.Endpoint{Stats: route.NewStats()})

			responseRecorder = &ResponseRecorderWithFullDuplex{httptest.NewRecorder(), nil, 0}
			resp = utils.NewProxyResponseWriter(responseRecorder)
		})

		Context("when backend fails to respond", func() {
			It("logs the error and associated endpoint", func() {
				body := []byte("some body")
				req := test_util.NewRequest("GET", "some-app", "/", bytes.NewReader(body))

				proxyObj.ServeHTTP(resp, req)

				Eventually(logger).Should(Say("route-endpoint"))
				Eventually(logger).Should(Say("error"))
			})
		})

		Describe("full duplex", func() {
			Context("for HTTP/1.1 requests", func() {
				Context("when concurrent read write is enabled", func() {
					BeforeEach(func() {
						conf.EnableHTTP1ConcurrentReadWrite = true
					})

					It("enables full duplex", func() {
						req := test_util.NewRequest("GET", "some-app", "/", bytes.NewReader([]byte("some-body")))
						proxyObj.ServeHTTP(resp, req)
						Expect(responseRecorder.EnableFullDuplexCallCount).To(Equal(1))
					})

					Context("when enabling duplex fails", func() {
						It("fails", func() {
							responseRecorder.EnableFullDuplexErr = errors.New("unsupported")
							req := test_util.NewRequest("GET", "some-app", "/", bytes.NewReader([]byte("some-body")))
							proxyObj.ServeHTTP(resp, req)

							Eventually(logger).Should(Say("enable-full-duplex-err"))
						})
					})
				})

				Context("when concurrent read write is not enabled", func() {
					BeforeEach(func() {
						conf.EnableHTTP1ConcurrentReadWrite = false
					})

					It("does not enable full duplex", func() {
						req := test_util.NewRequest("GET", "some-app", "/", bytes.NewReader([]byte("some-body")))
						proxyObj.ServeHTTP(resp, req)
						Expect(responseRecorder.EnableFullDuplexCallCount).To(Equal(0))
					})
				})
			})

			Context("for HTTP/2 requests", func() {
				It("does not enable full duplex", func() {
					req := test_util.NewRequest("GET", "some-app", "/", bytes.NewReader([]byte("some-body")))
					req.ProtoMajor = 2
					proxyObj.ServeHTTP(resp, req)
					Expect(responseRecorder.EnableFullDuplexCallCount).To(Equal(0))
				})
			})
		})

		Context("Log response time", func() {
			It("logs response time for HTTP connections", func() {
				body := []byte("some body")
				req := test_util.NewRequest("GET", "some-app", "/", bytes.NewReader(body))

				proxyObj.ServeHTTP(resp, req)
				Expect(fakeAccessLogger.LogCallCount()).To(Equal(1))
				Expect(fakeAccessLogger.LogArgsForCall(0).FinishedAt).NotTo(Equal(time.Time{}))
			})

			It("logs response time for TCP connections", func() {
				req := test_util.NewRequest("UPGRADE", "some-app", "/", nil)
				req.Header.Set("Upgrade", "tcp")
				req.Header.Set("Connection", "upgrade")

				proxyObj.ServeHTTP(resp, req)
				Expect(fakeAccessLogger.LogCallCount()).To(Equal(1))
				Expect(fakeAccessLogger.LogArgsForCall(0).FinishedAt).NotTo(Equal(time.Time{}))

			})

			It("logs response time for Web Socket connections", func() {
				req := test_util.NewRequest("UPGRADE", "some-app", "/", nil)
				req.Header.Set("Upgrade", "websocket")
				req.Header.Set("Connection", "upgrade")

				proxyObj.ServeHTTP(resp, req)
				Expect(fakeAccessLogger.LogCallCount()).To(Equal(1))
				Expect(fakeAccessLogger.LogArgsForCall(0).FinishedAt).NotTo(Equal(time.Time{}))
			})
		})
	})

	Describe("SkipSanitizeXFP", func() {
		DescribeTable("the returned function",
			func(viaRouteService proxy.RouteServiceValidator, expectedValue bool) {
				skipSanitizeRouteService := proxy.SkipSanitizeXFP(viaRouteService)
				skip := skipSanitizeRouteService(&http.Request{})
				Expect(skip).To(Equal(expectedValue))
			},
			Entry("routeServiceTraffic",
				routeServiceTraffic, true),
			Entry("notRouteServiceTraffic",
				notRouteServiceTraffic, false),
		)
	})

	Describe("SkipSanitize", func() {
		DescribeTable("the returned function",
			func(viaRouteService proxy.RouteServiceValidator, reqTLS *tls.ConnectionState, expectedValue bool) {
				skipSanitizationFunc := proxy.SkipSanitize(viaRouteService)
				skipSanitization := skipSanitizationFunc(&http.Request{TLS: reqTLS})
				Expect(skipSanitization).To(Equal(expectedValue))
			},
			Entry("notRouteServiceTraffic, req.TLS == nil",
				notRouteServiceTraffic, nil, false),
			Entry("notRouteServiceTraffic, req.TLS != nil",
				notRouteServiceTraffic, &tls.ConnectionState{}, false),
			Entry("routeServiceTraffic, req.TLS == nil",
				routeServiceTraffic, nil, false),
			Entry("routeServiceTraffic, req.TLS != nil",
				routeServiceTraffic, &tls.ConnectionState{}, true),
		)
	})

	Describe("ForceDeleteXFCCHeader", func() {
		BeforeEach(func() {
			logger = test_util.NewTestLogger("test")
		})
		DescribeTable("the returned function",
			func(arrivedViaRouteService proxy.RouteServiceValidator, lgr func() *slog.Logger, forwardedClientCert string, expectedValue bool, expectedErr error) {
				forceDeleteXFCCHeaderFunc := proxy.ForceDeleteXFCCHeader(arrivedViaRouteService, forwardedClientCert, lgr())
				forceDelete, err := forceDeleteXFCCHeaderFunc(&http.Request{})
				if expectedErr != nil {
					Expect(err).To(Equal(expectedErr))
				} else {
					Expect(err).NotTo(HaveOccurred())
				}
				Expect(forceDelete).To(Equal(expectedValue))
			},
			Entry("arrivedViaRouteService returns (false, nil), forwardedClientCert == sanitize_set",
				notArrivedViaRouteService, func() *slog.Logger { return logger.Logger }, "sanitize_set", false, nil),
			Entry("arrivedViaRouteService returns (false, nil), forwardedClientCert != sanitize_set",
				notArrivedViaRouteService, func() *slog.Logger { return logger.Logger }, "", false, nil),
			Entry("arrivedViaRouteService returns (true, nil), forwardedClientCert == sanitize_set",
				arrivedViaRouteService, func() *slog.Logger { return logger.Logger }, "sanitize_set", false, nil),
			Entry("arrivedViaRouteService returns (true, nil), forwardedClientCert != sanitize_set",
				arrivedViaRouteService, func() *slog.Logger { return logger.Logger }, "", true, nil),
			Entry("arrivedViaRouteService returns (false, error), forwardedClientCert == sanitize_set",
				errorViaRouteService, func() *slog.Logger { return logger.Logger }, "sanitize_set", false, errors.New("Bad route service validator")),
			Entry("arrivedViaRouteService returns (false, error), forwardedClientCert != sanitize_set",
				errorViaRouteService, func() *slog.Logger { return logger.Logger }, "", false, errors.New("Bad route service validator")),
		)
	})
})

var notRouteServiceTraffic = &hasBeenToRouteServiceValidatorFake{
	ValidatedIsRouteServiceTrafficCall: call{
		Returns: returns{
			Value: false,
		},
	},
}

var routeServiceTraffic = &hasBeenToRouteServiceValidatorFake{
	ValidatedIsRouteServiceTrafficCall: call{
		Returns: returns{
			Value: true,
		},
	},
}

var notArrivedViaRouteService = &hasBeenToRouteServiceValidatorFake{
	ValidatedHasBeenToRouteServiceCall: call{
		Returns: returns{
			Value: false,
			Error: nil,
		},
	},
}

var arrivedViaRouteService = &hasBeenToRouteServiceValidatorFake{
	ValidatedHasBeenToRouteServiceCall: call{
		Returns: returns{
			Value: true,
			Error: nil,
		},
	},
}
var errorViaRouteService = &hasBeenToRouteServiceValidatorFake{
	ValidatedHasBeenToRouteServiceCall: call{
		Returns: returns{
			Value: true,
			Error: errors.New("Bad route service validator"),
		},
	},
}

type hasBeenToRouteServiceValidatorFake struct {
	ValidatedHasBeenToRouteServiceCall call
	ValidatedIsRouteServiceTrafficCall call
}
type call struct {
	Returns returns
}
type returns struct {
	Value bool
	Error error
}

func (h *hasBeenToRouteServiceValidatorFake) ArrivedViaRouteService(req *http.Request, logger *slog.Logger) (bool, error) {
	return h.ValidatedHasBeenToRouteServiceCall.Returns.Value, h.ValidatedHasBeenToRouteServiceCall.Returns.Error
}

func (h *hasBeenToRouteServiceValidatorFake) IsRouteServiceTraffic(req *http.Request) bool {
	return h.ValidatedIsRouteServiceTrafficCall.Returns.Value
}

type ResponseRecorderWithFullDuplex struct {
	*httptest.ResponseRecorder

	EnableFullDuplexErr       error
	EnableFullDuplexCallCount int
}

func (r *ResponseRecorderWithFullDuplex) EnableFullDuplex() error {
	r.EnableFullDuplexCallCount++
	return r.EnableFullDuplexErr
}
