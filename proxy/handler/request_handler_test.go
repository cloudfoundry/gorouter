package handler_test

import (
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"time"

	"code.cloudfoundry.org/gorouter/errorwriter"
	metric "code.cloudfoundry.org/gorouter/metrics/fakes"
	"code.cloudfoundry.org/gorouter/proxy/handler"
	"code.cloudfoundry.org/gorouter/proxy/utils"
	iter "code.cloudfoundry.org/gorouter/route/fakes"
	"code.cloudfoundry.org/gorouter/test_util"

	. "github.com/onsi/ginkgo/v2"
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
				time.Second*2, time.Second*2, 3, &tls.Config{},
				nil,
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
				time.Second*2, time.Second*2, 3, &tls.Config{},
				nil,
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

	Context("when connection header has forbidden values", func() {
		var hopByHopHeadersToFilter []string
		BeforeEach(func() {
			hopByHopHeadersToFilter = []string{
				"X-Forwarded-For",
				"X-Forwarded-Proto",
				"B3",
				"X-B3",
				"X-B3-SpanID",
				"X-B3-TraceID",
				"X-Request-Start",
				"X-Forwarded-Client-Cert",
			}
		})
		Context("For a single Connection header", func() {
			BeforeEach(func() {
				req = &http.Request{
					RemoteAddr: "downtown-nino-brown",
					Host:       "gersh",
					URL: &url.URL{
						Path: "/foo",
					},
					Header: http.Header{},
				}
				values := []string{
					"Content-Type",
					"User-Agent",
					"X-Forwarded-Proto",
					"Accept",
					"X-B3-Spanid",
					"X-B3-Traceid",
					"B3",
					"X-Request-Start",
					"Cookie",
					"X-Cf-Applicationid",
					"X-Cf-Instanceid",
					"X-Cf-Instanceindex",
					"X-Vcap-Request-Id",
				}
				req.Header.Add("Connection", strings.Join(values, ", "))
				rh = handler.NewRequestHandler(
					req, pr,
					&metric.FakeProxyReporter{}, logger, ew,
					time.Second*2, time.Second*2, 3, &tls.Config{},
					hopByHopHeadersToFilter,
					handler.DisableSourceIPLogging(true),
				)
			})
			Describe("SanitizeRequestConnection", func() {
				It("Filters hop-by-hop headers", func() {
					rh.SanitizeRequestConnection()
					Expect(req.Header.Get("Connection")).To(Equal("Content-Type, User-Agent, Accept, Cookie, X-Cf-Applicationid, X-Cf-Instanceid, X-Cf-Instanceindex, X-Vcap-Request-Id"))
				})
			})
		})
		Context("For multiple Connection headers", func() {
			BeforeEach(func() {
				req = &http.Request{
					RemoteAddr: "downtown-nino-brown",
					Host:       "gersh",
					URL: &url.URL{
						Path: "/foo",
					},
					Header: http.Header{},
				}
				req.Header.Add("Connection", strings.Join([]string{
					"Content-Type",
					"X-B3-Spanid",
					"X-B3-Traceid",
					"X-Request-Start",
					"Cookie",
					"X-Cf-Instanceid",
					"X-Vcap-Request-Id",
				}, ", "))
				req.Header.Add("Connection", strings.Join([]string{
					"Content-Type",
					"User-Agent",
					"X-Forwarded-Proto",
					"Accept",
					"X-B3-Spanid",
					"X-Cf-Applicationid",
					"X-Cf-Instanceindex",
				}, ", "))
				rh = handler.NewRequestHandler(
					req, pr,
					&metric.FakeProxyReporter{}, logger, ew,
					time.Second*2, time.Second*2, 3, &tls.Config{},
					hopByHopHeadersToFilter,
					handler.DisableSourceIPLogging(true),
				)
			})
			Describe("SanitizeRequestConnection", func() {
				It("Filters hop-by-hop headers", func() {
					rh.SanitizeRequestConnection()
					headers := req.Header.Values("Connection")
					Expect(len(headers)).To(Equal(2))
					Expect(headers[0]).To(Equal("Content-Type, Cookie, X-Cf-Instanceid, X-Vcap-Request-Id"))
					Expect(headers[1]).To(Equal("Content-Type, User-Agent, Accept, X-Cf-Applicationid, X-Cf-Instanceindex"))
				})
			})
		})
	})
})
