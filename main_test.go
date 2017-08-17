package main_test

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"path"
	"regexp"
	"strconv"
	"strings"
	"sync"

	"code.cloudfoundry.org/gorouter/common/uuid"
	"code.cloudfoundry.org/gorouter/config"
	"code.cloudfoundry.org/gorouter/handlers"
	"code.cloudfoundry.org/gorouter/route"
	"code.cloudfoundry.org/gorouter/test"
	"code.cloudfoundry.org/gorouter/test/common"
	"code.cloudfoundry.org/gorouter/test_util"
	"code.cloudfoundry.org/localip"
	"github.com/nats-io/nats"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	. "github.com/onsi/gomega/gbytes"
	. "github.com/onsi/gomega/gexec"
	"github.com/onsi/gomega/ghttp"
	"gopkg.in/yaml.v2"

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

const defaultPruneInterval = 1
const defaultPruneThreshold = 2

var _ = Describe("Router Integration", func() {

	var (
		tmpdir          string
		natsPort        uint16
		natsRunner      *test_util.NATSRunner
		gorouterSession *Session
		oauthServerURL  string
	)

	writeConfig := func(config *config.Config, cfgFile string) {
		cfgBytes, err := yaml.Marshal(config)
		Expect(err).ToNot(HaveOccurred())
		ioutil.WriteFile(cfgFile, cfgBytes, os.ModePerm)
	}

	configDrainSetup := func(cfg *config.Config, pruneInterval, pruneThreshold, drainWait int) {
		// ensure the threshold is longer than the interval that we check,
		// because we set the route's timestamp to time.Now() on the interval
		// as part of pausing
		cfg.PruneStaleDropletsInterval = time.Duration(pruneInterval) * time.Second
		cfg.DropletStaleThreshold = time.Duration(pruneThreshold) * time.Second
		cfg.StartResponseDelayInterval = 1 * time.Second
		cfg.EndpointTimeout = 5 * time.Second
		cfg.DrainTimeout = 1 * time.Second
		cfg.DrainWait = time.Duration(drainWait) * time.Second
	}

	createConfig := func(cfgFile string, statusPort, proxyPort uint16, pruneInterval, pruneThreshold, drainWait int, suspendPruning bool, maxBackendConns int64, natsPorts ...uint16) *config.Config {
		cfg := test_util.SpecConfig(statusPort, proxyPort, natsPorts...)

		configDrainSetup(cfg, pruneInterval, pruneThreshold, drainWait)

		cfg.SuspendPruningIfNatsUnavailable = suspendPruning
		caCertsPath := filepath.Join("test", "assets", "certs", "uaa-ca.pem")
		caCertsPath, err := filepath.Abs(caCertsPath)
		Expect(err).ToNot(HaveOccurred())
		cfg.LoadBalancerHealthyThreshold = 0
		cfg.OAuth = config.OAuthConfig{
			TokenEndpoint: "127.0.0.1",
			Port:          8443,
			ClientName:    "client-id",
			ClientSecret:  "client-secret",
			CACerts:       caCertsPath,
		}
		cfg.Backends.MaxConns = maxBackendConns

		writeConfig(cfg, cfgFile)
		return cfg
	}
	createIsoSegConfig := func(cfgFile string, statusPort, proxyPort uint16, pruneInterval, pruneThreshold, drainWait int, suspendPruning bool, isoSegs []string, natsPorts ...uint16) *config.Config {
		cfg := test_util.SpecConfig(statusPort, proxyPort, natsPorts...)

		configDrainSetup(cfg, pruneInterval, pruneThreshold, drainWait)

		cfg.SuspendPruningIfNatsUnavailable = suspendPruning
		caCertsPath := filepath.Join("test", "assets", "certs", "uaa-ca.pem")
		caCertsPath, err := filepath.Abs(caCertsPath)
		Expect(err).ToNot(HaveOccurred())
		cfg.LoadBalancerHealthyThreshold = 0
		cfg.OAuth = config.OAuthConfig{
			TokenEndpoint: "127.0.0.1",
			Port:          8443,
			ClientName:    "client-id",
			ClientSecret:  "client-secret",
			CACerts:       caCertsPath,
		}
		cfg.IsolationSegments = isoSegs

		writeConfig(cfg, cfgFile)
		return cfg
	}

	createSSLConfig := func(statusPort, proxyPort, SSLPort uint16, natsPorts ...uint16) (*config.Config, *x509.CertPool) {
		cfg, clientTrustedCAs := test_util.SpecSSLConfig(statusPort, proxyPort, SSLPort, natsPorts...)

		configDrainSetup(cfg, defaultPruneInterval, defaultPruneThreshold, 0)
		return cfg, clientTrustedCAs
	}

	startGorouterSession := func(cfgFile string) *Session {
		gorouterCmd := exec.Command(gorouterPath, "-c", cfgFile)
		session, err := Start(gorouterCmd, GinkgoWriter, GinkgoWriter)
		Expect(err).ToNot(HaveOccurred())
		var eventsSessionLogs []byte
		Eventually(func() string {
			logAdd, err := ioutil.ReadAll(session.Out)
			Expect(err).ToNot(HaveOccurred(), "Gorouter session closed")
			eventsSessionLogs = append(eventsSessionLogs, logAdd...)
			return string(eventsSessionLogs)
		}, 70*time.Second).Should(SatisfyAll(
			ContainSubstring(`starting`),
			MatchRegexp(`Successfully-connected-to-nats.*localhost:\d+`),
			ContainSubstring(`gorouter.started`),
		))
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
		natsRunner = test_util.NewNATSRunner(int(natsPort))
		natsRunner.Start()
		oauthServerURL = oauthServer.Addr()
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

	Context("IsolationSegments", func() {
		var (
			statusPort uint16
			proxyPort  uint16
			cfgFile    string
		)
		BeforeEach(func() {
			statusPort = test_util.NextAvailPort()
			proxyPort = test_util.NextAvailPort()

			cfgFile = filepath.Join(tmpdir, "config.yml")
			createIsoSegConfig(cfgFile, statusPort, proxyPort, defaultPruneInterval, defaultPruneThreshold, 1, false, []string{"is1", "is2"}, natsPort)
		})

		It("logs retrieved IsolationSegments", func() {
			gorouterSession = startGorouterSession(cfgFile)
			Eventually(func() string {
				return string(gorouterSession.Out.Contents())
			}).Should(ContainSubstring(`"isolation_segments":["is1","is2"]`))
		})

		It("logs routing table sharding mode", func() {
			gorouterSession = startGorouterSession(cfgFile)
			Eventually(func() string {
				return string(gorouterSession.Out.Contents())
			}).Should(ContainSubstring(`"routing_table_sharding_mode":"all"`))
		})
	})
	Context("Backend TLS ", func() {
		var (
			config            *config.Config
			statusPort        uint16
			proxyPort         uint16
			cfgFile           string
			privateInstanceId string
			certChain         test_util.CertChain
			localIP           string
			mbusClient        *nats.Conn
		)
		BeforeEach(func() {
			statusPort = test_util.NextAvailPort()
			proxyPort = test_util.NextAvailPort()

			cfgFile = filepath.Join(tmpdir, "config.yml")
			config = createConfig(cfgFile, statusPort, proxyPort, defaultPruneInterval, defaultPruneThreshold, 0, false, 10, natsPort)
			config.CipherSuites = []uint16{tls.TLS_RSA_WITH_AES_256_CBC_SHA}
			config.SkipSSLValidation = false

			privateInstanceId, _ = uuid.GenerateUUID()
			certChain = test_util.CreateSignedCertWithRootCA(privateInstanceId)
			config.CACerts = []string{string(certChain.CACertPEM)}
		})

		JustBeforeEach(func() {
			var err error
			writeConfig(config, cfgFile)
			localIP, err = localip.LocalIP()
			mbusClient, err = newMessageBus(config)
			Expect(err).ToNot(HaveOccurred())

			gorouterSession = startGorouterSession(cfgFile)
		})

		AfterEach(func() {
			gorouterSession.Kill()
		})
		Context("when backend registration includes TLS port", func() {
			Context("when backend is listening for TLS connections", func() {
				Context("when registered instance id matches the common name on cert presented by the backend", func() {
					It("successfully connects to backend using TLS", func() {
						runningApp1 := test.NewGreetApp([]route.Uri{"innocent.bystander.vcap.me"}, proxyPort, mbusClient, nil)
						runningApp1.TlsRegister(privateInstanceId)
						runningApp1.TlsListen(certChain.CertPEM, certChain.PrivKeyPEM)
						heartbeatInterval := 200 * time.Millisecond
						runningTicker := time.NewTicker(heartbeatInterval)
						go func() {
							for {
								select {
								case <-runningTicker.C:
									runningApp1.TlsRegister(privateInstanceId)
								}
							}
						}()
						routesUri := fmt.Sprintf("http://%s:%s@%s:%d/routes", config.Status.User, config.Status.Pass, localIP, statusPort)

						Eventually(func() bool { return appRegistered(routesUri, runningApp1) }).Should(BeTrue())
						runningApp1.VerifyAppStatus(200)
					})
				})

			})

			Context("when backend is only listening for non TLS connections", func() {
				It("fails with a 525 SSL Handshake error", func() {
					runningApp1 := test.NewGreetApp([]route.Uri{"innocent.bystander.vcap.me"}, proxyPort, mbusClient, nil)
					runningApp1.TlsRegister(privateInstanceId)
					runningApp1.Listen()
					routesUri := fmt.Sprintf("http://%s:%s@%s:%d/routes", config.Status.User, config.Status.Pass, localIP, statusPort)

					heartbeatInterval := 200 * time.Millisecond
					runningTicker := time.NewTicker(heartbeatInterval)
					go func() {
						for {
							select {
							case <-runningTicker.C:
								runningApp1.TlsRegister(privateInstanceId)
							}
						}
					}()
					Eventually(func() bool { return appRegistered(routesUri, runningApp1) }).Should(BeTrue())
					runningApp1.VerifyAppStatus(525)
				})
			})
		})
	})
	Context("Frontend TLS", func() {
		var (
			cfg              *config.Config
			statusPort       uint16
			proxyPort        uint16
			cfgFile          string
			dialTls          func(version uint16) error
			clientTrustedCAs *x509.CertPool
		)
		BeforeEach(func() {
			statusPort = test_util.NextAvailPort()
			proxyPort = test_util.NextAvailPort()

			cfgFile = filepath.Join(tmpdir, "config.yml")
			cfg, clientTrustedCAs = createSSLConfig(statusPort, proxyPort, test_util.NextAvailPort(), defaultPruneInterval, defaultPruneThreshold, natsPort)
		})
		JustBeforeEach(func() {
			writeConfig(cfg, cfgFile)
			dialTls = func(version uint16) error {

				tlsConfig := &tls.Config{
					MaxVersion: version,
				}

				t := &http.Transport{TLSClientConfig: tlsConfig}
				client := &http.Client{Transport: t}
				_, err := client.Get(fmt.Sprintf("https://localhost:%d", cfg.SSLPort))
				return err
			}
		})

		Context("when no cipher suite is supported by both client and server", func() {
			BeforeEach(func() {
				keyPEM1, certPEM1 := test_util.CreateKeyPair("potato.com")
				keyPEM2, certPEM2 := test_util.CreateKeyPair("potato2.com")

				cfg.TLSPEM = []config.TLSPem{
					config.TLSPem{
						PrivateKey: string(keyPEM1),
						CertChain:  string(certPEM1),
					},
					config.TLSPem{
						PrivateKey: string(keyPEM2),
						CertChain:  string(certPEM2),
					},
				}
				cfg.CipherString = "RC4-SHA"
			})

			It("throws an error", func() {
				gorouterSession = startGorouterSession(cfgFile)
				err := dialTls(tls.VersionTLS12)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("handshake failure"))
			})
		})

		It("supports minimum TLS 1.2 by default", func() {
			gorouterSession = startGorouterSession(cfgFile)

			dialTls := func(version uint16) error {

				tlsConfig := &tls.Config{
					MaxVersion: version,
					RootCAs:    clientTrustedCAs,
					ServerName: "potato.com",
				}

				t := &http.Transport{TLSClientConfig: tlsConfig}
				client := &http.Client{Transport: t}
				_, err := client.Get(fmt.Sprintf("https://localhost:%d", cfg.SSLPort))
				return err
			}

			Expect(dialTls(tls.VersionSSL30)).To(HaveOccurred())
			Expect(dialTls(tls.VersionTLS10)).To(HaveOccurred())
			Expect(dialTls(tls.VersionTLS11)).To(HaveOccurred())
			Expect(dialTls(tls.VersionTLS12)).ToNot(HaveOccurred())
		})
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
			config = createConfig(cfgFile, statusPort, proxyPort, defaultPruneInterval, defaultPruneThreshold, 1, false, 0, natsPort)
		})

		JustBeforeEach(func() {
			gorouterSession = startGorouterSession(cfgFile)
		})

		It("responds to healthcheck", func() {
			req := test_util.NewRequest("GET", "", "/", nil)
			req.Header.Set("User-Agent", "HTTP-Monitor/1.1")
			conn, err := net.Dial("tcp", fmt.Sprintf("localhost:%d", proxyPort))
			Expect(err).ToNot(HaveOccurred())
			http_conn := test_util.NewHttpConn(conn)
			http_conn.WriteRequest(req)
			resp, body := http_conn.ReadResponse()
			Expect(resp.Status).To(Equal("200 OK"))
			Expect(body).To(Equal("ok\n"))
		})

		It("waits for all requests to finish", func() {
			mbusClient, err := newMessageBus(config)
			Expect(err).ToNot(HaveOccurred())

			requestMade := make(chan bool)
			requestProcessing := make(chan bool)
			responseRead := make(chan bool)

			longApp := common.NewTestApp([]route.Uri{"longapp.vcap.me"}, proxyPort, mbusClient, nil, "")
			longApp.AddHandler("/", func(w http.ResponseWriter, r *http.Request) {
				requestMade <- true
				<-requestProcessing
				_, ioErr := ioutil.ReadAll(r.Body)
				defer r.Body.Close()
				Expect(ioErr).ToNot(HaveOccurred())
				w.WriteHeader(http.StatusOK)
				w.Write([]byte{'b'})
			})
			longApp.Register()
			longApp.Listen()
			routesUri := fmt.Sprintf("http://%s:%s@%s:%d/routes", config.Status.User, config.Status.Pass, localIP, statusPort)

			Eventually(func() bool {
				return appRegistered(routesUri, longApp)
			}).Should(BeTrue())

			go func() {
				defer GinkgoRecover()
				//Open a connection that never goes active
				Eventually(func() bool {
					conn, dialErr := net.DialTimeout("tcp",
						fmt.Sprintf("%s:%d", localIP, proxyPort), 30*time.Second)
					if dialErr == nil {
						return conn.Close() == nil
					}
					return false
				}).Should(BeTrue())

				//Open a connection that goes active
				resp, httpErr := http.Get(longApp.Endpoint())
				Expect(httpErr).ShouldNot(HaveOccurred())
				Expect(resp.StatusCode).Should(Equal(http.StatusOK))
				bytes, httpErr := ioutil.ReadAll(resp.Body)
				resp.Body.Close()
				Expect(httpErr).ShouldNot(HaveOccurred())
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
			timeoutApp := common.NewTestApp([]route.Uri{"timeout.vcap.me"}, proxyPort, mbusClient, nil, "")
			timeoutApp.AddHandler("/", func(w http.ResponseWriter, r *http.Request) {
				blocker <- true
				<-blocker
			})
			timeoutApp.Register()
			timeoutApp.Listen()
			routesUri := fmt.Sprintf("http://%s:%s@%s:%d/routes", config.Status.User, config.Status.Pass, localIP, statusPort)
			Eventually(func() bool { return appRegistered(routesUri, timeoutApp) }).Should(BeTrue())

			go func() {
				defer GinkgoRecover()
				_, httpErr := http.Get(timeoutApp.Endpoint())
				resultCh <- httpErr
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
			Expect(err).ToNot(HaveOccurred())

			blocker := make(chan bool)
			timeoutApp := common.NewTestApp([]route.Uri{"timeout.vcap.me"}, proxyPort, mbusClient, nil, "")
			timeoutApp.AddHandler("/", func(w http.ResponseWriter, r *http.Request) {
				blocker <- true
				<-blocker
			})
			timeoutApp.Register()
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
				config, _ := createSSLConfig(statusPort, proxyPort, test_util.NextAvailPort(), defaultPruneInterval, defaultPruneThreshold, natsPort)
				writeConfig(config, cfgFile)
			})

			It("drains properly", func() {
				grouter := gorouterSession
				gorouterSession = nil
				err := grouter.Command.Process.Signal(syscall.SIGUSR1)

				Expect(err).ToNot(HaveOccurred())
				Eventually(grouter, 5).Should(Exit(0))
			})
		})

		Context("when multiple signals are received", func() {
			It("drains properly", func() {
				grouter := gorouterSession
				gorouterSession = nil
				err := grouter.Command.Process.Signal(syscall.SIGUSR1)
				Expect(err).ToNot(HaveOccurred())

				// send more signals to ensure gorouter still drains gracefully
				go func() {
					for i := 0; i < 10; i++ {
						grouter.Command.Process.Signal(syscall.SIGUSR1)
						time.Sleep(5 * time.Millisecond)
					}
				}()
				Eventually(grouter).Should(Say("gorouter.stopped"))
			})
		})
	})

	Context("When Dropsonde is misconfigured", func() {
		It("fails to start", func() {
			statusPort := test_util.NextAvailPort()
			proxyPort := test_util.NextAvailPort()

			cfgFile := filepath.Join(tmpdir, "config.yml")
			config := createConfig(cfgFile, statusPort, proxyPort, defaultPruneInterval, defaultPruneThreshold, 0, false, 0, natsPort)
			config.Logging.MetronAddress = ""
			writeConfig(config, cfgFile)

			gorouterCmd := exec.Command(gorouterPath, "-c", cfgFile)
			gorouterSession, _ = Start(gorouterCmd, GinkgoWriter, GinkgoWriter)
			Eventually(gorouterSession, 5*time.Second).Should(Exit(1))
		})
	})

	It("logs component logs", func() {
		statusPort := test_util.NextAvailPort()
		proxyPort := test_util.NextAvailPort()
		cfgFile := filepath.Join(tmpdir, "config.yml")
		createConfig(cfgFile, statusPort, proxyPort, defaultPruneInterval, defaultPruneThreshold, 0, false, 0, natsPort)

		gorouterSession = startGorouterSession(cfgFile)

		Eventually(gorouterSession.Out.Contents).Should(ContainSubstring("Component Router registered successfully"))
	})

	It("has Nats connectivity", func() {
		localIP, err := localip.LocalIP()
		Expect(err).ToNot(HaveOccurred())

		statusPort := test_util.NextAvailPort()
		proxyPort := test_util.NextAvailPort()

		cfgFile := filepath.Join(tmpdir, "config.yml")
		config := createConfig(cfgFile, statusPort, proxyPort, defaultPruneInterval, defaultPruneThreshold, 0, false, 0, natsPort)

		gorouterSession = startGorouterSession(cfgFile)

		mbusClient, err := newMessageBus(config)
		Expect(err).ToNot(HaveOccurred())

		zombieApp := test.NewGreetApp([]route.Uri{"zombie.vcap.me"}, proxyPort, mbusClient, nil)
		zombieApp.Register()
		zombieApp.Listen()

		runningApp := test.NewGreetApp([]route.Uri{"innocent.bystander.vcap.me"}, proxyPort, mbusClient, nil)
		runningApp.AddHandler("/some-path", func(w http.ResponseWriter, r *http.Request) {
			defer GinkgoRecover()
			traceHeader := r.Header.Get(handlers.B3TraceIdHeader)
			spanIDHeader := r.Header.Get(handlers.B3SpanIdHeader)
			Expect(traceHeader).ToNot(BeEmpty())
			Expect(spanIDHeader).ToNot(BeEmpty())
			w.WriteHeader(http.StatusOK)
		})
		runningApp.Register()
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

		uri := fmt.Sprintf("http://%s:%d/%s", "innocent.bystander.vcap.me", proxyPort, "some-path")
		_, err = http.Get(uri)
		Expect(err).ToNot(HaveOccurred())
	})

	Context("when nats server shuts down and comes back up", func() {
		It("should not panic, log the disconnection, and reconnect", func() {
			localIP, err := localip.LocalIP()
			Expect(err).ToNot(HaveOccurred())

			statusPort := test_util.NextAvailPort()
			proxyPort := test_util.NextAvailPort()

			cfgFile := filepath.Join(tmpdir, "config.yml")
			config := createConfig(cfgFile, statusPort, proxyPort, defaultPruneInterval, defaultPruneThreshold, 0, false, 0, natsPort)
			config.NatsClientPingInterval = 1 * time.Second
			writeConfig(config, cfgFile)
			gorouterSession = startGorouterSession(cfgFile)

			mbusClient, err := newMessageBus(config)
			Expect(err).ToNot(HaveOccurred())

			zombieApp := test.NewGreetApp([]route.Uri{"zombie.vcap.me"}, proxyPort, mbusClient, nil)
			zombieApp.Register()
			zombieApp.Listen()

			routesUri := fmt.Sprintf("http://%s:%s@%s:%d/routes", config.Status.User, config.Status.Pass, localIP, statusPort)

			Eventually(func() bool { return appRegistered(routesUri, zombieApp) }).Should(BeTrue())

			heartbeatInterval := 200 * time.Millisecond
			zombieTicker := time.NewTicker(heartbeatInterval)

			go func() {
				for {
					select {
					case <-zombieTicker.C:
						zombieApp.Register()
					}
				}
			}()

			zombieApp.VerifyAppStatus(200)

			natsRunner.Stop()

			Eventually(gorouterSession).Should(Say("nats-connection-disconnected"))
			Eventually(gorouterSession, time.Second*25).Should(Say("nats-connection-still-disconnected"))
			natsRunner.Start()
			Eventually(gorouterSession, time.Second*5).Should(Say("nats-connection-reconnected"))
			Consistently(gorouterSession, time.Second*25).ShouldNot(Say("nats-connection-still-disconnected"))
			Consistently(gorouterSession.ExitCode, 150*time.Second).ShouldNot(Equal(1))
		})
	})

	Context("multiple nats server", func() {
		var (
			config         *config.Config
			cfgFile        string
			natsPort2      uint16
			proxyPort      uint16
			statusPort     uint16
			natsRunner2    *test_util.NATSRunner
			pruneInterval  int
			pruneThreshold int
		)

		BeforeEach(func() {
			natsPort2 = test_util.NextAvailPort()
			natsRunner2 = test_util.NewNATSRunner(int(natsPort2))

			statusPort = test_util.NextAvailPort()
			proxyPort = test_util.NextAvailPort()

			cfgFile = filepath.Join(tmpdir, "config.yml")
			pruneInterval = 2
			pruneThreshold = 10
			config = createConfig(cfgFile, statusPort, proxyPort, pruneInterval, pruneThreshold, 0, false, 0, natsPort, natsPort2)
		})

		AfterEach(func() {
			natsRunner2.Stop()
		})

		JustBeforeEach(func() {
			gorouterSession = startGorouterSession(cfgFile)
		})

		It("fails over to second nats server before pruning", func() {
			localIP, err := localip.LocalIP()
			Expect(err).ToNot(HaveOccurred())

			mbusClient, err := newMessageBus(config)
			Expect(err).ToNot(HaveOccurred())

			runningApp := test.NewGreetApp([]route.Uri{"demo.vcap.me"}, proxyPort, mbusClient, nil)
			runningApp.Register()
			runningApp.Listen()

			routesUri := fmt.Sprintf("http://%s:%s@%s:%d/routes", config.Status.User, config.Status.Pass, localIP, statusPort)

			Eventually(func() bool { return appRegistered(routesUri, runningApp) }).Should(BeTrue())

			heartbeatInterval := 200 * time.Millisecond
			runningTicker := time.NewTicker(heartbeatInterval)

			go func() {
				for {
					select {
					case <-runningTicker.C:
						runningApp.Register()
					}
				}
			}()

			runningApp.VerifyAppStatus(200)

			// Give enough time to register multiple times
			time.Sleep(heartbeatInterval * 3)

			natsRunner.Stop()
			natsRunner2.Start()

			staleCheckInterval := config.PruneStaleDropletsInterval
			staleThreshold := config.DropletStaleThreshold
			// Give router time to make a bad decision (i.e. prune routes)
			sleepTime := (2 * staleCheckInterval) + (2 * staleThreshold)
			time.Sleep(sleepTime)

			// Expect not to have pruned the routes as it fails over to next NAT server
			runningApp.VerifyAppStatus(200)

			natsRunner.Start()

		})

		Context("when suspend_pruning_if_nats_unavailable enabled", func() {

			BeforeEach(func() {
				natsPort2 = test_util.NextAvailPort()
				natsRunner2 = test_util.NewNATSRunner(int(natsPort2))

				statusPort = test_util.NextAvailPort()
				proxyPort = test_util.NextAvailPort()

				cfgFile = filepath.Join(tmpdir, "config.yml")
				pruneInterval = 2
				pruneThreshold = 10
				suspendPruningIfNatsUnavailable := true
				config = createConfig(cfgFile, statusPort, proxyPort, pruneInterval, pruneThreshold, 0, suspendPruningIfNatsUnavailable, 0, natsPort, natsPort2)
			})

			It("does not prune routes when nats is unavailable", func() {
				localIP, err := localip.LocalIP()
				Expect(err).ToNot(HaveOccurred())

				mbusClient, err := newMessageBus(config)
				Expect(err).ToNot(HaveOccurred())

				runningApp := test.NewGreetApp([]route.Uri{"demo.vcap.me"}, proxyPort, mbusClient, nil)
				runningApp.Register()
				runningApp.Listen()

				routesUri := fmt.Sprintf("http://%s:%s@%s:%d/routes", config.Status.User, config.Status.Pass, localIP, statusPort)

				Eventually(func() bool { return appRegistered(routesUri, runningApp) }).Should(BeTrue())

				heartbeatInterval := 200 * time.Millisecond
				runningTicker := time.NewTicker(heartbeatInterval)

				go func() {
					for {
						select {
						case <-runningTicker.C:
							runningApp.Register()
						}
					}
				}()

				runningApp.VerifyAppStatus(200)

				// Give enough time to register multiple times
				time.Sleep(heartbeatInterval * 3)

				natsRunner.Stop()

				staleCheckInterval := config.PruneStaleDropletsInterval
				staleThreshold := config.DropletStaleThreshold

				// Give router time to make a bad decision (i.e. prune routes)
				sleepTime := (2 * staleCheckInterval) + (2 * staleThreshold)
				time.Sleep(sleepTime)

				// Expect not to have pruned the routes after nats goes away
				runningApp.VerifyAppStatus(200)
			})
		})
	})

	Context("route services", func() {
		var (
			session                        *Session
			config                         *config.Config
			statusPort, proxyPort, sslPort uint16
		)

		BeforeEach(func() {
			statusPort = test_util.NextAvailPort()
			proxyPort = test_util.NextAvailPort()
			sslPort = test_util.NextAvailPort()

			config, _ = createSSLConfig(statusPort, proxyPort, sslPort, natsPort)
			config.RouteServiceSecret = "route-service-secret"
			config.RouteServiceSecretPrev = "my-previous-route-service-secret"

		})

		JustBeforeEach(func() {
			cfgFile := filepath.Join(tmpdir, "config.yml")
			writeConfig(config, cfgFile)
			session = startGorouterSession(cfgFile)
		})

		AfterEach(func() {
			stopGorouter(session)
		})

		Context("When an HTTPS request is destined to an app bound to route service", func() {
			var rsKey, rsCert []byte
			BeforeEach(func() {
				rsKey, rsCert = test_util.CreateKeyPair("test.routeservice.com")
				config.CACerts = []string{string(rsCert)}
			})
			It("successfully connects to the route service", func() {
				rsTLSCert, err := tls.X509KeyPair(rsCert, rsKey)
				Expect(err).ToNot(HaveOccurred())

				routeServiceSrv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(http.StatusTeapot)
				}))

				routeServiceSrv.TLS = &tls.Config{
					Certificates: []tls.Certificate{rsTLSCert},
					ServerName:   "test.routeservice.com",
				}
				routeServiceSrv.StartTLS()
				defer routeServiceSrv.Close()

				mbusClient, err := newMessageBus(config)
				Expect(err).ToNot(HaveOccurred())

				runningApp := common.NewTestApp([]route.Uri{"demo.vcap.me"}, proxyPort, mbusClient, nil, routeServiceSrv.URL)
				runningApp.Register()
				runningApp.Listen()

				localIP, err := localip.LocalIP()
				Expect(err).ToNot(HaveOccurred())

				routesUri := fmt.Sprintf("http://%s:%s@%s:%d/routes", config.Status.User, config.Status.Pass, localIP, statusPort)

				Eventually(func() bool { return appRegistered(routesUri, runningApp) }).Should(BeTrue())

				req, err := http.NewRequest("GET", fmt.Sprintf("http://%s:%d", localIP, proxyPort), nil)
				Expect(err).ToNot(HaveOccurred())
				req.Host = "demo.vcap.me"
				client := http.DefaultClient
				res, err := client.Do(req)
				Expect(err).ToNot(HaveOccurred())
				Expect(res.StatusCode).To(Equal(http.StatusTeapot))
			})
		})
	})

	Context("when no oauth config is specified", func() {
		Context("and routing api is disabled", func() {
			It("is able to start up", func() {
				statusPort := test_util.NextAvailPort()
				proxyPort := test_util.NextAvailPort()

				cfgFile := filepath.Join(tmpdir, "config.yml")
				cfg := createConfig(cfgFile, statusPort, proxyPort, defaultPruneInterval, defaultPruneThreshold, 0, false, 0, natsPort)
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

			cfg = createConfig(cfgFile, statusPort, proxyPort, defaultPruneInterval, defaultPruneThreshold, 0, false, 0, natsPort)
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
			config           *config.Config
			routingApiServer *ghttp.Server
			cfgFile          string
			responseBytes    []byte
			verifyAuthHeader http.HandlerFunc
		)

		BeforeEach(func() {
			statusPort := test_util.NextAvailPort()
			proxyPort := test_util.NextAvailPort()

			cfgFile = filepath.Join(tmpdir, "config.yml")
			config = createConfig(cfgFile, statusPort, proxyPort, defaultPruneInterval, defaultPruneThreshold, 0, false, 0, natsPort)

			responseBytes = []byte(`[{
				"guid": "abc123",
				"name": "router_group_name",
				"type": "http"
			}]`)
		})

		JustBeforeEach(func() {
			routingApiServer = ghttp.NewUnstartedServer()
			routingApiServer.RouteToHandler(
				"GET", "/routing/v1/router_groups", ghttp.CombineHandlers(
					verifyAuthHeader,
					func(w http.ResponseWriter, req *http.Request) {
						if req.URL.Query().Get("name") != "router_group_name" {
							ghttp.RespondWith(http.StatusNotFound, []byte(`error: router group not found`))(w, req)
							return
						}
						ghttp.RespondWith(http.StatusOK, responseBytes)(w, req)
					},
				),
			)
			path, err := regexp.Compile("/routing/v1/.*")
			Expect(err).ToNot(HaveOccurred())
			routingApiServer.RouteToHandler(
				"GET", path, ghttp.CombineHandlers(
					verifyAuthHeader,
					ghttp.RespondWith(http.StatusOK, `[{}]`),
				),
			)
			routingApiServer.AppendHandlers(
				func(rw http.ResponseWriter, req *http.Request) {
					defer GinkgoRecover()
					Expect(true).To(
						BeFalse(),
						fmt.Sprintf(
							"Received unhandled request: %s %s",
							req.Method,
							req.URL.RequestURI(),
						),
					)
				},
			)
			routingApiServer.Start()

			config.RoutingApi.Uri, config.RoutingApi.Port = uriAndPort(routingApiServer.URL())

		})
		AfterEach(func() {
			routingApiServer.Close()
		})

		Context("when the routing api auth is disabled ", func() {
			BeforeEach(func() {
				verifyAuthHeader = func(rw http.ResponseWriter, r *http.Request) {}
			})
			It("uses the no-op token fetcher", func() {
				config.RoutingApi.AuthDisabled = true
				writeConfig(config, cfgFile)

				// note, this will start with routing api, but will not be able to connect
				session := startGorouterSession(cfgFile)
				defer stopGorouter(session)
				Eventually(gorouterSession.Out.Contents).Should(ContainSubstring("using-noop-token-fetcher"))
			})
		})

		Context("when the routing api auth is enabled (default)", func() {
			Context("when uaa is available on tls port", func() {
				BeforeEach(func() {
					verifyAuthHeader = func(rw http.ResponseWriter, req *http.Request) {
						defer GinkgoRecover()
						Expect(req.Header.Get("Authorization")).ToNot(BeEmpty())
						Expect(req.Header.Get("Authorization")).ToNot(
							Equal("bearer"),
							fmt.Sprintf(
								`"bearer" shouldn't be the only string in the "Authorization" header. Req: %s %s`,
								req.Method,
								req.URL.RequestURI(),
							),
						)
					}
					config.OAuth.TokenEndpoint, config.OAuth.Port = hostnameAndPort(oauthServerURL)
				})

				It("fetches a token from uaa", func() {
					writeConfig(config, cfgFile)

					session := startGorouterSession(cfgFile)
					defer stopGorouter(session)
					Eventually(gorouterSession.Out.Contents).Should(ContainSubstring("started-fetching-token"))
				})
				It("does not exit", func() {
					writeConfig(config, cfgFile)

					gorouterCmd := exec.Command(gorouterPath, "-c", cfgFile)
					session, err := Start(gorouterCmd, GinkgoWriter, GinkgoWriter)
					Expect(err).ToNot(HaveOccurred())
					defer session.Kill()
					Consistently(session, 5*time.Second).ShouldNot(Exit(1))
				})
			})

			Context("when the uaa is not available", func() {
				It("gorouter exits with non-zero code", func() {
					writeConfig(config, cfgFile)

					gorouterCmd := exec.Command(gorouterPath, "-c", cfgFile)
					session, err := Start(gorouterCmd, GinkgoWriter, GinkgoWriter)
					Expect(err).ToNot(HaveOccurred())
					defer session.Kill()
					Eventually(session, 30*time.Second).Should(Say("unable-to-fetch-token"))
					Eventually(session, 5*time.Second).Should(Exit(1))
				})
			})

			Context("when routing api is not available", func() {
				BeforeEach(func() {
					config.OAuth.TokenEndpoint, config.OAuth.Port = hostnameAndPort(oauthServerURL)
				})
				It("gorouter exits with non-zero code", func() {
					routingApiServer.Close()
					writeConfig(config, cfgFile)

					gorouterCmd := exec.Command(gorouterPath, "-c", cfgFile)
					session, err := Start(gorouterCmd, GinkgoWriter, GinkgoWriter)
					Expect(err).ToNot(HaveOccurred())
					defer session.Kill()
					Eventually(session, 30*time.Second).Should(Say("routing-api-connection-failed"))
					Eventually(session, 5*time.Second).Should(Exit(1))
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
				defer session.Kill()
				Eventually(session, 30*time.Second).Should(Say("tls-not-enabled"))
				Eventually(session, 5*time.Second).Should(Exit(1))
			})
		})
	})

	Context("when max conn per backend is set", func() {
		It("responds with 503 when conn limit is reached", func() {
			var wg, wg2 sync.WaitGroup

			localIP, err := localip.LocalIP()
			Expect(err).ToNot(HaveOccurred())

			statusPort := test_util.NextAvailPort()
			proxyPort := test_util.NextAvailPort()

			cfgFile := filepath.Join(tmpdir, "config.yml")
			config := createConfig(cfgFile, statusPort, proxyPort, defaultPruneInterval, defaultPruneThreshold, 0, false, 1, natsPort)
			config.EndpointTimeout = 10 * time.Second
			writeConfig(config, cfgFile)

			gorouterSession = startGorouterSession(cfgFile)
			defer gorouterSession.Kill()

			mbusClient, err := newMessageBus(config)
			Expect(err).ToNot(HaveOccurred())

			waitChan := make(chan struct{})
			runningApp1 := test.NewGreetApp([]route.Uri{"innocent.bystander.vcap.me"}, proxyPort, mbusClient, nil)
			runningApp1.AddHandler("/sleep", func(w http.ResponseWriter, r *http.Request) {
				defer GinkgoRecover()
				waitChan <- struct{}{}
				wg2.Wait()
				w.WriteHeader(http.StatusOK)
			})
			runningApp1.Register()
			runningApp1.Listen()
			routesUri := fmt.Sprintf("http://%s:%s@%s:%d/routes", config.Status.User, config.Status.Pass, localIP, statusPort)

			Eventually(func() bool { return appRegistered(routesUri, runningApp1) }).Should(BeTrue())
			runningApp1.VerifyAppStatus(200)

			heartbeatInterval := 200 * time.Millisecond
			runningTicker := time.NewTicker(heartbeatInterval)

			go func() {
				for {
					select {
					case <-runningTicker.C:
						runningApp1.Register()
					}
				}
			}()

			wg.Add(1)
			wg2.Add(1)
			go func() {
				defer GinkgoRecover()
				defer wg.Done()
				goErr := runningApp1.CheckAppStatusWithPath(200, "sleep")
				Expect(goErr).ToNot(HaveOccurred())
			}()
			Eventually(waitChan).Should(Receive())
			err = runningApp1.CheckAppStatusWithPath(503, "sleep")
			Expect(err).ToNot(HaveOccurred())
			wg2.Done()

			wg.Wait()
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
func newMessageBus(c *config.Config) (*nats.Conn, error) {
	natsMembers := make([]string, len(c.Nats))
	options := nats.DefaultOptions
	for _, info := range c.Nats {
		uri := url.URL{
			Scheme: "nats",
			User:   url.UserPassword(info.User, info.Pass),
			Host:   fmt.Sprintf("%s:%d", info.Host, info.Port),
		}
		natsMembers = append(natsMembers, uri.String())
	}
	options.Servers = natsMembers
	return options.Connect()
}

func appRegistered(routesUri string, app *common.TestApp) bool {
	routeFound, err := routeExists(routesUri, string(app.Urls()[0]))
	return err == nil && routeFound
}

func appUnregistered(routesUri string, app *common.TestApp) bool {
	routeFound, err := routeExists(routesUri, string(app.Urls()[0]))
	return err == nil && !routeFound
}

func routeExists(routesEndpoint, routeName string) (bool, error) {
	resp, err := http.Get(routesEndpoint)
	if err != nil {
		fmt.Println("Failed to get from routes endpoint")
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

func setupTlsServer() *ghttp.Server {
	oauthServer := ghttp.NewUnstartedServer()

	caCertsPath := path.Join("test", "assets", "certs")
	caCertsPath, err := filepath.Abs(caCertsPath)
	Expect(err).ToNot(HaveOccurred())

	public := filepath.Join(caCertsPath, "server.pem")
	private := filepath.Join(caCertsPath, "server.key")
	cert, err := tls.LoadX509KeyPair(public, private)
	Expect(err).ToNot(HaveOccurred())

	tlsConfig := &tls.Config{
		Certificates: []tls.Certificate{cert},
		CipherSuites: []uint16{tls.TLS_RSA_WITH_AES_256_CBC_SHA},
	}
	oauthServer.HTTPTestServer.TLS = tlsConfig
	oauthServer.AllowUnhandledRequests = true
	oauthServer.UnhandledRequestStatusCode = http.StatusOK

	publicKey := "-----BEGIN PUBLIC KEY-----\\n" +
		"MIGfMA0GCSqGSIb3DQEBAQUAA4GNADCBiQKBgQDHFr+KICms+tuT1OXJwhCUmR2d\\n" +
		"KVy7psa8xzElSyzqx7oJyfJ1JZyOzToj9T5SfTIq396agbHJWVfYphNahvZ/7uMX\\n" +
		"qHxf+ZH9BL1gk9Y6kCnbM5R60gfwjyW1/dQPjOzn9N394zd2FJoFHwdq9Qs0wBug\\n" +
		"spULZVNRxq7veq/fzwIDAQAB\\n" +
		"-----END PUBLIC KEY-----"

	data := fmt.Sprintf("{\"alg\":\"rsa\", \"value\":\"%s\"}", publicKey)
	oauthServer.RouteToHandler("GET", "/token_key",
		ghttp.CombineHandlers(
			ghttp.VerifyRequest("GET", "/token_key"),
			ghttp.RespondWith(http.StatusOK, data)),
	)
	oauthServer.RouteToHandler("POST", "/oauth/token",
		func(w http.ResponseWriter, req *http.Request) {
			jsonBytes := []byte(`{"access_token":"some-token", "expires_in":10}`)
			w.Write(jsonBytes)
		})
	return oauthServer
}

func newTlsListener(listener net.Listener) net.Listener {
	caCertsPath := path.Join("test", "assets", "certs")
	caCertsPath, err := filepath.Abs(caCertsPath)
	Expect(err).ToNot(HaveOccurred())

	public := filepath.Join(caCertsPath, "server.pem")
	private := filepath.Join(caCertsPath, "server.key")
	cert, err := tls.LoadX509KeyPair(public, private)
	Expect(err).ToNot(HaveOccurred())

	tlsConfig := &tls.Config{
		Certificates: []tls.Certificate{cert},
		CipherSuites: []uint16{tls.TLS_RSA_WITH_AES_256_CBC_SHA},
	}

	return tls.NewListener(listener, tlsConfig)
}
