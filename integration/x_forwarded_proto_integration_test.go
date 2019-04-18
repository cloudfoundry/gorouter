package integration

import (
	"code.cloudfoundry.org/gorouter/routeservice"
	"fmt"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"net/http"
	"net/http/httptest"
	"net/url"
)

var _ = Describe("modifications of X-Forwarded-Proto header", func() {

	// testState ought to be re-usable for different high-level tests
	var testState *testState

	BeforeEach(func() {
		testState = NewTestState()
	})

	AfterEach(func() {
		if testState != nil {
			testState.StopAndCleanup()
		}
	})

	type gorouterConfig struct {
		forceForwardedProtoHTTPS bool
		sanitizeForwardedProto   bool
	}

	type testCase struct {
		clientRequestScheme      string
		clientRequestHeader      string
		expectBackendToSeeHeader string
	}

	type rsTestCase struct {
		clientRequestScheme string
		clientRequestHeader string
		expectBackendHeader string
		rsRequestScheme     string
		expectedRsHeader    string
	}

	//  | Force Forwarded Proto HTTPS  | Sanitze Forwarded Proto |
	//  |-----------------------------|-------------------------|
	testCases := map[gorouterConfig][]testCase{
		{false, false}: {
			//  | client scheme | client header| received  |
			//  |---------------|--------------|-----------|
			{"http", "", "http"},
			{"http", "http", "http"},
			{"http", "https", "https"},
			{"https", "", "https"},
			{"https", "http", "http"},
			{"https", "https", "https"},
		},

		{false, true}: {
			{"http", "", "http"},
			{"http", "http", "http"},
			{"http", "https", "http"},
			{"https", "", "https"},
			{"https", "http", "https"},
			{"https", "https", "https"},
		},

		{true, false}: {
			{"http", "", "https"},
			{"http", "http", "https"},
			{"http", "https", "https"},
			{"https", "", "https"},
			{"https", "http", "https"},
			{"https", "https", "https"},
		},

		{true, true}: {
			{"http", "", "https"},
			{"http", "http", "https"},
			{"http", "https", "https"},
			{"https", "", "https"},
			{"https", "http", "https"},
			{"https", "https", "https"},
		},
	}

	for gc, tcs := range testCases {
		goroutercfg := gc
		testCases := tcs

		It(fmt.Sprintf("gorouter config %+v: sets the headers correctly", goroutercfg), func() {
			testState.cfg.ForceForwardedProtoHttps = goroutercfg.forceForwardedProtoHTTPS
			testState.cfg.SanitizeForwardedProto = goroutercfg.sanitizeForwardedProto
			testState.StartGorouter()

			doRequest := func(testCase testCase, hostname string) {
				req := testState.newRequest(fmt.Sprintf("%s://%s", testCase.clientRequestScheme, hostname))
				req.Header.Set("X-Forwarded-Proto", testCase.clientRequestHeader)

				resp, err := testState.client.Do(req)
				Expect(err).NotTo(HaveOccurred())
				Expect(resp.StatusCode).To(Equal(200))
				resp.Body.Close()
			}

			for i, testCase := range testCases {
				By(fmt.Sprintf("case %d: %v", i, testCase))
				hostname := fmt.Sprintf("basic-app-%d.some.domain", i)

				receivedHeaders := make(chan http.Header, 1)
				testApp := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					receivedHeaders <- r.Header
					w.WriteHeader(200)
				}))
				defer testApp.Close()
				testState.register(testApp, hostname)

				doRequest(testCase, hostname)

				gotHeader := <-receivedHeaders
				Expect(gotHeader).To(HaveKeyWithValue("X-Forwarded-Proto", []string{testCase.expectBackendToSeeHeader}))
			}
		})
	}
	//  | Force Forwarded Proto HTTPS  | Sanitze Forwarded Proto |
	//  |-----------------------------|-------------------------|
	rsTestCases := map[gorouterConfig][]rsTestCase{
		{false, false}: {
			//  | client scheme   | client header| expected backend header | route service scheme | expected route service header |
			//  |-----------------|--------------|-------------------------|----------------------|-------------------------------|
			{"http", "", "http", "http", "http"},
			{"http", "", "http", "https", "http"},
			{"http", "http", "http", "http", "http"},
			{"http", "http", "http", "https", "http"},
			{"http", "https", "https", "http", "https"},
			{"http", "https", "https", "https", "https"},
			{"https", "", "https", "http", "https"},
			{"https", "", "https", "https", "https"},
			{"https", "http", "http", "http", "http"},
			{"https", "http", "http", "https", "http"},
			{"https", "https", "https", "http", "https"},
			{"https", "https", "https", "https", "https"},
		},

		{false, true}: {
			{"http", "", "http", "http", "http"},
			{"http", "", "http", "https", "http"},
			{"http", "http", "http", "http", "http"},
			{"http", "http", "http", "https", "http"},
			{"http", "https", "http", "http", "http"},
			{"http", "https", "http", "https", "http"},
			{"https", "", "https", "http", "https"},
			{"https", "", "https", "https", "https"},
			{"https", "http", "https", "http", "https"},
			{"https", "http", "https", "https", "https"},
			{"https", "https", "https", "http", "https"},
			{"https", "https", "https", "https", "https"},
		},

		{true, false}: {
			{"http", "", "https", "http", "https"},
			{"http", "", "https", "https", "https"},
			{"http", "http", "https", "http", "https"},
			{"http", "http", "https", "https", "https"},
			{"http", "https", "https", "http", "https"},
			{"http", "https", "https", "https", "https"},
			{"https", "", "https", "http", "https"},
			{"https", "", "https", "https", "https"},
			{"https", "http", "https", "http", "https"},
			{"https", "http", "https", "https", "https"},
			{"https", "https", "https", "http", "https"},
			{"https", "https", "https", "https", "https"},
		},

		{true, true}: {
			{"http", "", "https", "http", "https"},
			{"http", "", "https", "https", "https"},
			{"http", "http", "https", "http", "https"},
			{"http", "http", "https", "https", "https"},
			{"http", "https", "https", "http", "https"},
			{"http", "https", "https", "https", "https"},
			{"https", "", "https", "http", "https"},
			{"https", "", "https", "https", "https"},
			{"https", "http", "https", "http", "https"},
			{"https", "http", "https", "https", "https"},
			{"https", "https", "https", "http", "https"},
			{"https", "https", "https", "https", "https"},
		},
	}
	for gc, tcs := range rsTestCases {
		goroutercfg := gc
		rsInternalTestCases := tcs

		for i, testCase := range rsInternalTestCases {
			It(fmt.Sprintf("gorouter config: %+v sets the headers correctly\nclientRequestScheme: %s\nclientRequestHeader: %s\nexpectBackendHeader: %s\nrsRequestScheme: %s\nexpectedRsHeader: %s\n",
				goroutercfg,
				testCase.clientRequestScheme,
				testCase.clientRequestHeader,
				testCase.expectBackendHeader,
				testCase.rsRequestScheme,
				testCase.expectedRsHeader), func() {

				hostname := "basic-app.some.domain"
				testState.cfg.ForceForwardedProtoHttps = goroutercfg.forceForwardedProtoHTTPS
				testState.cfg.SanitizeForwardedProto = goroutercfg.sanitizeForwardedProto
				testState.StartGorouter()

				doRequest := func(testCase rsTestCase, hostname string) {
					req := testState.newRequest(fmt.Sprintf("%s://%s", testCase.clientRequestScheme, hostname))
					req.Header.Set("X-Forwarded-Proto", testCase.clientRequestHeader)
					resp, err := testState.client.Do(req)
					Expect(err).NotTo(HaveOccurred())
					Expect(resp.StatusCode).To(Equal(200))
					resp.Body.Close()
				}

				appReceivedHeaders := make(chan http.Header, 1)
				testApp := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					appReceivedHeaders <- r.Header
					w.WriteHeader(200)
				}))
				defer testApp.Close()
				testState.register(testApp, hostname)

				externalRsHeaders := make(chan http.Header, 1)
				externalRouteService := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					externalRsHeaders <- r.Header
					w.WriteHeader(200)
					url, err := url.Parse(r.Header.Get(routeservice.HeaderKeyForwardedURL))
					Expect(err).ToNot(HaveOccurred())
					newRequest := testState.newRequest(fmt.Sprintf("%s://%s", testCase.rsRequestScheme, url.Host))

					// routes service does not change headers
					for k, v := range r.Header {
						newRequest.Header[k] = v
					}
					resp, err := testState.client.Do(newRequest)
					Expect(err).NotTo(HaveOccurred())
					defer resp.Body.Close()
				}))

				externalRouteService.TLS = testState.trustedExternalServiceTLS
				externalRouteService.StartTLS()
				defer externalRouteService.Close()

				By("registering external route service")
				testState.registerWithExternalRouteService(testApp, externalRouteService, testState.trustedExternalServiceHostname, hostname)

				doRequest(testCase, hostname)

				var expectedBackendHeader http.Header
				Expect(appReceivedHeaders).To(Receive(&expectedBackendHeader))
				Expect(expectedBackendHeader).To(HaveKeyWithValue("X-Forwarded-Proto", []string{testCase.expectBackendHeader}))

				var expectedRsHeader http.Header
				Expect(externalRsHeaders).To(Receive(&expectedRsHeader))
				Expect(expectedRsHeader).To(HaveKeyWithValue("X-Forwarded-Proto", []string{testCase.expectedRsHeader}))

				By("registering internal route service")
				internalRsHeaders := make(chan http.Header, 1)
				internalRouteService := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					internalRsHeaders <- r.Header
					w.WriteHeader(200)
					url, err := url.Parse(r.Header.Get(routeservice.HeaderKeyForwardedURL))
					Expect(err).ToNot(HaveOccurred())
					newRequest := testState.newRequest(fmt.Sprintf("%s://%s", testCase.rsRequestScheme, url.Host))

					// route service does not change headers
					for k, v := range r.Header {
						newRequest.Header[k] = v
					}

					resp, err := testState.client.Do(newRequest)
					Expect(err).NotTo(HaveOccurred())
					defer resp.Body.Close()
				}))
				defer internalRouteService.Close()
				hostname = fmt.Sprintf("basic-app-%d-via-internal-route-service.some.domain", i)
				testState.registerWithInternalRouteService(testApp, internalRouteService, hostname, testState.cfg.SSLPort)
				doRequest(testCase, hostname)

				expectedBackendHeader = <-appReceivedHeaders
				Expect(expectedBackendHeader).To(HaveKeyWithValue("X-Forwarded-Proto", []string{testCase.expectBackendHeader}))

				expectedInternalRsHeader := <-internalRsHeaders
				Expect(expectedInternalRsHeader).To(HaveKeyWithValue("X-Forwarded-Proto", []string{testCase.expectedRsHeader}))
			})
		}
	}
})
