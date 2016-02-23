package main_test

import (
	"crypto/tls"
	"errors"
	"strconv"
	"strings"

	"github.com/cloudfoundry-incubator/candiedyaml"
	"github.com/cloudfoundry/gorouter/config"
	"github.com/cloudfoundry/gorouter/route"
	"github.com/cloudfoundry/gorouter/test"
	"github.com/cloudfoundry/gorouter/test_util"
	"github.com/cloudfoundry/gunk/natsrunner"
	"github.com/cloudfoundry/yagnats"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	. "github.com/onsi/gomega/gbytes"
	. "github.com/onsi/gomega/gexec"
	"github.com/pivotal-golang/localip"

	"net"
	"net/http/httptest"
	"net/url"
	"syscall"

	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

var _ = Describe("Router Integration", func() {
	var tmpdir string

	var natsPort uint16
	var natsRunner *natsrunner.NATSRunner

	var gorouterSession *Session

	writeConfig := func(config *config.Config, cfgFile string) {
		cfgBytes, err := candiedyaml.Marshal(config)
		Expect(err).ToNot(HaveOccurred())
		ioutil.WriteFile(cfgFile, cfgBytes, os.ModePerm)
	}

	configDrainSetup := func(cfg *config.Config) {
		// ensure the threshold is longer than the interval that we check,
		// because we set the route's timestamp to time.Now() on the interval
		// as part of pausing
		cfg.PruneStaleDropletsIntervalInSeconds = 1
		cfg.DropletStaleThresholdInSeconds = 2
		cfg.StartResponseDelayIntervalInSeconds = 1
		cfg.EndpointTimeoutInSeconds = 5
		cfg.DrainTimeoutInSeconds = 1

		cfg.OAuth = config.OAuthConfig{
			TokenEndpoint:            "uaa.bosh-lite.com",
			Port:                     8443,
			ClientName:               "client-id",
			ClientSecret:             "client-secret",
			SkipOAuthTLSVerification: true,
		}
	}

	createConfig := func(cfgFile string, statusPort, proxyPort uint16) *config.Config {
		config := test_util.SpecConfig(natsPort, statusPort, proxyPort)

		configDrainSetup(config)

		writeConfig(config, cfgFile)
		return config
	}

	createSSLConfig := func(cfgFile string, statusPort, proxyPort, SSLPort uint16) *config.Config {
		config := test_util.SpecSSLConfig(natsPort, statusPort, proxyPort, SSLPort)

		configDrainSetup(config)

		writeConfig(config, cfgFile)
		return config
	}

	startGorouterSession := func(cfgFile string) *Session {
		gorouterCmd := exec.Command(gorouterPath, "-c", cfgFile)
		session, err := Start(gorouterCmd, GinkgoWriter, GinkgoWriter)
		Expect(err).ToNot(HaveOccurred())
		Eventually(session, 5).Should(Say("gorouter.started"))
		gorouterSession = session
		return session
	}

	stopGorouter := func(gorouterSession *Session) {
		err := gorouterSession.Command.Process.Signal(syscall.SIGTERM)
		Expect(err).ToNot(HaveOccurred())
		Eventually(gorouterSession, 5).Should(Exit(0))
	}

	BeforeEach(func() {
		var err error
		tmpdir, err = ioutil.TempDir("", "gorouter")
		Expect(err).ToNot(HaveOccurred())

		natsPort = test_util.NextAvailPort()
		natsRunner = natsrunner.NewNATSRunner(int(natsPort))
		natsRunner.Start()
	})

	AfterEach(func() {
		if natsRunner != nil {
			natsRunner.Stop()
		}

		os.RemoveAll(tmpdir)

		if gorouterSession != nil && gorouterSession.ExitCode() == -1 {
			stopGorouter(gorouterSession)
		}
	})

	Context("Drain", func() {
		var config *config.Config
		var localIP string
		var statusPort uint16
		var proxyPort uint16
		var cfgFile string

		BeforeEach(func() {
			var err error
			localIP, err = localip.LocalIP()
			Expect(err).ToNot(HaveOccurred())

			statusPort = test_util.NextAvailPort()
			proxyPort = test_util.NextAvailPort()

			cfgFile = filepath.Join(tmpdir, "config.yml")
			config = createConfig(cfgFile, statusPort, proxyPort)
		})

		JustBeforeEach(func() {
			gorouterSession = startGorouterSession(cfgFile)
		})

		It("waits for all requests to finish", func() {
			mbusClient, err := newMessageBus(config)
			Expect(err).ToNot(HaveOccurred())

			requestMade := make(chan bool)
			requestProcessing := make(chan bool)
			responseRead := make(chan bool)

			longApp := test.NewTestApp([]route.Uri{"longapp.vcap.me"}, proxyPort, mbusClient, nil, "")
			longApp.AddHandler("/", func(w http.ResponseWriter, r *http.Request) {
				requestMade <- true
				<-requestProcessing
				_, err := ioutil.ReadAll(r.Body)
				defer r.Body.Close()
				Expect(err).ToNot(HaveOccurred())
				w.WriteHeader(http.StatusOK)
				w.Write([]byte{'b'})
			})
			longApp.Listen()
			routesUri := fmt.Sprintf("http://%s:%s@%s:%d/routes", config.Status.User, config.Status.Pass, localIP, statusPort)

			Eventually(func() bool {
				return appRegistered(routesUri, longApp)
			}).Should(BeTrue())

			go func() {
				defer GinkgoRecover()
				//Open a connection that never goes active
				Eventually(func() bool {
					conn, err := net.DialTimeout("tcp",
						fmt.Sprintf("%s:%d", localIP, proxyPort), 30*time.Second)
					if err == nil {
						return conn.Close() == nil
					}
					return false
				}).Should(BeTrue())

				//Open a connection that goes active
				resp, err := http.Get(longApp.Endpoint())
				Expect(err).ShouldNot(HaveOccurred())
				Expect(resp.StatusCode).Should(Equal(http.StatusOK))
				bytes, err := ioutil.ReadAll(resp.Body)
				resp.Body.Close()
				Expect(err).ShouldNot(HaveOccurred())
				Expect(bytes).Should(Equal([]byte{'b'}))
				responseRead <- true
			}()

			grouter := gorouterSession
			gorouterSession = nil

			<-requestMade

			err = grouter.Command.Process.Signal(syscall.SIGUSR1)

			requestProcessing <- true

			Expect(err).ToNot(HaveOccurred())

			Eventually(grouter, 5).Should(Exit(0))
			Eventually(responseRead).Should(Receive(BeTrue()))
		})

		It("returns error when the gorouter terminates before a request completes", func() {
			mbusClient, err := newMessageBus(config)
			Expect(err).ToNot(HaveOccurred())

			blocker := make(chan bool)
			resultCh := make(chan error, 1)
			timeoutApp := test.NewTestApp([]route.Uri{"timeout.vcap.me"}, proxyPort, mbusClient, nil, "")
			timeoutApp.AddHandler("/", func(w http.ResponseWriter, r *http.Request) {
				blocker <- true
				<-blocker
			})
			timeoutApp.Listen()
			routesUri := fmt.Sprintf("http://%s:%s@%s:%d/routes", config.Status.User, config.Status.Pass, localIP, statusPort)
			Eventually(func() bool { return appRegistered(routesUri, timeoutApp) }).Should(BeTrue())

			go func() {
				defer GinkgoRecover()
				_, err := http.Get(timeoutApp.Endpoint())
				resultCh <- err
			}()

			<-blocker
			defer func() {
				blocker <- true
			}()

			grouter := gorouterSession
			gorouterSession = nil
			err = grouter.Command.Process.Signal(syscall.SIGUSR1)
			Expect(err).ToNot(HaveOccurred())
			Eventually(grouter, 5).Should(Exit(0))

			var result error
			Eventually(resultCh, 5).Should(Receive(&result))
			Expect(result).ToNot(BeNil())
		})

		It("prevents new connections", func() {
			mbusClient, err := newMessageBus(config)

			blocker := make(chan bool)
			timeoutApp := test.NewTestApp([]route.Uri{"timeout.vcap.me"}, proxyPort, mbusClient, nil, "")
			timeoutApp.AddHandler("/", func(w http.ResponseWriter, r *http.Request) {
				blocker <- true
				<-blocker
			})
			timeoutApp.Listen()
			routesUri := fmt.Sprintf("http://%s:%s@%s:%d/routes", config.Status.User, config.Status.Pass, localIP, statusPort)
			Eventually(func() bool { return appRegistered(routesUri, timeoutApp) }).Should(BeTrue())

			go func() {
				http.Get(timeoutApp.Endpoint())
			}()

			<-blocker
			defer func() {
				blocker <- true
			}()

			grouter := gorouterSession
			gorouterSession = nil
			err = grouter.Command.Process.Signal(syscall.SIGUSR1)
			Expect(err).ToNot(HaveOccurred())
			Eventually(grouter, 5).Should(Exit(0))

			_, err = http.Get(timeoutApp.Endpoint())
			Expect(err).To(HaveOccurred())
			urlErr := err.(*url.Error)
			opErr := urlErr.Err.(*net.OpError)
			Expect(opErr.Op).To(Equal("dial"))
		})

		Context("when ssl is enabled", func() {
			BeforeEach(func() {
				createSSLConfig(cfgFile, statusPort, proxyPort, test_util.NextAvailPort())
			})

			It("drains properly", func() {
				grouter := gorouterSession
				gorouterSession = nil
				err := grouter.Command.Process.Signal(syscall.SIGUSR1)

				Expect(err).ToNot(HaveOccurred())
				Eventually(grouter, 5).Should(Exit(0))
			})
		})
	})

	Context("When Dropsonde is misconfigured", func() {
		It("fails to start", func() {
			statusPort := test_util.NextAvailPort()
			proxyPort := test_util.NextAvailPort()

			cfgFile := filepath.Join(tmpdir, "config.yml")
			config := createConfig(cfgFile, statusPort, proxyPort)
			config.Logging.MetronAddress = ""
			writeConfig(config, cfgFile)

			gorouterCmd := exec.Command(gorouterPath, "-c", cfgFile)
			gorouterSession, _ = Start(gorouterCmd, GinkgoWriter, GinkgoWriter)
			Eventually(gorouterSession, 5*time.Second).Should(Exit(2))
		})
	})

	It("logs component logs", func() {
		statusPort := test_util.NextAvailPort()
		proxyPort := test_util.NextAvailPort()
		cfgFile := filepath.Join(tmpdir, "config.yml")
		createConfig(cfgFile, statusPort, proxyPort)

		gorouterSession = startGorouterSession(cfgFile)

		Expect(gorouterSession.Out.Contents()).To(ContainSubstring("Component Router registered successfully"))
	})

	It("has Nats connectivity", func() {
		localIP, err := localip.LocalIP()
		Expect(err).ToNot(HaveOccurred())

		statusPort := test_util.NextAvailPort()
		proxyPort := test_util.NextAvailPort()

		cfgFile := filepath.Join(tmpdir, "config.yml")
		config := createConfig(cfgFile, statusPort, proxyPort)

		gorouterSession = startGorouterSession(cfgFile)

		mbusClient, err := newMessageBus(config)

		zombieApp := test.NewGreetApp([]route.Uri{"zombie.vcap.me"}, proxyPort, mbusClient, nil)
		zombieApp.Listen()

		runningApp := test.NewGreetApp([]route.Uri{"innocent.bystander.vcap.me"}, proxyPort, mbusClient, nil)
		runningApp.Listen()

		routesUri := fmt.Sprintf("http://%s:%s@%s:%d/routes", config.Status.User, config.Status.Pass, localIP, statusPort)

		Eventually(func() bool { return appRegistered(routesUri, zombieApp) }).Should(BeTrue())
		Eventually(func() bool { return appRegistered(routesUri, runningApp) }).Should(BeTrue())

		heartbeatInterval := 200 * time.Millisecond
		zombieTicker := time.NewTicker(heartbeatInterval)
		runningTicker := time.NewTicker(heartbeatInterval)

		go func() {
			for {
				select {
				case <-zombieTicker.C:
					zombieApp.Register()
				case <-runningTicker.C:
					runningApp.Register()
				}
			}
		}()

		zombieApp.VerifyAppStatus(200)
		runningApp.VerifyAppStatus(200)

		// Give enough time to register multiple times
		time.Sleep(heartbeatInterval * 3)

		// kill registration ticker => kill app (must be before stopping NATS since app.Register is fake and queues messages in memory)
		zombieTicker.Stop()

		natsRunner.Stop()

		staleCheckInterval := config.PruneStaleDropletsInterval
		staleThreshold := config.DropletStaleThreshold
		// Give router time to make a bad decision (i.e. prune routes)
		time.Sleep(10 * (staleCheckInterval + staleThreshold))

		// While NATS is down all routes should go down
		zombieApp.VerifyAppStatus(404)
		runningApp.VerifyAppStatus(404)

		natsRunner.Start()

		// After NATS starts up the zombie should stay gone
		zombieApp.VerifyAppStatus(404)
		runningApp.VerifyAppStatus(200)
	})

	Context("when the route_services_secret and the route_services_secret_decrypt_only are valid", func() {
		It("starts fine", func() {
			statusPort := test_util.NextAvailPort()
			proxyPort := test_util.NextAvailPort()

			cfgFile := filepath.Join(tmpdir, "config.yml")
			config := createConfig(cfgFile, statusPort, proxyPort)
			config.RouteServiceSecret = "route-service-secret"
			config.RouteServiceSecretPrev = "my-previous-route-service-secret"
			writeConfig(config, cfgFile)

			// The process should not have any error.
			session := startGorouterSession(cfgFile)
			stopGorouter(session)
		})
	})

	Context("when no oauth config is specified", func() {
		Context("and routing api is disabled", func() {
			It("is able to start up", func() {
				statusPort := test_util.NextAvailPort()
				proxyPort := test_util.NextAvailPort()

				cfgFile := filepath.Join(tmpdir, "config.yml")
				cfg := createConfig(cfgFile, statusPort, proxyPort)
				cfg.OAuth = config.OAuthConfig{}
				writeConfig(cfg, cfgFile)

				// The process should not have any error.
				session := startGorouterSession(cfgFile)
				stopGorouter(session)
			})
		})
	})

	Context("when routing api is disabled", func() {
		var (
			cfgFile string
			cfg     *config.Config
		)

		BeforeEach(func() {
			statusPort := test_util.NextAvailPort()
			proxyPort := test_util.NextAvailPort()

			cfgFile = filepath.Join(tmpdir, "config.yml")

			cfg = createConfig(cfgFile, statusPort, proxyPort)
			writeConfig(cfg, cfgFile)
		})

		It("doesn't start the route fetcher", func() {
			session := startGorouterSession(cfgFile)
			Eventually(session).ShouldNot(Say("setting-up-routing-api"))
			stopGorouter(session)
		})

	})

	Context("when the routing api is enabled", func() {
		var (
			config         *config.Config
			uaaTlsListener net.Listener
			routingApi     *httptest.Server
			cfgFile        string
		)

		BeforeEach(func() {
			statusPort := test_util.NextAvailPort()
			proxyPort := test_util.NextAvailPort()

			cfgFile = filepath.Join(tmpdir, "config.yml")
			config = createConfig(cfgFile, statusPort, proxyPort)

			routingApi = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				jsonBytes := []byte(`[{"route":"foo.com","port":65340,"ip":"1.2.3.4","ttl":60,"log_guid":"foo-guid"}]`)
				w.Write(jsonBytes)
			}))
			config.RoutingApi.Uri, config.RoutingApi.Port = uriAndPort(routingApi.URL)

		})

		Context("when the routing api auth is disabled ", func() {
			It("uses the no-op token fetcher", func() {
				config.RoutingApi.AuthDisabled = true
				writeConfig(config, cfgFile)

				// note, this will start with routing api, but will not be able to connect
				session := startGorouterSession(cfgFile)
				Expect(gorouterSession.Out.Contents()).To(ContainSubstring("using-noop-token-fetcher"))
				stopGorouter(session)
			})
		})

		Context("when the routing api auth is enabled (default)", func() {
			Context("when uaa is available on tls port", func() {
				BeforeEach(func() {
					uaaTlsListener = setupTlsServer()
					config.OAuth.TokenEndpoint, config.OAuth.Port = hostnameAndPort(uaaTlsListener.Addr().String())
				})

				It("fetches a token from uaa", func() {
					writeConfig(config, cfgFile)

					// note, this will start with routing api, but will not be able to connect
					session := startGorouterSession(cfgFile)
					Expect(gorouterSession.Out.Contents()).To(ContainSubstring("fetching-token-from-uaa"))
					Expect(gorouterSession.Out.Contents()).To(ContainSubstring("using-https-scheme-for-uaa"))
					stopGorouter(session)
				})
			})

			Context("when the uaa is not available", func() {
				It("gorouter exits with non-zero code", func() {
					writeConfig(config, cfgFile)

					gorouterCmd := exec.Command(gorouterPath, "-c", cfgFile)
					session, err := Start(gorouterCmd, GinkgoWriter, GinkgoWriter)
					Expect(err).ToNot(HaveOccurred())
					Eventually(session, 30*time.Second).Should(Say("unable-to-fetch-token"))
					Eventually(session, 5*time.Second).Should(Exit(2))
				})
			})

			Context("when routing api is not available", func() {
				BeforeEach(func() {
					uaaTlsListener = setupTlsServer()
					config.OAuth.TokenEndpoint, config.OAuth.Port = hostnameAndPort(uaaTlsListener.Addr().String())
				})
				It("gorouter exits with non-zero code", func() {
					routingApi.Close()
					writeConfig(config, cfgFile)

					gorouterCmd := exec.Command(gorouterPath, "-c", cfgFile)
					session, err := Start(gorouterCmd, GinkgoWriter, GinkgoWriter)
					Expect(err).ToNot(HaveOccurred())
					Eventually(session, 30*time.Second).Should(Say("routing-api-connection-failed"))
					Eventually(session, 5*time.Second).Should(Exit(2))
				})
			})
		})

		Context("when tls for uaa is disabled", func() {
			It("fails fast", func() {
				config.OAuth.Port = -1
				writeConfig(config, cfgFile)

				gorouterCmd := exec.Command(gorouterPath, "-c", cfgFile)
				session, err := Start(gorouterCmd, GinkgoWriter, GinkgoWriter)
				Expect(err).ToNot(HaveOccurred())
				Eventually(session, 30*time.Second).Should(Say("tls-not-enabled"))
				Eventually(session, 5*time.Second).Should(Exit(2))
			})
		})
	})

	Context("when failing to open configured logging file", func() {
		var cfgFile string

		BeforeEach(func() {
			statusPort := test_util.NextAvailPort()
			proxyPort := test_util.NextAvailPort()

			cfgFile = filepath.Join(tmpdir, "config.yml")
			config := createConfig(cfgFile, statusPort, proxyPort)
			config.Logging.File = "nonExistentDir/file"
			writeConfig(config, cfgFile)
		})

		It("exits with non-zero code", func() {
			gorouterCmd := exec.Command(gorouterPath, "-c", cfgFile)
			session, err := Start(gorouterCmd, GinkgoWriter, GinkgoWriter)
			Expect(err).ToNot(HaveOccurred())
			Eventually(session, 30*time.Second).Should(Say("error-opening-log-file"))
			Eventually(session, 5*time.Second).Should(Exit(2))
		})
	})
})

