package integration

import (
	"crypto/tls"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/http/httptest"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("Route services", func() {

	var testState *testState

	const (
		appHostname = "app-with-route-service.some.domain"
	)

	var (
		testApp      *httptest.Server
		routeService *httptest.Server
	)

	BeforeEach(func() {
		testState = NewTestState()
		testApp = httptest.NewServer(http.HandlerFunc(
			func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(200)
				_, err := w.Write([]byte("I'm the app"))
				Expect(err).ToNot(HaveOccurred())
			}))

		routeService = httptest.NewUnstartedServer(
			http.HandlerFunc(
				func(w http.ResponseWriter, r *http.Request) {
					defer GinkgoRecover()

					forwardedURL := r.Header.Get("X-CF-Forwarded-Url")
					sigHeader := r.Header.Get("X-Cf-Proxy-Signature")
					metadata := r.Header.Get("X-Cf-Proxy-Metadata")

					req := testState.newRequest(forwardedURL)

					req.Header.Add("X-CF-Forwarded-Url", forwardedURL)
					req.Header.Add("X-Cf-Proxy-Metadata", metadata)
					req.Header.Add("X-Cf-Proxy-Signature", sigHeader)

					res, err := testState.routeServiceClient.Do(req)
					Expect(err).ToNot(HaveOccurred())
					defer res.Body.Close()
					Expect(res.StatusCode).To(Equal(http.StatusOK))

					body, err := ioutil.ReadAll(res.Body)
					Expect(err).ToNot(HaveOccurred())
					Expect(body).To(Equal([]byte("I'm the app")))

					w.WriteHeader(res.StatusCode)
					_, err = w.Write([]byte("I'm the route service"))
					Expect(err).ToNot(HaveOccurred())
				}))

	})

	AfterEach(func() {
		if testState != nil {
			testState.StopAndCleanup()
		}
		routeService.Close()
		testApp.Close()
	})

	Context("Happy Path", func() {
		Context("When an app is registered with a simple route service", func() {
			BeforeEach(func() {
				testState.StartGorouterOrFail()
				routeService.Start()

				testState.registerWithInternalRouteService(
					testApp,
					routeService,
					appHostname,
					testState.cfg.SSLPort,
				)
			})

			It("succeeds", func() {
				req := testState.newRequest(
					fmt.Sprintf("https://%s", appHostname),
				)

				res, err := testState.client.Do(req)
				Expect(err).ToNot(HaveOccurred())
				Expect(res.StatusCode).To(Equal(http.StatusOK))
				body, err := ioutil.ReadAll(res.Body)
				Expect(err).ToNot(HaveOccurred())
				Expect(body).To(Equal([]byte("I'm the route service")))
			})

			It("properly URL-encodes and decodes", func() {
				req := testState.newRequest(
					fmt.Sprintf("https://%s?%s", appHostname, "param=a%0Ab"),
				)

				res, err := testState.client.Do(req)
				Expect(err).ToNot(HaveOccurred())
				Expect(res.StatusCode).To(Equal(http.StatusOK))
				body, err := ioutil.ReadAll(res.Body)
				Expect(err).ToNot(HaveOccurred())
				Expect(body).To(Equal([]byte("I'm the route service")))
			})
		})
	})

	Context("when the route service only uses TLS 1.3", func() {
		BeforeEach(func() {
			routeService.TLS = testState.trustedExternalServiceTLS
			routeService.TLS.MaxVersion = tls.VersionTLS13
			routeService.TLS.MinVersion = tls.VersionTLS13
			routeService.StartTLS()
		})

		JustBeforeEach(func() {
			testState.registerWithExternalRouteService(
				testApp,
				routeService,
				testState.trustedExternalServiceHostname,
				appHostname,
			)
		})

		Context("when the client has MaxVersion of TLS 1.2", func() {
			BeforeEach(func() {
				testState.cfg.MaxTLSVersionString = "TLSv1.2"
				testState.cfg.MinTLSVersionString = "TLSv1.2"
				testState.StartGorouterOrFail()
			})

			It("fails with a 502", func() {
				req := testState.newRequest(
					fmt.Sprintf("https://%s", appHostname),
				)

				res, err := testState.client.Do(req)
				Expect(err).ToNot(HaveOccurred())
				Expect(res.StatusCode).To(Equal(502))
				Expect(res.Header.Get("X-Cf-RouterError")).To(ContainSubstring("protocol version not supported"))
			})
		})

		Context("when the client has MaxVersion of TLS 1.3", func() {
			BeforeEach(func() {
				testState.cfg.MaxTLSVersionString = "TLSv1.3"
				testState.StartGorouterOrFail()
			})

			It("succeeds", func() {
				req := testState.newRequest(
					fmt.Sprintf("https://%s", appHostname),
				)

				res, err := testState.client.Do(req)
				Expect(err).ToNot(HaveOccurred())
				Expect(res.StatusCode).To(Equal(http.StatusOK))
				body, err := ioutil.ReadAll(res.Body)
				Expect(err).ToNot(HaveOccurred())
				Expect(body).To(Equal([]byte("I'm the route service")))
			})
		})
	})

	Context("when the route service has a MaxVersion of TLS 1.1", func() {
		BeforeEach(func() {
			routeService.TLS = testState.trustedExternalServiceTLS
			routeService.TLS.CipherSuites = []uint16{tls.TLS_RSA_WITH_AES_128_CBC_SHA}
			routeService.TLS.MaxVersion = tls.VersionTLS11
			routeService.TLS.MinVersion = tls.VersionTLS11
			routeService.StartTLS()
		})

		JustBeforeEach(func() {
			testState.registerWithExternalRouteService(
				testApp,
				routeService,
				testState.trustedExternalServiceHostname,
				appHostname,
			)
		})

		Context("when the client has MinVersion of TLS 1.2", func() {
			BeforeEach(func() {
				testState.cfg.MinTLSVersionString = "TLSv1.2"
				testState.cfg.MaxTLSVersionString = "TLSv1.2"
				testState.StartGorouterOrFail()
			})

			It("fails with a 502", func() {
				req := testState.newRequest(
					fmt.Sprintf("https://%s", appHostname),
				)

				res, err := testState.client.Do(req)
				Expect(err).ToNot(HaveOccurred())
				Expect(res.StatusCode).To(Equal(502))
				Expect(res.Header.Get("X-Cf-RouterError")).To(ContainSubstring("protocol version not supported"))
			})
		})

		Context("when the client has MinVersion of TLS 1.1", func() {
			BeforeEach(func() {
				testState.cfg.MinTLSVersionString = "TLSv1.1"
				testState.cfg.CipherString = "TLS_RSA_WITH_AES_128_CBC_SHA"
				testState.StartGorouterOrFail()
			})

			It("succeeds", func() {
				req := testState.newRequest(
					fmt.Sprintf("https://%s", appHostname),
				)

				res, err := testState.client.Do(req)
				Expect(err).ToNot(HaveOccurred())
				Expect(res.StatusCode).To(Equal(http.StatusOK))
				body, err := ioutil.ReadAll(res.Body)
				Expect(err).ToNot(HaveOccurred())
				Expect(body).To(Equal([]byte("I'm the route service")))
			})
		})
	})
})
