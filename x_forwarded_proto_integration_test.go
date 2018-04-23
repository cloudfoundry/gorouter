package main_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
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
		clientRequestScheme string
		clientRequestHeader string

		expectBackendToSeeHeader string
	}

	//  | FFPH      | SFP       |
	//  |-----------|-----------|
	testCases := map[gorouterConfig][]testCase{
		{false, false}: {
			//  | port   | client header| received  |
			//  |--------|--------------|-----------|
			{"http", "http", "http"},
			{"http", "https", "https"},
			{"https", "http", "http"},
			{"https", "https", "https"},
		},

		{false, true}: {
			{"http", "http", "http"},
			{"http", "https", "http"}, // new feature here!
			{"https", "http", "https"},
			{"https", "https", "https"},
		},

		{true, false}: {
			{"http", "http", "https"},
			{"http", "https", "https"},
			{"https", "http", "https"},
			{"https", "https", "https"},
		},

		{true, true}: {
			{"http", "http", "https"},
			{"http", "https", "https"},
			{"https", "http", "https"},
			{"https", "https", "https"},
		},
	}

	for gc, tcs := range testCases {
		gorouterConfig := gc
		testCases := tcs

		It(fmt.Sprintf("gorouter config %v: sets the headers correctly", gorouterConfig), func() {
			testState.cfg.ForceForwardedProtoHttps = gorouterConfig.forceForwardedProtoHTTPS
			testState.cfg.SanitizeForwardedProto = gorouterConfig.sanitizeForwardedProto
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

				By(fmt.Sprintf("case %d: %v via external route service", i, testCase))
				hostname = fmt.Sprintf("basic-app-%d-via-external-route-service.some.domain", i)

				receivedHeaders = make(chan http.Header, 1)
				routeService := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					receivedHeaders <- r.Header
					w.WriteHeader(200)
				}))
				routeService.TLS = testState.trustedExternalServiceTLS
				routeService.StartTLS()
				defer routeService.Close()
				testState.registerWithExternalRouteService(testApp, routeService, testState.trustedExternalServiceHostname, hostname)

				doRequest(testCase, hostname)

				gotHeader = <-receivedHeaders
				Expect(gotHeader).To(HaveKeyWithValue("X-Forwarded-Proto", []string{testCase.expectBackendToSeeHeader}))

				By(fmt.Sprintf("case %d: %v via internal route service", i, testCase))
				hostname = fmt.Sprintf("basic-app-%d-via-internal-route-service.some.domain", i)

				receivedHeaders = make(chan http.Header, 1)
				routeService = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					receivedHeaders <- r.Header
					w.WriteHeader(200)
				}))
				defer routeService.Close()
				testState.registerWithInternalRouteService(testApp, routeService, hostname)

				doRequest(testCase, hostname)

				gotHeader = <-receivedHeaders
				Expect(gotHeader).To(HaveKeyWithValue("X-Forwarded-Proto", []string{testCase.expectBackendToSeeHeader}))
			}
		})
	}
})
