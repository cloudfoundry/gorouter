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

		testState.StartGorouter()
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
						_, _ = w.Write([]byte("I'm the app"))
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
							defer res.Body.Close()
							Expect(err).ToNot(HaveOccurred())
							Expect(res.StatusCode).To(Equal(http.StatusOK))

							body, err := ioutil.ReadAll(res.Body)
							Expect(err).ToNot(HaveOccurred())
							Expect(body).To(Equal([]byte("I'm the app")))

							w.WriteHeader(res.StatusCode)
							_, _ = w.Write([]byte("I'm the route service"))
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
		})
	})
})
