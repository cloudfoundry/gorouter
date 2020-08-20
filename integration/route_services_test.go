package integration

import (
	"fmt"
	"io/ioutil"
	"net/http"
	"net/http/httptest"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("Route services", func() {

	var testState *testState

	BeforeEach(func() {
		testState = NewTestState()

		testState.StartGorouterOrFail()
	})

	AfterEach(func() {
		if testState != nil {
			testState.StopAndCleanup()
		}
	})

	Context("Happy Path", func() {
		const (
			appHostname = "app-with-route-service.some.domain"
		)

		Context("When an app is registered with a simple route service", func() {
			var (
				testApp      *httptest.Server
				routeService *httptest.Server
			)
			BeforeEach(func() {
				testApp = httptest.NewServer(http.HandlerFunc(
					func(w http.ResponseWriter, r *http.Request) {
						w.WriteHeader(200)
						_, err := w.Write([]byte("I'm the app"))
						Expect(err).ToNot(HaveOccurred())
					}))

				routeService = httptest.NewServer(
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

				testState.registerWithInternalRouteService(
					testApp,
					routeService,
					appHostname,
					testState.cfg.SSLPort,
				)

			})

			AfterEach(func() {
				routeService.Close()
				testApp.Close()
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
})
