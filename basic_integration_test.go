package main_test

import (
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"math/rand"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"code.cloudfoundry.org/gorouter/config"
	"code.cloudfoundry.org/gorouter/mbus"
	"code.cloudfoundry.org/gorouter/route"
	"code.cloudfoundry.org/gorouter/test_util"

	nats "github.com/nats-io/go-nats"
	"gopkg.in/yaml.v2"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	. "github.com/onsi/gomega/gexec"
)

var _ = Describe("Basic integration tests", func() {

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

	// each high-level feature gets a describe block
	Describe("modifications of X-Forwarded-Proto header", func() {
		// we scope testCase to the high-level feature

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
					req := testState.newRequest(testCase.clientRequestScheme, hostname)
					req.Header.Set("X-Forwarded-Proto", testCase.clientRequestHeader)

					resp, err := testState.client.Do(req)
					Expect(err).NotTo(HaveOccurred())
					Expect(resp.StatusCode).To(Equal(200))
					resp.Body.Close()
				}

				for i, testCase := range testCases {
					By(fmt.Sprintf("case %d: %v", i, testCase), func() {
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
					})

					By(fmt.Sprintf("case %d via external route service", i), func() {
						hostname := fmt.Sprintf("basic-app-%d-via-external-route-service.some.domain", i)

						receivedHeaders := make(chan http.Header, 1)
						routeService := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
							receivedHeaders <- r.Header
							w.WriteHeader(200)
						}))
						routeService.TLS = testState.trustedExternalServiceTLS
						routeService.StartTLS()
						defer routeService.Close()
						testState.registerAsExternalRouteService(routeService, testState.trustedExternalServiceHostname, hostname)

						doRequest(testCase, hostname)

						gotHeader := <-receivedHeaders
						Expect(gotHeader).To(HaveKeyWithValue("X-Forwarded-Proto", []string{testCase.expectBackendToSeeHeader}))
					})
				}
			})
		}
	})

})

type testState struct {
	// these get set by the constructor
	cfg                            *config.Config
	client                         *http.Client
	trustedExternalServiceHostname string
	trustedExternalServiceTLS      *tls.Config

	// these get set when gorouter is started
	tmpdir          string
	natsRunner      *test_util.NATSRunner
	gorouterSession *Session
	mbusClient      *nats.Conn
}

func (s *testState) newRequest(scheme, hostname string) *http.Request {
	req, err := http.NewRequest("GET", scheme+"://"+hostname, nil)
	Expect(err).NotTo(HaveOccurred())
	port := s.cfg.Port
	if scheme == "https" {
		port = s.cfg.SSLPort
	}
	req.URL.Host = fmt.Sprintf("127.0.0.1:%d", port)
	return req
}

func (s *testState) register(backend *httptest.Server, routeURI string) {
	_, backendPort := hostnameAndPort(backend.Listener.Addr().String())
	rm := mbus.RegistryMessage{
		Host: "127.0.0.1",
		Port: uint16(backendPort),
		Uris: []route.Uri{route.Uri(routeURI)},
		StaleThresholdInSeconds: 1,
		RouteServiceURL:         "",
		PrivateInstanceID:       fmt.Sprintf("%x", rand.Int31()),
	}
	s.registerAndWait(rm)
}

func (s *testState) registerAsExternalRouteService(routeServiceServer *httptest.Server, routeServiceHostname string, routeURI string) {
	_, serverPort := hostnameAndPort(routeServiceServer.Listener.Addr().String())
	rm := mbus.RegistryMessage{
		Host: "169.254.255.255", // blackhole: traffic should just go to the route service
		Port: uint16(4),         // blackhole: no one uses port 4
		Uris: []route.Uri{route.Uri(routeURI)},
		StaleThresholdInSeconds: 1,
		RouteServiceURL:         fmt.Sprintf("https://%s:%d", routeServiceHostname, serverPort),
		PrivateInstanceID:       fmt.Sprintf("%x", rand.Int31()),
	}
	s.registerAndWait(rm)
}

func (s *testState) registerAndWait(rm mbus.RegistryMessage) {
	b, _ := json.Marshal(rm)
	s.mbusClient.Publish("router.register", b)

	routesUri := fmt.Sprintf("http://%s:%s@127.0.0.1:%d/routes", s.cfg.Status.User, s.cfg.Status.Pass, s.cfg.Status.Port)
	Eventually(func() (bool, error) {
		return routeExists(routesUri, string(rm.Uris[0]))
	}).Should(BeTrue())
}

