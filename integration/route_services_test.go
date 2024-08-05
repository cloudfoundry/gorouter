package integration

import (
	"crypto/tls"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"time"

	"code.cloudfoundry.org/gorouter/route"
	"code.cloudfoundry.org/gorouter/test/common"
	. "github.com/onsi/ginkgo/v2"
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
				w.Header().Add("X-App-Instance", "app1")
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

					body, err := io.ReadAll(res.Body)
					Expect(err).ToNot(HaveOccurred())
					Expect(body).To(Equal([]byte("I'm the app")))

					w.Header().Add("X-App-Instance", res.Header.Get("X-App-Instance"))
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

	routeSvcUrl := func(routeService *httptest.Server) string {
		port := strings.Split(routeService.Listener.Addr().String(), ":")[1]
		return fmt.Sprintf("https://%s:%s", testState.trustedExternalServiceHostname, port)
	}

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
				body, err := io.ReadAll(res.Body)
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
				body, err := io.ReadAll(res.Body)
				Expect(err).ToNot(HaveOccurred())
				Expect(body).To(Equal([]byte("I'm the route service")))
			})
		})
	})

	Context("When an route with a route service has a stale endpoint", func() {
		var (
			tlsTestApp1, tlsTestApp2 *common.TestApp
			tlsTestAppID             string
		)

		tlsTestAppID = "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"
		setupAppInstance := func(index int) *common.TestApp {
			app := common.NewTestApp(
				[]route.Uri{appHostname},
				testState.cfg.Port,
				testState.mbusClient,
				nil,
				routeSvcUrl(routeService),
			)

			app.AddHandler("/", func(w http.ResponseWriter, r *http.Request) {
				w.Header().Add("X-App-Instance", fmt.Sprintf("app%d", index+1))
				w.WriteHeader(http.StatusOK)
				_, err := w.Write([]byte("I'm the app"))
				Expect(err).NotTo(HaveOccurred())
			})

			app.GUID = tlsTestAppID
			app.TlsRegisterWithIndex(testState.trustedBackendServerCertSAN, index)
			errChan := app.TlsListen(testState.trustedBackendTLSConfig.Clone())
			Consistently(errChan).ShouldNot(Receive())

			return app
		}

		BeforeEach(func() {
			testState.StartGorouterOrFail()
			routeService.TLS = testState.trustedExternalServiceTLS
			routeService.StartTLS()

			tlsTestApp1 = setupAppInstance(0)
			tlsTestApp2 = setupAppInstance(1)

			// Verify we get app1 if we request it while it's running
			req := testState.newRequest(
				fmt.Sprintf("https://%s", appHostname),
			)
			Eventually(func(g Gomega) {
				res, err := testState.client.Do(req)
				g.Expect(err).ToNot(HaveOccurred())
				g.Expect(res.StatusCode).To(Equal(http.StatusOK))
				g.Expect(res.Header.Get("X-App-Instance")).To(Equal("app1"))
			}).Should(Succeed())
			tlsTestApp1.Stop()
		})

		AfterEach(func() {
			tlsTestApp1.Unregister()
			tlsTestApp2.Unregister()
		})

		It("prunes the stale endpoint", func() {
			req := testState.newRequest(
				fmt.Sprintf("https://%s", appHostname),
			)
			time.Sleep(100 * time.Millisecond)
			Consistently(func(g Gomega) {
				res, err := testState.client.Do(req)
				g.Expect(err).ToNot(HaveOccurred())
				g.Expect(res.StatusCode).To(Equal(http.StatusOK))
				g.Expect(res.Header.Get("X-App-Instance")).To(Equal("app2"))
			}).Should(Succeed())
		})
		Context("when the route service on the stale route was out of date", func() {
			var routeService2 *httptest.Server
			BeforeEach(func() {
				routeService2 = httptest.NewUnstartedServer(
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

							body, err := io.ReadAll(res.Body)
							Expect(err).ToNot(HaveOccurred())
							Expect(body).To(Equal([]byte("I'm the app")))

							w.Header().Add("X-App-Instance", res.Header.Get("X-App-Instance"))
							w.WriteHeader(res.StatusCode)
							_, err = w.Write([]byte("I'm the route service"))
							Expect(err).ToNot(HaveOccurred())
						}))
				routeService2.TLS = testState.trustedExternalServiceTLS
				routeService2.StartTLS()
				tlsTestApp2.SetRouteService(routeSvcUrl(routeService2))
				tlsTestApp2.TlsRegisterWithIndex(testState.trustedBackendServerCertSAN, 1)
				routeService.Close()
			})
			AfterEach(func() {
				routeService2.Close()
			})

			It("still prunes the stale endpoint", func() {
				req := testState.newRequest(
					fmt.Sprintf("https://%s", appHostname),
				)
				time.Sleep(100 * time.Millisecond)

				Consistently(func(g Gomega) {
					res, err := testState.client.Do(req)
					g.Expect(err).ToNot(HaveOccurred())
					g.Expect(res.StatusCode).To(Equal(http.StatusOK))
					g.Expect(res.Header.Get("X-App-Instance")).To(Equal("app2"))
				}).Should(Succeed())
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
				body, err := io.ReadAll(res.Body)
				Expect(err).ToNot(HaveOccurred())
				Expect(body).To(Equal([]byte("I'm the route service")))
			})
		})
	})

	Context("when the route service has a MaxVersion of TLS 1.1", func() {
		BeforeEach(func() {
			routeService.TLS = testState.trustedExternalServiceTLS
			routeService.TLS.CipherSuites = []uint16{tls.TLS_ECDHE_RSA_WITH_AES_256_CBC_SHA}
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
				testState.cfg.CipherString = "TLS_ECDHE_RSA_WITH_AES_256_CBC_SHA"
				testState.StartGorouterOrFail()
			})

			It("succeeds", func() {
				req := testState.newRequest(
					fmt.Sprintf("https://%s", appHostname),
				)

				res, err := testState.client.Do(req)
				Expect(err).ToNot(HaveOccurred())
				Expect(res.StatusCode).To(Equal(http.StatusOK))
				body, err := io.ReadAll(res.Body)
				Expect(err).ToNot(HaveOccurred())
				Expect(body).To(Equal([]byte("I'm the route service")))
			})
		})
	})
})
