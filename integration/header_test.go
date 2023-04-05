package integration

import (
	"fmt"
	"io/ioutil"
	"net/http"
	"net/http/httptest"

	"code.cloudfoundry.org/gorouter/config"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Headers", func() {
	var (
		testState *testState

		testAppRoute string
		testApp      *StateTrackingTestApp
	)

	const (
		testHeader      = "Test-Header"
		testHeaderValue = "Value"
	)

	BeforeEach(func() {
		testState = NewTestState()
		testApp = NewUnstartedTestApp(http.HandlerFunc(
			func(w http.ResponseWriter, r *http.Request) {
				defer GinkgoRecover()
				_, err := ioutil.ReadAll(r.Body)
				Expect(err).NotTo(HaveOccurred())
				w.Header().Set(testHeader, testHeaderValue)
				w.WriteHeader(200)
			}))
		testAppRoute = "potato.potato"
	})

	AfterEach(func() {
		if testState != nil {
			testState.StopAndCleanup()
		}
		testApp.Close()
	})

	Context("Sanity Test", func() {
		BeforeEach(func() {
			testState.StartGorouterOrFail()
			testApp.Start()
			testState.register(testApp.Server, testAppRoute)
		})

		It("returns a header that was set by the app", func() {
			req := testState.newRequest(fmt.Sprintf("http://%s", testAppRoute))
			resp, err := testState.client.Do(req)
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.StatusCode).To(Equal(200))
			_, err = ioutil.ReadAll(resp.Body)
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.Header.Get(testHeader)).To(Equal(testHeaderValue))
			resp.Body.Close()
		})
	})

	Context("Remove Headers", func() {
		BeforeEach(func() {
			testState.cfg.HTTPRewrite.Responses.RemoveHeaders =
				[]config.HeaderNameValue{
					{
						Name: testHeader,
					},
				}

			testState.StartGorouterOrFail()
			testApp.Start()
			testState.register(testApp.Server, testAppRoute)
		})

		It("removes the header specified in the config", func() {
			req := testState.newRequest(fmt.Sprintf("http://%s", testAppRoute))
			resp, err := testState.client.Do(req)
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.StatusCode).To(Equal(200))
			_, err = ioutil.ReadAll(resp.Body)
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.Header.Get(testHeader)).To(BeEmpty())
			resp.Body.Close()
		})
	})

	Context("Add Headers", func() {
		const (
			newHeader      = "New-Header"
			newHeaderValue = "newValue"
		)

		BeforeEach(func() {
			testState.cfg.HTTPRewrite.Responses.AddHeadersIfNotPresent =
				[]config.HeaderNameValue{
					{
						Name:  newHeader,
						Value: newHeaderValue,
					},
				}

			testState.StartGorouterOrFail()
			testApp.Start()
			testState.register(testApp.Server, testAppRoute)
		})

		It("adds the header specified in the config", func() {
			req := testState.newRequest(fmt.Sprintf("http://%s", testAppRoute))
			resp, err := testState.client.Do(req)
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.StatusCode).To(Equal(200))
			_, err = ioutil.ReadAll(resp.Body)
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.Header.Get(newHeader)).To(Equal(newHeaderValue))
			resp.Body.Close()
		})
	})

	Context("Route Service Headers", func() {
		const (
			HeaderKeySignature    = "X-CF-Proxy-Signature"
			HeaderKeyForwardedURL = "X-CF-Forwarded-Url"
			HeaderKeyMetadata     = "X-CF-Proxy-Metadata"
		)

		BeforeEach(func() {

			testState.StartGorouterOrFail()
			testApp.Start()
			testState.register(testApp.Server, testAppRoute)
		})

		It("strips the sensitive headers from the route service response", func() {
			appHostname := "app-with-route-service.some.domain"

			testApp := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				Fail("The app should never be hit since the route service call will fail")
			}))
			defer testApp.Close()

			routeService := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set(HeaderKeySignature, "This value should NOT be leaked")
				w.Header().Set(HeaderKeyForwardedURL, "Some URL that may be leaked")
				w.Header().Set(HeaderKeyMetadata, "Some metadata that may be leaked")
				w.WriteHeader(400)
			}))
			defer routeService.Close()

			testState.registerWithInternalRouteService(
				testApp,
				routeService,
				appHostname,
				testState.cfg.SSLPort,
			)

			testState.client.Transport.(*http.Transport).TLSClientConfig.Certificates = testState.trustedClientTLSConfig.Certificates
			req := testState.newRequest(fmt.Sprintf("https://%s", appHostname))
			resp, err := testState.client.Do(req)
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.StatusCode).To(Equal(400))
			Expect(resp.Header.Get(HeaderKeySignature)).To(BeEmpty())
		})
	})
})
