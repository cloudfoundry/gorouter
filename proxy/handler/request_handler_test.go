package handler_test

import (
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"net/url"
	"time"

	"code.cloudfoundry.org/gorouter/errorwriter"
	metric "code.cloudfoundry.org/gorouter/metrics/fakes"
	"code.cloudfoundry.org/gorouter/proxy/handler"
	"code.cloudfoundry.org/gorouter/proxy/utils"
	iter "code.cloudfoundry.org/gorouter/route/fakes"
	"code.cloudfoundry.org/gorouter/test_util"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gbytes"
)

var _ = Describe("RequestHandler", func() {
	var (
		rh     *handler.RequestHandler
		logger *test_util.TestZapLogger
		ew     = errorwriter.NewPlaintextErrorWriter()
		req    *http.Request
		pr     utils.ProxyResponseWriter
	)

	BeforeEach(func() {
		logger = test_util.NewTestZapLogger("test")
		pr = utils.NewProxyResponseWriter(httptest.NewRecorder())
	})

	Context("when disableLogForwardedFor is set to true", func() {
		BeforeEach(func() {
			req = &http.Request{
				RemoteAddr: "downtown-nino-brown",
				Host:       "gersh",
				URL: &url.URL{
					Path: "/foo",
				},
				Header: http.Header{
					"X-Forwarded-For": []string{"1.1.1.1"},
				},
			}
			rh = handler.NewRequestHandler(
				req, pr,
				&metric.FakeProxyReporter{}, logger, ew,
				time.Second*2, &tls.Config{},
				handler.DisableXFFLogging(true),
			)
		})
		Describe("HandleBadGateway", func() {
			It("does not include the X-Forwarded-For header in log output", func() {
				rh.HandleBadGateway(nil, req)
				Eventually(logger.Buffer()).Should(gbytes.Say(`"X-Forwarded-For":"-"`))
			})
		})

		Describe("HandleTCPRequest", func() {
			It("does not include the X-Forwarded-For header in log output", func() {
				rh.HandleTcpRequest(&iter.FakeEndpointIterator{})
				Eventually(logger.Buffer()).Should(gbytes.Say(`"X-Forwarded-For":"-"`))
			})

			Context("when serveTcp returns an error", func() {
				It("does not include X-Forwarded-For in log output", func() {
					i := &iter.FakeEndpointIterator{}
					i.NextReturns(nil)
					rh.HandleTcpRequest(i)
					Eventually(logger.Buffer()).Should(gbytes.Say("tcp-request-failed"))
					Eventually(logger.Buffer()).Should(gbytes.Say(`"X-Forwarded-For":"-"`))
				})
			})
		})

		Describe("HandleTCPRequest", func() {
			It("does not include the X-Forwarded-For header in log output", func() {
				rh.HandleWebSocketRequest(&iter.FakeEndpointIterator{})
				Eventually(logger.Buffer()).Should(gbytes.Say(`"X-Forwarded-For":"-"`))
			})
		})
	})

	Context("when disableLogSourceIP is set to true", func() {
		BeforeEach(func() {
			req = &http.Request{
				RemoteAddr: "downtown-nino-brown",
				Host:       "gersh",
				URL: &url.URL{
					Path: "/foo",
				},
			}
			rh = handler.NewRequestHandler(
				req, pr,
				&metric.FakeProxyReporter{}, logger, ew,
				time.Second*2, &tls.Config{},
				handler.DisableSourceIPLogging(true),
			)
		})
		Describe("HandleBadGateway", func() {
			It("does not include the RemoteAddr header in log output", func() {
				rh.HandleBadGateway(nil, req)
				Eventually(logger.Buffer()).Should(gbytes.Say(`"RemoteAddr":"-"`))
			})
		})

		Describe("HandleTCPRequest", func() {
			It("does not include the RemoteAddr header in log output", func() {
				rh.HandleTcpRequest(&iter.FakeEndpointIterator{})
				Eventually(logger.Buffer()).Should(gbytes.Say(`"RemoteAddr":"-"`))
			})

			Context("when serveTcp returns an error", func() {
				It("does not include RemoteAddr in log output", func() {
					i := &iter.FakeEndpointIterator{}
					i.NextReturns(nil)
					rh.HandleTcpRequest(i)
					Eventually(logger.Buffer()).Should(gbytes.Say("tcp-request-failed"))
					Eventually(logger.Buffer()).Should(gbytes.Say(`"RemoteAddr":"-"`))
				})
			})
		})

		Describe("HandleTCPRequest", func() {
			It("does not include the RemoteAddr header in log output", func() {
				rh.HandleWebSocketRequest(&iter.FakeEndpointIterator{})
				Eventually(logger.Buffer()).Should(gbytes.Say(`"RemoteAddr":"-"`))
			})
		})
	})
})
