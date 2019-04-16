package proxy_test

import (
	"bytes"
	"crypto/tls"
	"errors"
	"net/http"
	"net/http/httptest"
	"time"

	"code.cloudfoundry.org/gorouter/common/threading"

	fakelogger "code.cloudfoundry.org/gorouter/accesslog/fakes"
	sharedfakes "code.cloudfoundry.org/gorouter/fakes"
	"code.cloudfoundry.org/gorouter/logger"
	"code.cloudfoundry.org/gorouter/metrics"
	"code.cloudfoundry.org/gorouter/metrics/fakes"
	"code.cloudfoundry.org/gorouter/proxy"
	"code.cloudfoundry.org/gorouter/proxy/test_helpers"
	"code.cloudfoundry.org/gorouter/proxy/utils"
	"code.cloudfoundry.org/gorouter/registry"
	"code.cloudfoundry.org/gorouter/route"
	"code.cloudfoundry.org/gorouter/routeservice"
	"code.cloudfoundry.org/gorouter/test_util"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/ginkgo/extensions/table"
	. "github.com/onsi/gomega"
	. "github.com/onsi/gomega/gbytes"
)

var _ = Describe("Proxy Unit tests", func() {
	var (
		proxyObj           http.Handler
		fakeAccessLogger   *fakelogger.FakeAccessLogger
		logger             logger.Logger
		resp               utils.ProxyResponseWriter
		combinedReporter   metrics.ProxyReporter
		routeServiceConfig *routeservice.RouteServiceConfig
		rt                 *sharedfakes.RoundTripper
		tlsConfig          *tls.Config
	)

	Describe("ServeHTTP", func() {
		BeforeEach(func() {
			tlsConfig = &tls.Config{
				CipherSuites:       conf.CipherSuites,
				InsecureSkipVerify: conf.SkipSSLValidation,
			}

			fakeAccessLogger = &fakelogger.FakeAccessLogger{}

			logger = test_util.NewTestZapLogger("test")
			r = registry.NewRouteRegistry(logger, conf, new(fakes.FakeRouteRegistryReporter))

			routeServiceConfig = routeservice.NewRouteServiceConfig(
				logger,
				conf.RouteServiceEnabled,
				conf.RouteServicesHairpinning,
				conf.RouteServiceTimeout,
				crypto,
				cryptoPrev,
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
			proxyObj = proxy.NewProxy(logger, fakeAccessLogger, conf, r, combinedReporter,
				routeServiceConfig, tlsConfig, tlsConfig, &threading.SharedBoolean{}, rt)

			r.Register(route.Uri("some-app"), &route.Endpoint{Stats: route.NewStats()})

			resp = utils.NewProxyResponseWriter(httptest.NewRecorder())
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

		Context("when the route registry is nil, causing the proxy to panic", func() {
			var healthCheck *threading.SharedBoolean
			BeforeEach(func() {
				healthCheck = &threading.SharedBoolean{}
				healthCheck.Set(true)
				proxyObj = proxy.NewProxy(logger, fakeAccessLogger, conf, nil, combinedReporter, routeServiceConfig, tlsConfig, tlsConfig, healthCheck, rt)
			})

			It("fails the healthcheck", func() {
				req := test_util.NewRequest("GET", "some-app", "/", nil)

				proxyObj.ServeHTTP(resp, req)
				Expect(healthCheck.Get()).To(BeFalse())

				req.Header.Set("User-Agent", "HTTP-Monitor/1.1")
				proxyObj.ServeHTTP(resp, req)
				Expect(resp.Status()).To(Equal(http.StatusServiceUnavailable))
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
		DescribeTable("the returned function",
			func(arrivedViaRouteService proxy.RouteServiceValidator, forwardedClientCert string, expectedValue bool, expectedErr error) {
				forceDeleteXFCCHeaderFunc := proxy.ForceDeleteXFCCHeader(arrivedViaRouteService, forwardedClientCert)
				forceDelete, err := forceDeleteXFCCHeaderFunc(&http.Request{})
				if expectedErr != nil {
					Expect(err).To(Equal(expectedErr))
				} else {
					Expect(err).NotTo(HaveOccurred())
				}
				Expect(forceDelete).To(Equal(expectedValue))
			},
			Entry("arrivedViaRouteService returns (false, nil), forwardedClientCert == sanitize_set",
				notArrivedViaRouteService, "sanitize_set", false, nil),
			Entry("arrivedViaRouteService returns (false, nil), forwardedClientCert != sanitize_set",
				notArrivedViaRouteService, "", false, nil),
			Entry("arrivedViaRouteService returns (true, nil), forwardedClientCert == sanitize_set",
				arrivedViaRouteService, "sanitize_set", false, nil),
			Entry("arrivedViaRouteService returns (true, nil), forwardedClientCert != sanitize_set",
				arrivedViaRouteService, "", true, nil),
			Entry("arrivedViaRouteService returns (false, error), forwardedClientCert == sanitize_set",
				errorViaRouteService, "sanitize_set", false, errors.New("Bad route service validator")),
			Entry("arrivedViaRouteService returns (false, error), forwardedClientCert != sanitize_set",
				errorViaRouteService, "", false, errors.New("Bad route service validator")),
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

var notArrivedViaRouteServicesServer = func(*http.Request) bool {
	return false
}

var arrivedViaRouteServicesServer = func(*http.Request) bool {
	return true
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

func (h *hasBeenToRouteServiceValidatorFake) ArrivedViaRouteService(req *http.Request) (bool, error) {
	return h.ValidatedHasBeenToRouteServiceCall.Returns.Value, h.ValidatedHasBeenToRouteServiceCall.Returns.Error
}

func (h *hasBeenToRouteServiceValidatorFake) IsRouteServiceTraffic(req *http.Request) bool {
	return h.ValidatedIsRouteServiceTrafficCall.Returns.Value
}
