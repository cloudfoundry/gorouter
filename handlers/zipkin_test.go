package handlers_test

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"

	router_http "code.cloudfoundry.org/gorouter/common/http"
	"code.cloudfoundry.org/gorouter/config"
	"code.cloudfoundry.org/gorouter/handlers"
	"code.cloudfoundry.org/gorouter/proxy/utils"
	"code.cloudfoundry.org/gorouter/route"
	"code.cloudfoundry.org/gorouter/test_util"

	"code.cloudfoundry.org/gorouter/logger"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/ghttp"
	zipkinmodel "github.com/openzipkin/zipkin-go/model"
	"github.com/openzipkin/zipkin-go/propagation/b3"
)

// 64-bit random hexadecimal string
const (
	b3IDRegex      = `^[[:xdigit:]]{32}$`
	b3Regex        = `^[[:xdigit:]]{32}-[[:xdigit:]]{16}(-[01d](-[[:xdigit:]]{16})?)?$`
	b3TraceID      = "7f46165474d11ee5836777d85df2cdab"
	b3SpanID       = "54ebcb82b14862d9"
	b3SpanRegex    = `[[:xdigit:]]{16}$`
	b3ParentSpanID = "e56b75d6af463476"
	b3Single       = "1g56165474d11ee5836777d85df2cdab-32ebcb82b14862d9-1-ab6b75d6af463476"
)

