package proxy_test

import (
	"bytes"
	"crypto/tls"
	"errors"
	"net/http"
	"net/http/httptest"
	"time"

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
		proxyObj         http.Handler
		fakeAccessLogger *fakelogger.FakeAccessLogger
		logger           logger.Logger
		resp             utils.ProxyResponseWriter
		combinedReporter metrics.ProxyReporter
	)

	Describe("ServeHTTP", func() {
		BeforeEach(func() {
			tlsConfig := &tls.Config{
				CipherSuites:       conf.CipherSuites,
				InsecureSkipVerify: conf.SkipSSLValidation,
			}

			fakeAccessLogger = &fakelogger.FakeAccessLogger{}

			logger = test_util.NewTestZapLogger("test")
			r = registry.NewRouteRegistry(logger, conf, new(fakes.FakeRouteRegistryReporter))

			routeServiceConfig := routeservice.NewRouteServiceConfig(
				logger,
				conf.RouteServiceEnabled,
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

			rt := &sharedfakes.RoundTripper{}
			conf.HealthCheckUserAgent = "HTTP-Monitor/1.1"

			skipSanitization = func(req *http.Request) bool { return false }
			proxyObj = proxy.NewProxy(logger, fakeAccessLogger, conf, r, combinedReporter,
				routeServiceConfig, tlsConfig, nil, rt, skipSanitization)

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
	})

	Describe("SkipSanitizeXFP", func() {
		DescribeTable("the returned function",
			func(arrivedViaRouteService func(*http.Request) bool, viaRouteService proxy.RouteServiceValidator, expectedValue bool, expectedErr error) {
				skipSanitizeRouteService := proxy.SkipSanitizeXFP(arrivedViaRouteService, viaRouteService)
				skip, err := skipSanitizeRouteService(&http.Request{})
				if expectedErr != nil {
					Expect(err).To(Equal(expectedErr))
				} else {
					Expect(err).NotTo(HaveOccurred())
				}
				Expect(skip).To(Equal(expectedValue))
			},
			Entry("arrivedViaRouteService returns (true, nil)",
				arrivedViaRouteServicesServer, arrivedViaRouteService, true, nil),
			Entry("arrivedViaRouteService returns (true, nil)",
				notArrivedViaRouteServicesServer, arrivedViaRouteService, true, nil),
			Entry("arrivedViaRouteService returns (false, nil)",
				arrivedViaRouteServicesServer, notArrivedViaRouteService, true, nil),
			Entry("arrivedViaRouteService returns (false, nil)",
				notArrivedViaRouteServicesServer, notArrivedViaRouteService, false, nil),
			Entry("arrivedViaRouteService returns (false, error)",
				arrivedViaRouteServicesServer, errorViaRouteService, false, errors.New("Bad route service validator")),
		)
	})

	Describe("SkipSanitize", func() {
		DescribeTable("the returned function",
			func(arrivedViaRouteServicesServer func(*http.Request) bool, viaRouteService proxy.RouteServiceValidator, reqTLS *tls.ConnectionState, expectedValue bool, expectedErr error) {
				skipSanitizationFunc := proxy.SkipSanitize(arrivedViaRouteServicesServer, viaRouteService)
				skipSanitization, err := skipSanitizationFunc(&http.Request{TLS: reqTLS})
				if expectedErr != nil {
					Expect(err).To(Equal(expectedErr))
				} else {
					Expect(err).NotTo(HaveOccurred())
				}
				Expect(skipSanitization).To(Equal(expectedValue))
			},
			Entry("arrivedViaRouteServicesServer returns false, arrivedViaRouteService returns (false, nil), req.TLS == nil",
				notArrivedViaRouteServicesServer, notArrivedViaRouteService, nil, false, nil),
			Entry("arrivedViaRouteServicesServer returns false, arrivedViaRouteService returns (false, nil), req.TLS != nil",
				notArrivedViaRouteServicesServer, notArrivedViaRouteService, &tls.ConnectionState{}, false, nil),
			Entry("arrivedViaRouteServicesServer returns true, arrivedViaRouteService returns (false, nil), req.TLS == nil",
				arrivedViaRouteServicesServer, notArrivedViaRouteService, nil, true, nil),
			Entry("arrivedViaRouteServicesServer returns true, arrivedViaRouteService returns (false, nil), req.TLS != nil",
				arrivedViaRouteServicesServer, notArrivedViaRouteService, &tls.ConnectionState{}, true, nil),
			Entry("arrivedViaRouteServicesServer returns false, arrivedViaRouteService returns (true, nil), req.TLS == nil",
				notArrivedViaRouteServicesServer, arrivedViaRouteService, nil, false, nil),
			Entry("arrivedViaRouteServicesServer returns false, arrivedViaRouteService returns (true, nil), req.TLS != nil",
				notArrivedViaRouteServicesServer, arrivedViaRouteService, &tls.ConnectionState{}, true, nil),
			Entry("arrivedViaRouteServicesServer returns true, arrivedViaRouteService returns (true, nil), req.TLS == nil",
				arrivedViaRouteServicesServer, arrivedViaRouteService, nil, true, nil),
			Entry("arrivedViaRouteServicesServer returns true, arrivedViaRouteService returns (true, nil), req.TLS != nil",
				arrivedViaRouteServicesServer, arrivedViaRouteService, &tls.ConnectionState{}, true, nil),
			Entry("arrivedViaRouteServicesServer returns false, arrivedViaRouteService returns (false, error), req.TLS == nil",
				notArrivedViaRouteServicesServer, errorViaRouteService, nil, false, errors.New("Bad route service validator")),
			Entry("arrivedViaRouteServicesServer returns false, arrivedViaRouteService returns (false, error), req.TLS != nil",
				notArrivedViaRouteServicesServer, errorViaRouteService, &tls.ConnectionState{}, false, errors.New("Bad route service validator")),
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
