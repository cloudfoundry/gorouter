package main_test

import (
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
		type testCase struct {
			// gorouter configuration options
			forceForwardedProtoHTTPS bool
			sanitizeForwardedProto   bool

			// client behavior
			clientRequestScheme string
			clientRequestHeader string

			expectBackendToSeeHeader string
		}

		testCases := []testCase{
			//  | FFPH      | SFP       | port   | client header| received  |
			//  |-----------|-----------|--------|--------------|-----------|
			{false, false, "http", "http", "http"},
			{false, false, "http", "https", "https"},
			{false, false, "https", "http", "http"},
			{false, false, "https", "https", "https"},
			{false, true, "http", "http", "http"},
			{false, true, "http", "https", "http"}, // new feature here!
			{false, true, "https", "http", "https"},
			{false, true, "https", "https", "https"},
			{true, false, "http", "http", "https"},
			{true, false, "http", "https", "https"},
			{true, false, "https", "http", "https"},
			{true, false, "https", "https", "https"},
			{true, false, "http", "http", "https"},
			{true, true, "http", "https", "https"},
			{true, true, "https", "http", "https"},
			{true, true, "https", "https", "https"},
		}

		for i, tc := range testCases {
			testCase := tc
			It(fmt.Sprintf("case %d: %v: sets the header correctly", i, testCase), func() {
				testState.cfg.ForceForwardedProtoHttps = testCase.forceForwardedProtoHTTPS
				// testState.cfg.SanitizeForwardedProto = testCase.sanitizeForwardedProto  <-- implement this!
				testState.StartGorouter()

				receivedHeaders := make(chan http.Header, 1)
				testApp := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					receivedHeaders <- r.Header
					w.WriteHeader(200)
				}))
				defer testApp.Close()
				testState.register(testApp, "basic-app.some.domain")

				req := testState.newRequest(testCase.clientRequestScheme, "basic-app.some.domain")
				if testCase.clientRequestHeader != "" {
					req.Header.Set("X-Forwarded-Proto", testCase.clientRequestHeader)
				}
				resp, err := testState.client.Do(req)
				Expect(err).NotTo(HaveOccurred())
				Expect(resp.StatusCode).To(Equal(200))
				resp.Body.Close()

				gotHeader := <-receivedHeaders
				Expect(gotHeader).To(HaveKeyWithValue("X-Forwarded-Proto", []string{testCase.expectBackendToSeeHeader}))
			})
		}
	})

})

type testState struct {
	// these get set by the constructor
	cfg    *config.Config
	client *http.Client

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

	b, _ := json.Marshal(rm)
	s.mbusClient.Publish("router.register", b)

	routesUri := fmt.Sprintf("http://%s:%s@127.0.0.1:%d/routes", s.cfg.Status.User, s.cfg.Status.Pass, s.cfg.Status.Port)
	Eventually(func() (bool, error) {
		return routeExists(routesUri, routeURI)
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