func uriAndPort(url string) (string, int) {
	parts := strings.Split(url, ":")
	uri := strings.Join(parts[0:2], ":")
	port, _ := strconv.Atoi(parts[2])
	return uri, port
}

func hostnameAndPort(url string) (string, int) {
	parts := strings.Split(url, ":")
	hostname := parts[0]
	port, _ := strconv.Atoi(parts[1])
	return hostname, port
}
func newMessageBus(c *config.Config) (yagnats.NATSConn, error) {
	natsMembers := make([]string, len(c.Nats))
	for _, info := range c.Nats {
		uri := url.URL{
			Scheme: "nats",
			User:   url.UserPassword(info.User, info.Pass),
			Host:   fmt.Sprintf("%s:%d", info.Host, info.Port),
		}
		natsMembers = append(natsMembers, uri.String())
	}

	return yagnats.Connect(natsMembers)
}

func appRegistered(routesUri string, app *test.TestApp) bool {
	routeFound, err := routeExists(routesUri, string(app.Urls()[0]))
	return err == nil && routeFound
}

func appUnregistered(routesUri string, app *test.TestApp) bool {
	routeFound, err := routeExists(routesUri, string(app.Urls()[0]))
	return err == nil && !routeFound
}

func routeExists(routesEndpoint, routeName string) (bool, error) {
	resp, err := http.Get(routesEndpoint)
	if err != nil {
		return false, err
	}
	switch resp.StatusCode {
	case http.StatusOK:
		bytes, err := ioutil.ReadAll(resp.Body)
		resp.Body.Close()
		Expect(err).ToNot(HaveOccurred())
		routes := make(map[string]interface{})
		err = json.Unmarshal(bytes, &routes)
		Expect(err).ToNot(HaveOccurred())

		_, found := routes[routeName]
		return found, nil

	default:
		return false, errors.New("Didn't get an OK response")
	}
}

func setupTlsServer() net.Listener {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	Expect(err).ToNot(HaveOccurred())

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(fmt.Sprintf("{\"alg\":\"alg\", \"value\": \"%s\" }", "fake-public-key")))
	})

	tlsListener := newTlsListener(listener)
	tlsServer := &http.Server{Handler: handler}

	go func() {
		err := tlsServer.Serve(tlsListener)
		Expect(err).ToNot(HaveOccurred())
	}()
	return tlsListener
}

func newTlsListener(listener net.Listener) net.Listener {
	public := "test/assets/public.pem"
	private := "test/assets/private.pem"
	cert, err := tls.LoadX509KeyPair(public, private)
	Expect(err).ToNot(HaveOccurred())

	tlsConfig := &tls.Config{
		Certificates: []tls.Certificate{cert},
		CipherSuites: []uint16{tls.TLS_RSA_WITH_AES_256_CBC_SHA},
	}

	return tls.NewListener(listener, tlsConfig)
}