func NewTestState() *testState {
	// TODO: don't hide so much behind these test_util methods
	cfg, clientTLSConfig := test_util.SpecSSLConfig(test_util.NextAvailPort(), test_util.NextAvailPort(), test_util.NextAvailPort(), test_util.NextAvailPort())

	// TODO: why these magic numbers?
	cfg.PruneStaleDropletsInterval = 2 * time.Second
	cfg.DropletStaleThreshold = 10 * time.Second
	cfg.StartResponseDelayInterval = 1 * time.Second
	cfg.EndpointTimeout = 5 * time.Second
	cfg.EndpointDialTimeout = 10 * time.Millisecond
	cfg.DrainTimeout = 200 * time.Millisecond
	cfg.DrainWait = 1 * time.Second

	cfg.Backends.MaxConns = 10
	cfg.LoadBalancerHealthyThreshold = 0

	cfg.SuspendPruningIfNatsUnavailable = true

	externalRouteServiceHostname := "external-route-service.localhost.routing.cf-app.com"
	routeServiceKey, routeServiceCert := test_util.CreateKeyPair(externalRouteServiceHostname)
	routeServiceTLSCert, err := tls.X509KeyPair(routeServiceCert, routeServiceKey)
	Expect(err).ToNot(HaveOccurred())
	cfg.CACerts = string(routeServiceCert)

	uaaCACertsPath, err := filepath.Abs(filepath.Join("test", "assets", "certs", "uaa-ca.pem"))
	Expect(err).ToNot(HaveOccurred())

	cfg.OAuth = config.OAuthConfig{
		ClientName:   "client-id",
		ClientSecret: "client-secret",
		CACerts:      uaaCACertsPath,
	}
	cfg.OAuth.TokenEndpoint, cfg.OAuth.Port = hostnameAndPort(oauthServer.Addr())

	return &testState{
		cfg: cfg,
		client: &http.Client{
			Transport: &http.Transport{
				TLSClientConfig: clientTLSConfig,
			},
		},
		trustedExternalServiceHostname: externalRouteServiceHostname,
		trustedExternalServiceTLS: &tls.Config{
			Certificates: []tls.Certificate{routeServiceTLSCert},
		},
	}
}

func (s *testState) StartGorouter() {
	Expect(s.cfg).NotTo(BeNil(), "set up test cfg before calling this function")

	s.natsRunner = test_util.NewNATSRunner(int(s.cfg.Nats[0].Port))
	s.natsRunner.Start()

	var err error
	s.tmpdir, err = ioutil.TempDir("", "gorouter")
	Expect(err).ToNot(HaveOccurred())

	cfgFile := filepath.Join(s.tmpdir, "config.yml")

	cfgBytes, err := yaml.Marshal(s.cfg)
	Expect(err).ToNot(HaveOccurred())
	Expect(ioutil.WriteFile(cfgFile, cfgBytes, 0644)).To(Succeed())

	cmd := exec.Command(gorouterPath, "-c", cfgFile)
	s.gorouterSession, err = Start(cmd, GinkgoWriter, GinkgoWriter)
	Expect(err).ToNot(HaveOccurred())

	Eventually(func() string {
		if s.gorouterSession.ExitCode() >= 0 {
			Fail("gorouter quit early!")
		}
		return string(s.gorouterSession.Out.Contents())
	}, 20*time.Second).Should(SatisfyAll(
		ContainSubstring(`starting`),
		MatchRegexp(`Successfully-connected-to-nats.*localhost:\d+`),
		ContainSubstring(`gorouter.started`),
	))

	s.mbusClient, err = newMessageBus(s.cfg)
	Expect(err).ToNot(HaveOccurred())
}

func (s *testState) StopAndCleanup() {
	if s.natsRunner != nil {
		s.natsRunner.Stop()
	}

	os.RemoveAll(s.tmpdir)

	if s.gorouterSession != nil && s.gorouterSession.ExitCode() == -1 {
		Eventually(s.gorouterSession.Terminate(), 5).Should(Exit(0))
	}
}