var _ = Describe("Zipkin", func() {
	var (
		handler            *handlers.Zipkin
		logger             logger.Logger
		resp               http.ResponseWriter
		req                *http.Request
		nextCalled         bool
		zipkinServer       *ghttp.Server
		zipkinServerConfig config.ZipkinCollectorConfig
		receivedSpans      chan []zipkinmodel.SpanModel
	)

	nextHandler := http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		rw.WriteHeader(http.StatusBadGateway)
		rw.Header().Set(router_http.CfRouterError, "endpoint_failure")
		nextCalled = true
	})

	BeforeEach(func() {
		logger = test_util.NewTestZapLogger("zipkin")
		req = test_util.NewRequest("GET", "example.com", "/", nil)
		reqInfo := &handlers.RequestInfo{}
		reqInfo.RouteEndpoint = route.NewEndpoint(&route.EndpointOpts{
			AppId:                "some-app-id",
			Host:                 "some-host",
			Port:                 8080,
			PrivateInstanceId:    "some-instance-id",
			PrivateInstanceIndex: "some-app-idx",
		})
		req = req.WithContext(context.WithValue(req.Context(), handlers.RequestInfoCtxKey, reqInfo))
		resp = utils.NewProxyResponseWriter(httptest.NewRecorder())
		nextCalled = false
		zipkinServer = ghttp.NewUnstartedServer()
		zipkinServer.HTTPTestServer.TLS = &tls.Config{ClientAuth: tls.RequireAndVerifyClientCert}
		clientKey, clientCert := test_util.CreateKeyPair("client-cert")
		certPool := x509.NewCertPool()
		certPool.AppendCertsFromPEM(clientCert)

		zipkinServer.HTTPTestServer.TLS.ClientCAs = certPool
		zipkinServer.HTTPTestServer.StartTLS()
		receivedSpans = make(chan []zipkinmodel.SpanModel, 1)

		zipkinServer.AppendHandlers(ghttp.CombineHandlers(
			ghttp.VerifyRequest("POST", "/api/v2/spans"),
			func(w http.ResponseWriter, req *http.Request) {
				defer GinkgoRecover()
				b, err := io.ReadAll(req.Body)
				Expect(err).NotTo(HaveOccurred())
				var spans []zipkinmodel.SpanModel
				err = json.Unmarshal(b, &spans)
				Expect(err).NotTo(HaveOccurred())
				receivedSpans <- spans
			},
		))

		caPEM := new(bytes.Buffer)
		pem.Encode(caPEM, &pem.Block{
			Type:  "CERTIFICATE",
			Bytes: zipkinServer.HTTPTestServer.TLS.Certificates[0].Certificate[0],
		})

		zipkinServerConfig = config.ZipkinCollectorConfig{
			URL: fmt.Sprintf("%s%s", zipkinServer.URL(), "/api/v2/spans"),
			// URL:        "http://127.0.0.1:9411/api/v2/spans",
			ClientCert: string(clientCert),
			ClientKey:  string(clientKey),
			CACert:     caPEM.String(),
		}
	})

	AfterEach(func() {
	})

	Context("with Zipkin enabled", func() {
		BeforeEach(func() {
			handler = handlers.NewZipkin(true, zipkinServerConfig, logger)
		})

		It("sets zipkin headers", func() {
			handler.ServeHTTP(resp, req, nextHandler)
			Expect(req.Header.Get(b3.SpanID)).To(MatchRegexp(b3SpanRegex))
			Expect(req.Header.Get(b3.TraceID)).ToNot(BeEmpty())
			Expect(req.Header.Get(b3.ParentSpanID)).To(BeEmpty())
			Expect(req.Header.Get(b3.Context)).ToNot(BeEmpty())

			Expect(nextCalled).To(BeTrue(), "Expected the next handler to be called.")
		})

		It("sends a message to zipkin collector", func() {
			handler.ServeHTTP(resp, req, nextHandler)
			Expect(zipkinServer.ReceivedRequests()).To(HaveLen(1))
			spans := <-receivedSpans
			Expect(spans).To(HaveLen(1))
			Expect(spans[0].SpanContext.ID.String()).To(Equal(req.Header.Get(b3.SpanID)))
			Expect(spans[0].SpanContext.TraceID.String()).To(Equal(req.Header.Get(b3.TraceID)))
			Expect(spans[0].SpanContext.ParentID).To(BeNil())
			Expect(spans[0].Kind).To(Equal(zipkinmodel.Client))
			Expect(spans[0].Tags).To(Equal(map[string]string{
				"app_id":           "some-app-id",
				"app_index":        "some-app-idx",
				"instance_id":      "some-instance-id",
				"addr":             "some-host:8080",
				"status_code":      "502",
				"x_cf_routererror": "endpoint_failure",
			}))
		})

		Context("with B3TraceIdHeader, B3SpanIdHeader and B3ParentSpanIdHeader already set", func() {
			BeforeEach(func() {
				req.Header.Set(b3.TraceID, b3TraceID)
				req.Header.Set(b3.SpanID, b3SpanID)
				req.Header.Set(b3.ParentSpanID, b3ParentSpanID)
			})

			It("doesn't overwrite the B3ParentSpanIdHeader", func() {
				handler.ServeHTTP(resp, req, nextHandler)
				Expect(req.Header.Get(b3.ParentSpanID)).To(Equal(b3ParentSpanID))

				Expect(nextCalled).To(BeTrue(), "Expected the next handler to be called.")
			})

			It("doesn't overwrite the B3SpanIdHeader", func() {
				handler.ServeHTTP(resp, req, nextHandler)
				Expect(req.Header.Get(b3.SpanID)).To(Equal(b3SpanID))

				Expect(nextCalled).To(BeTrue(), "Expected the next handler to be called.")
			})

			It("doesn't overwrite the B3TraceIdHeader", func() {
				handler.ServeHTTP(resp, req, nextHandler)
				Expect(req.Header.Get(b3.TraceID)).To(Equal(b3TraceID))

				Expect(nextCalled).To(BeTrue(), "Expected the next handler to be called.")
			})

			It("sends a message to zipkin collector", func() {
				handler.ServeHTTP(resp, req, nextHandler)
				Expect(zipkinServer.ReceivedRequests()).To(HaveLen(1))
				spans := <-receivedSpans
				Expect(spans).To(HaveLen(1))
				Expect(spans[0].SpanContext.ID.String()).To(Equal(b3SpanID))
				Expect(spans[0].SpanContext.TraceID.String()).To(Equal(b3TraceID))
				Expect(spans[0].SpanContext.ParentID.String()).To(Equal(b3ParentSpanID))
			})
		})

		Context("with B3TraceIdHeader and B3SpanIdHeader already set", func() {
			BeforeEach(func() {
				req.Header.Set(b3.TraceID, b3TraceID)
				req.Header.Set(b3.SpanID, b3SpanID)
			})

			It("propagates the B3Header", func() {
				handler.ServeHTTP(resp, req, nextHandler)

				Expect(req.Header.Get(b3.Context)).To(Equal(b3TraceID + "-" + b3SpanID))
				Expect(nextCalled).To(BeTrue(), "Expected the next handler to be called.")

				Expect(zipkinServer.ReceivedRequests()).To(HaveLen(1))
				spans := <-receivedSpans
				Expect(spans).To(HaveLen(1))
				Expect(spans[0].SpanContext.ID.String()).To(Equal(req.Header.Get(b3.SpanID)))
				Expect(spans[0].SpanContext.TraceID.String()).To(Equal(req.Header.Get(b3.TraceID)))
				Expect(spans[0].SpanContext.ParentID).To(BeNil())
			})

			It("propagates the B3Header with Sampled header", func() {
				req.Header.Set(b3.Sampled, "true")

				handler.ServeHTTP(resp, req, nextHandler)

				Expect(req.Header.Get(b3.Context)).To(Equal(b3TraceID + "-" + b3SpanID + "-1"))
				Expect(nextCalled).To(BeTrue(), "Expected the next handler to be called.")
				// Sampled is not reported
				Expect(zipkinServer.ReceivedRequests()).To(HaveLen(1))
				spans := <-receivedSpans
				Expect(spans).To(HaveLen(1))
				Expect(spans[0].SpanContext.Sampled).To(BeNil())
				Expect(spans[0].SpanContext.Debug).To(BeFalse())
			})

			It("propagates the B3Header with Flags header", func() {
				req.Header.Set(b3.Flags, "1")
				req.Header.Set(b3.Sampled, "false")

				handler.ServeHTTP(resp, req, nextHandler)

				Expect(req.Header.Get(b3.Context)).To(Equal(b3TraceID + "-" + b3SpanID + "-d"))
				Expect(nextCalled).To(BeTrue(), "Expected the next handler to be called.")
				Expect(zipkinServer.ReceivedRequests()).To(HaveLen(1))
				spans := <-receivedSpans
				Expect(spans).To(HaveLen(1))
				// Sampled is not reported
				Expect(spans[0].SpanContext.Sampled).To(BeNil())
				Expect(spans[0].SpanContext.Debug).To(BeTrue())
			})

			It("propagates the B3Header with ParentSpanID header", func() {
				req.Header.Set(b3.Sampled, "false")
				req.Header.Set(b3.ParentSpanID, b3ParentSpanID)

				handler.ServeHTTP(resp, req, nextHandler)

				Expect(req.Header.Get(b3.Context)).To(Equal(b3TraceID + "-" + b3SpanID + "-0-" + b3ParentSpanID))
				Expect(nextCalled).To(BeTrue(), "Expected the next handler to be called.")

				Expect(zipkinServer.ReceivedRequests()).To(HaveLen(1))
				spans := <-receivedSpans
				Expect(spans).To(HaveLen(1))
				// Sampled is not reported
				Expect(spans[0].SpanContext.Sampled).To(BeNil())
				Expect(spans[0].SpanContext.ParentID.String()).To(Equal(b3ParentSpanID))
			})

			It("doesn't overwrite the B3SpanIdHeader", func() {
				handler.ServeHTTP(resp, req, nextHandler)
				Expect(req.Header.Get(b3.SpanID)).To(Equal(b3SpanID))
				Expect(req.Header.Get(b3.Context)).To(Equal(b3TraceID + "-" + b3SpanID))
				Expect(req.Header.Get(b3.ParentSpanID)).To(BeEmpty())

				Expect(nextCalled).To(BeTrue(), "Expected the next handler to be called.")

				Expect(zipkinServer.ReceivedRequests()).To(HaveLen(1))
				spans := <-receivedSpans
				Expect(spans).To(HaveLen(1))
				Expect(spans[0].SpanContext.ID.String()).To(Equal(b3SpanID))
				Expect(spans[0].SpanContext.ParentID).To(BeNil())
			})

			It("doesn't overwrite the B3TraceIdHeader", func() {
				handler.ServeHTTP(resp, req, nextHandler)
				Expect(req.Header.Get(b3.TraceID)).To(Equal(b3TraceID))

				Expect(nextCalled).To(BeTrue(), "Expected the next handler to be called.")

				Expect(zipkinServer.ReceivedRequests()).To(HaveLen(1))
				spans := <-receivedSpans
				Expect(spans).To(HaveLen(1))
				Expect(spans[0].SpanContext.TraceID.String()).To(Equal(b3TraceID))
			})
		})

		Context("with only B3SpanIdHeader set", func() {
			BeforeEach(func() {
				req.Header.Set(b3.SpanID, b3SpanID)
			})

			It("adds the B3TraceIdHeader and overwrites the SpanId", func() {
				handler.ServeHTTP(resp, req, nextHandler)
				Expect(req.Header.Get(b3.TraceID)).To(MatchRegexp(b3IDRegex))
				Expect(req.Header.Get(b3.SpanID)).To(MatchRegexp(b3SpanRegex))
				Expect(req.Header.Get(b3.SpanID)).NotTo(Equal(b3SpanID))
				Expect(req.Header.Get(b3.ParentSpanID)).To(BeEmpty())
				Expect(req.Header.Get(b3.Context)).To(MatchRegexp(b3Regex))

				Expect(nextCalled).To(BeTrue(), "Expected the next handler to be called.")

				Expect(zipkinServer.ReceivedRequests()).To(HaveLen(1))
				spans := <-receivedSpans
				Expect(spans).To(HaveLen(1))
				Expect(spans[0].SpanContext.TraceID.String()).To(MatchRegexp(b3IDRegex))
				Expect(spans[0].SpanContext.ID.String()).To(MatchRegexp(b3SpanRegex))
				Expect(spans[0].SpanContext.ID.String()).NotTo(Equal(b3SpanID))
				Expect(spans[0].SpanContext.ParentID).To(BeNil())
			})
		})

		Context("with only B3TraceIdHeader set", func() {
			BeforeEach(func() {
				req.Header.Set(b3.TraceID, b3TraceID)
			})

			It("overwrites the B3TraceIdHeader and adds a SpanId", func() {
				handler.ServeHTTP(resp, req, nextHandler)
				Expect(req.Header.Get(b3.TraceID)).To(MatchRegexp(b3IDRegex))
				Expect(req.Header.Get(b3.TraceID)).NotTo(Equal(b3TraceID))
				Expect(req.Header.Get(b3.SpanID)).To(MatchRegexp(b3SpanRegex))
				Expect(req.Header.Get(b3.ParentSpanID)).To(BeEmpty())
				Expect(req.Header.Get(b3.Context)).To(MatchRegexp(b3Regex))

				Expect(nextCalled).To(BeTrue(), "Expected the next handler to be called.")
			})
		})

		Context("with B3Header already set", func() {
			BeforeEach(func() {
				req.Header.Set(b3.Context, b3Single)
			})

			It("doesn't overwrite the B3Header", func() {
				handler.ServeHTTP(resp, req, nextHandler)
				Expect(req.Header.Get(b3.Context)).To(Equal(b3Single))
				Expect(req.Header.Get(b3.TraceID)).To(BeEmpty())
				Expect(req.Header.Get(b3.SpanID)).To(BeEmpty())
				Expect(req.Header.Get(b3.ParentSpanID)).To(BeEmpty())

				Expect(nextCalled).To(BeTrue(), "Expected the next handler to be called.")
			})
		})
	})

	Context("with Zipkin disabled", func() {
		BeforeEach(func() {
			handler = handlers.NewZipkin(false, config.ZipkinCollectorConfig{}, logger)
		})

		It("doesn't set any headers", func() {
			handler.ServeHTTP(resp, req, nextHandler)
			Expect(req.Header.Get(b3.SpanID)).To(BeEmpty())
			Expect(req.Header.Get(b3.TraceID)).To(BeEmpty())
			Expect(req.Header.Get(b3.ParentSpanID)).To(BeEmpty())
			Expect(req.Header.Get(b3.Context)).To(BeEmpty())

			Expect(nextCalled).To(BeTrue(), "Expected the next handler to be called.")
		})
	})
})
