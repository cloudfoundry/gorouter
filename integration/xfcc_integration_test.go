package integration

import (
	"crypto/tls"
	"encoding/pem"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"

	"code.cloudfoundry.org/gorouter/config"
	"code.cloudfoundry.org/gorouter/routeservice"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("modifications of X-Forwarded-Client-Cert", func() {
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
		forwardedClientCert string
	}

	type clientConfig struct {
		clientRequestScheme       string
		routeServiceRequestScheme string
		clientCert                bool
		clientXFCC                bool

		expectedXFCCAtRouteService string
		expectedXFCCAtApp          string
	}

	testCases := map[gorouterConfig][]clientConfig{
		{config.ALWAYS_FORWARD}: {
			// | client scheme | route service scheme | clientCert | clientXFCC | expectedXFCCAtRouteService | expectedXFCCAtApp |
			// |---------------|----------------------|------------|------------|----------------------------|-------------------|
			{"http", "http", false, false, "", ""},
			{"http", "http", false, true, "clientXFCC", ""},
			{"http", "https", false, false, "", ""},
			{"http", "https", false, true, "clientXFCC", ""},
			{"https", "http", false, false, "", ""},
			{"https", "http", false, true, "clientXFCC", ""},
			{"https", "http", true, false, "", ""},
			{"https", "http", true, true, "clientXFCC", ""},
			{"https", "https", false, false, "", ""},
			{"https", "https", false, true, "clientXFCC", ""},
			{"https", "https", true, false, "", ""},
			{"https", "https", true, true, "clientXFCC", ""},
		},
		{config.FORWARD}: {
			// | client scheme | route service scheme | clientCert | clientXFCC | expectedXFCCAtRouteService | expectedXFCCAtApp |
			// |---------------|----------------------|------------|------------|----------------------------|-------------------|
			{"http", "http", false, false, "", ""},
			{"http", "http", false, true, "", ""},
			{"http", "https", false, false, "", ""},
			{"http", "https", false, true, "", ""},
			{"https", "http", false, false, "", ""},
			{"https", "http", false, true, "", ""},
			{"https", "http", true, false, "", ""},
			{"https", "http", true, true, "clientXFCC", ""},
			{"https", "https", false, false, "", ""},
			{"https", "https", false, true, "", ""},
			{"https", "https", true, false, "", ""},
			{"https", "https", true, true, "clientXFCC", ""},
		},
		{config.SANITIZE_SET}: {
			// | client scheme | route service scheme | clientCert | clientXFCC | expectedXFCCAtRouteService | expectedXFCCAtApp |
			// |---------------|----------------------|------------|------------|----------------------------|-------------------|
			{"http", "http", false, false, "", ""},
			{"http", "http", false, true, "", ""},
			{"http", "https", false, false, "", ""},
			{"http", "https", false, true, "", ""},
			{"https", "http", false, false, "", ""},
			{"https", "http", false, true, "", ""},
			{"https", "http", true, false, "clientCert", ""},
			{"https", "http", true, true, "clientCert", ""},
			{"https", "https", false, false, "", ""},
			{"https", "https", false, true, "", ""},
			{"https", "https", true, false, "clientCert", "clientCert"},
			{"https", "https", true, true, "clientCert", "clientCert"},
		},
	}
	for gc, ccs := range testCases {
		gorouterCfg := gc
		clientCfgs := ccs

		for _, cc := range clientCfgs {
			clientCfg := cc

			It(fmt.Sprintf(
				"supports requests via a route service:\n\tforwarded_client_cert == %s\n\tclient request scheme: %s\n\troute service request scheme: %s\n\tclient cert: %t\n\tclient XFCC header: %t\n",
				gorouterCfg.forwardedClientCert,
				clientCfg.clientRequestScheme,
				clientCfg.routeServiceRequestScheme,
				clientCfg.clientCert,
				clientCfg.clientXFCC,
			), func() {
				testState.cfg.ForwardedClientCert = gorouterCfg.forwardedClientCert
				testState.cfg.EnableSSL = true
				testState.cfg.ClientCertificateValidationString = "request"
				if clientCfg.routeServiceRequestScheme == "https" {
					testState.cfg.RouteServiceRecommendHttps = true
				}

				testState.StartGorouter()

				doRequest := func(scheme, hostname string, addXFCCHeader bool) {
					req := testState.newRequest(fmt.Sprintf("%s://%s", scheme, hostname))
					if addXFCCHeader {
						req.Header.Add("X-Forwarded-Client-Cert", "some-client-xfcc")
					}
					resp, err := testState.client.Do(req)
					Expect(err).NotTo(HaveOccurred())
					Expect(resp.StatusCode).To(Equal(200))
					resp.Body.Close()
				}
				appHostname := "app-with-route-service.some.domain"
				appReceivedHeaders := make(chan http.Header, 1)

				testApp := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					appReceivedHeaders <- r.Header
					w.WriteHeader(200)
				}))
				defer testApp.Close()

				routeServiceReceivedHeaders := make(chan http.Header, 1)
				routeService := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					routeServiceReceivedHeaders <- r.Header
					w.WriteHeader(200)

					url := r.Header.Get(routeservice.HeaderKeyForwardedURL)
					newRequest := testState.newRequest(url)
					for k, v := range r.Header {
						newRequest.Header[k] = v
					}
					var resp *http.Response
					var err error
					if clientCfg.routeServiceRequestScheme == "https" {
						testState.routeServiceClient.Transport.(*http.Transport).TLSClientConfig.Certificates =
							testState.trustedRouteServiceClientTLSConfig.Certificates
						resp, err = testState.routeServiceClient.Do(newRequest)
					} else {
						resp, err = http.DefaultClient.Do(newRequest)
					}
					Expect(err).NotTo(HaveOccurred())
					defer resp.Body.Close()
				}))
				defer routeService.Close()

				testState.registerWithInternalRouteService(testApp, routeService, appHostname, testState.cfg.SSLPort)

				if clientCfg.clientCert {
					testState.client.Transport.(*http.Transport).TLSClientConfig.Certificates = testState.trustedClientTLSConfig.Certificates
				}
				doRequest(clientCfg.clientRequestScheme, appHostname, clientCfg.clientXFCC)

				switch clientCfg.expectedXFCCAtRouteService {
				case "":
					Expect(<-routeServiceReceivedHeaders).NotTo(HaveKey("X-Forwarded-Client-Cert"))
				case "clientXFCC":
					Expect((<-routeServiceReceivedHeaders).Get("X-Forwarded-Client-Cert")).To(Equal("some-client-xfcc"))
				case "clientCert":
					Expect((<-routeServiceReceivedHeaders).Get("X-Forwarded-Client-Cert")).To(Equal(
						sanitize(testState.trustedClientTLSConfig.Certificates[0]),
					))
				}

				switch clientCfg.expectedXFCCAtApp {
				case "":
					Expect(<-appReceivedHeaders).NotTo(HaveKey("X-Forwarded-Client-Cert"))
				case "clientXFCC":
					Expect((<-appReceivedHeaders).Get("X-Forwarded-Client-Cert")).To(Equal("some-client-xfcc"))
				case "clientCert":
					Expect((<-appReceivedHeaders).Get("X-Forwarded-Client-Cert")).To(Equal(
						sanitize(testState.trustedClientTLSConfig.Certificates[0]),
					))
				}
			})
		}
	}
})

func sanitize(cert tls.Certificate) string {
	b := pem.Block{Type: "CERTIFICATE", Bytes: cert.Certificate[0]}
	certPEM := pem.EncodeToMemory(&b)
	s := string(certPEM)
	r := strings.NewReplacer("-----BEGIN CERTIFICATE-----", "",
		"-----END CERTIFICATE-----", "",
		"\n", "")
	return r.Replace(s)
}
