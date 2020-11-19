package integration

import (
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	tls_helpers "code.cloudfoundry.org/cf-routing-test-helpers/tls"
	"code.cloudfoundry.org/gorouter/config"
	"code.cloudfoundry.org/gorouter/mbus"
	"code.cloudfoundry.org/gorouter/route"
	"code.cloudfoundry.org/gorouter/test"
	"code.cloudfoundry.org/gorouter/test/common"
	"code.cloudfoundry.org/gorouter/test_util"
	"code.cloudfoundry.org/tlsconfig"

	nats "github.com/nats-io/nats.go"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	. "github.com/onsi/gomega/gbytes"
	. "github.com/onsi/gomega/gexec"
	"github.com/onsi/gomega/ghttp"
)

var _ = Describe("Router Integration", func() {

	var (
		cfg                                      *config.Config
		cfgFile                                  string
		tmpdir                                   string
		natsPort, statusPort, proxyPort, sslPort uint16
		natsRunner                               *test_util.NATSRunner
		gorouterSession                          *Session
		oauthServerURL                           string
	)

	BeforeEach(func() {
		var err error
		tmpdir, err = ioutil.TempDir("", "gorouter")
		Expect(err).ToNot(HaveOccurred())
		cfgFile = filepath.Join(tmpdir, "config.yml")

		statusPort = test_util.NextAvailPort()
		proxyPort = test_util.NextAvailPort()
		natsPort = test_util.NextAvailPort()
		sslPort = test_util.NextAvailPort()

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

	Context("when config is invalid", func() {
		It("fails to start", func() {
			writeConfig(&config.Config{EnableSSL: true}, cfgFile)

			gorouterCmd := exec.Command(gorouterPath, "-c", cfgFile)
			gorouterSession, _ = Start(gorouterCmd, GinkgoWriter, GinkgoWriter)
			Eventually(gorouterSession, 5*time.Second).Should(Exit(1))
			Eventually(func() string {
				return string(gorouterSession.Out.Contents())
			}).Should(ContainSubstring(`Error loading config`))
		})
	})

	Context("IsolationSegments", func() {
		BeforeEach(func() {
			createIsoSegConfig(statusPort, proxyPort, cfgFile, defaultPruneInterval, defaultPruneThreshold, 1, false, []string{"is1", "is2"}, natsPort)
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

	Describe("Frontend TLS", func() {
		var (
			clientTLSConfig *tls.Config
			mbusClient      *nats.Conn
		)
		BeforeEach(func() {
			cfg, clientTLSConfig = createSSLConfig(statusPort, proxyPort, sslPort, natsPort)

		})
		JustBeforeEach(func() {
			var err error
			writeConfig(cfg, cfgFile)
			mbusClient, err = newMessageBus(cfg)
			Expect(err).ToNot(HaveOccurred())
		})

		It("forwards incoming TLS requests to backends", func() {
			gorouterSession = startGorouterSession(cfgFile)
			runningApp1 := test.NewGreetApp([]route.Uri{"test." + test_util.LocalhostDNS}, proxyPort, mbusClient, nil)
			runningApp1.Register()
			runningApp1.Listen()
			routesUri := fmt.Sprintf("http://%s:%s@%s:%d/routes", cfg.Status.User, cfg.Status.Pass, localIP, statusPort)

			heartbeatInterval := 200 * time.Millisecond
			runningTicker := time.NewTicker(heartbeatInterval)
			done := make(chan bool, 1)
			defer func() { done <- true }()
			go func() {
				for {
					select {
					case <-runningTicker.C:
						runningApp1.Register()
					case <-done:
						return
					}
				}
			}()
			Eventually(func() bool { return appRegistered(routesUri, runningApp1) }).Should(BeTrue())
			client := &http.Client{Transport: &http.Transport{TLSClientConfig: clientTLSConfig}}
			resp, err := client.Get(fmt.Sprintf("https://test.%s:%d", test_util.LocalhostDNS, cfg.SSLPort))
			Expect(err).ToNot(HaveOccurred())
			Expect(resp.StatusCode).To(Equal(http.StatusOK))
		})

		It("supports minimum TLS 1.2 by default", func() {
			gorouterSession = startGorouterSession(cfgFile)

			dialTls := func(version uint16) error {
				clientTLSConfig.MaxVersion = version
				client := &http.Client{Transport: &http.Transport{TLSClientConfig: clientTLSConfig}}
				_, err := client.Get(fmt.Sprintf("https://local.localhost.routing.cf-app.com:%d", cfg.SSLPort))
				return err
			}

			Expect(dialTls(tls.VersionSSL30)).NotTo(Succeed())
			Expect(dialTls(tls.VersionTLS10)).NotTo(Succeed())
			Expect(dialTls(tls.VersionTLS11)).NotTo(Succeed())
			Expect(dialTls(tls.VersionTLS12)).To(Succeed())
		})

		Context("client ca certs", func() {
			var (
				onlyTrustClientCACerts bool
				clientTLSConfig        *tls.Config
			)

			var curlAppWithCustomClientTLSConfig = func(expectedStatusCode int) {
				gorouterSession = startGorouterSession(cfgFile)

				runningApp1 := test.NewGreetApp([]route.Uri{"test." + test_util.LocalhostDNS}, proxyPort, mbusClient, nil)
				runningApp1.Register()
				runningApp1.Listen()
				routesURI := fmt.Sprintf("http://%s:%s@%s:%d/routes", cfg.Status.User, cfg.Status.Pass, localIP, statusPort)
				Eventually(func() bool { return appRegistered(routesURI, runningApp1) }, "2s").Should(BeTrue())

				client := &http.Client{Transport: &http.Transport{TLSClientConfig: clientTLSConfig}}
				resp, err := client.Get(fmt.Sprintf("https://test.%s:%d", test_util.LocalhostDNS, cfg.SSLPort))
				if expectedStatusCode >= 200 && expectedStatusCode < 600 {
					Expect(err).ToNot(HaveOccurred())
					Expect(resp.StatusCode).To(Equal(expectedStatusCode))
				} else {
					Expect(err).To(HaveOccurred())
				}
			}

			Context("when only_trust_client_ca_certs is false", func() {
				BeforeEach(func() {
					onlyTrustClientCACerts = false
				})

				Context("when the client knows about a CA in the ClientCACerts", func() {
					BeforeEach(func() {
						cfg, clientTLSConfig = createCustomSSLConfig(onlyTrustClientCACerts, test_util.TLSConfigFromClientCACerts, statusPort, proxyPort, sslPort, natsPort)
					})
					It("can reach the gorouter successfully", func() {
						curlAppWithCustomClientTLSConfig(http.StatusOK)
					})
				})

				Context("when the client knows about a CA in the CACerts", func() {
					BeforeEach(func() {
						cfg, clientTLSConfig = createCustomSSLConfig(onlyTrustClientCACerts, test_util.TLSConfigFromCACerts, statusPort, proxyPort, sslPort, natsPort)
					})
					It("can reach the gorouter succ", func() {
						curlAppWithCustomClientTLSConfig(http.StatusOK)
					})
				})
			})

			Context("when only_trust_client_ca_certs is true", func() {
				BeforeEach(func() {
					onlyTrustClientCACerts = true
				})

				Context("when the client presents a cert signed by a CA in ClientCACerts", func() {
					BeforeEach(func() {
						cfg, clientTLSConfig = createCustomSSLConfig(onlyTrustClientCACerts, test_util.TLSConfigFromClientCACerts, statusPort, proxyPort, sslPort, natsPort)
					})

					It("can reach the gorouter successfully", func() {
						curlAppWithCustomClientTLSConfig(http.StatusOK)
					})
				})

				Context("when the client presents a cert signed by a CA in CACerts", func() {
					BeforeEach(func() {
						cfg, clientTLSConfig = createCustomSSLConfig(onlyTrustClientCACerts, test_util.TLSConfigFromCACerts, statusPort, proxyPort, sslPort, natsPort)
					})

					It("cannot reach the gorouter", func() {
						curlAppWithCustomClientTLSConfig(-1)
					})
				})
			})
		})
	})

	Context("Drain", func() {

		BeforeEach(func() {
			cfg = createConfig(statusPort, proxyPort, cfgFile, defaultPruneInterval, defaultPruneThreshold, 1, false, 0, natsPort)
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
			mbusClient, err := newMessageBus(cfg)
			Expect(err).ToNot(HaveOccurred())

			requestMade := make(chan bool)
			requestProcessing := make(chan bool)
			responseRead := make(chan bool)

			longApp := common.NewTestApp([]route.Uri{"longapp." + test_util.LocalhostDNS}, proxyPort, mbusClient, nil, "")
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
			routesUri := fmt.Sprintf("http://%s:%s@%s:%d/routes", cfg.Status.User, cfg.Status.Pass, localIP, statusPort)

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
			mbusClient, err := newMessageBus(cfg)
			Expect(err).ToNot(HaveOccurred())

			blocker := make(chan bool)
			blocker2 := make(chan bool)

			resultCh := make(chan error, 1)
			timeoutApp := common.NewTestApp([]route.Uri{"timeout." + test_util.LocalhostDNS}, proxyPort, mbusClient, nil, "")
			timeoutApp.AddHandler("/", func(w http.ResponseWriter, r *http.Request) {
				blocker <- true
				<-blocker2
			})
			timeoutApp.Register()
			timeoutApp.Listen()
			routesUri := fmt.Sprintf("http://%s:%s@%s:%d/routes", cfg.Status.User, cfg.Status.Pass, localIP, statusPort)
			Eventually(func() bool { return appRegistered(routesUri, timeoutApp) }).Should(BeTrue())

			go func() {
				defer GinkgoRecover()
				_, httpErr := http.Get(timeoutApp.Endpoint())
				resultCh <- httpErr
			}()

			<-blocker
			defer func() {
				blocker2 <- true
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
			mbusClient, err := newMessageBus(cfg)
			Expect(err).ToNot(HaveOccurred())

			blocker := make(chan bool)
			blocker2 := make(chan bool)
			timeoutApp := common.NewTestApp([]route.Uri{"timeout." + test_util.LocalhostDNS}, proxyPort, mbusClient, nil, "")
			timeoutApp.AddHandler("/", func(w http.ResponseWriter, r *http.Request) {
				blocker <- true
				<-blocker2
			})
			timeoutApp.Register()
			timeoutApp.Listen()
			routesUri := fmt.Sprintf("http://%s:%s@%s:%d/routes", cfg.Status.User, cfg.Status.Pass, localIP, statusPort)
			Eventually(func() bool { return appRegistered(routesUri, timeoutApp) }).Should(BeTrue())

			go func() {
				http.Get(timeoutApp.Endpoint())
			}()

			<-blocker
			defer func() {
				blocker2 <- true
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
				tempCfg, _ := createSSLConfig(statusPort, proxyPort, sslPort, natsPort)
				writeConfig(tempCfg, cfgFile)
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
				Eventually(grouter, 5*time.Second).Should(Say("gorouter.stopped"))
			})
		})
	})

	Context("When Dropsonde is misconfigured", func() {
		It("fails to start", func() {
			tempCfg := createConfig(statusPort, proxyPort, cfgFile, defaultPruneInterval, defaultPruneThreshold, 0, false, 0, natsPort)
			tempCfg.Logging.MetronAddress = ""
			writeConfig(tempCfg, cfgFile)

			gorouterCmd := exec.Command(gorouterPath, "-c", cfgFile)
			gorouterSession, _ = Start(gorouterCmd, GinkgoWriter, GinkgoWriter)
			Eventually(gorouterSession, 5*time.Second).Should(Exit(1))
		})
	})

	It("logs component logs", func() {
		createConfig(statusPort, proxyPort, cfgFile, defaultPruneInterval, defaultPruneThreshold, 0, false, 0, natsPort)

		gorouterSession = startGorouterSession(cfgFile)

		Eventually(gorouterSession.Out.Contents).Should(ContainSubstring("Component Router registered successfully"))
	})

	Describe("metrics emitted", func() {
		var (
			fakeMetron test_util.FakeMetron
		)

		BeforeEach(func() {
			fakeMetron = test_util.NewFakeMetron()
		})
		AfterEach(func() {
			Expect(fakeMetron.Close()).To(Succeed())
		})

		It("emits route registration latency metrics, but only after a waiting period", func() {
			tempCfg := createConfig(statusPort, proxyPort, cfgFile, defaultPruneInterval, defaultPruneThreshold, 0, false, 0, natsPort)
			tempCfg.Logging.MetronAddress = fakeMetron.Address()
			tempCfg.RouteLatencyMetricMuzzleDuration = 2 * time.Second
			writeConfig(tempCfg, cfgFile)

			mbusClient, err := newMessageBus(tempCfg)
			Expect(err).ToNot(HaveOccurred())
			sendRegistration := func(port int, url string) error {
				rm := mbus.RegistryMessage{
					Host:                    "127.0.0.1",
					Port:                    uint16(port),
					Uris:                    []route.Uri{route.Uri(url)},
					Tags:                    nil,
					App:                     "0",
					StaleThresholdInSeconds: 1,
					EndpointUpdatedAtNs:     time.Now().Add(-10 * time.Second).UnixNano(),
					// simulate 10 seconds of latency on NATS message
				}

				b, _ := json.Marshal(rm)
				return mbusClient.Publish("router.register", b)
			}

			gorouterSession = startGorouterSession(cfgFile)

			counter := 0
			Consistently(func() error {
				err := sendRegistration(5000+counter, "http://something")
				if err != nil {
					return err
				}
				counter++
				// check that the latency metric is not emitted
				metricEvents := fakeMetron.AllEvents()
				for _, event := range metricEvents {
					if event.Name == "route_registration_latency" {
						return fmt.Errorf("got unexpected latency event: %v", event)
					}
				}
				return nil
			}, "1s", "100ms").Should(Succeed())

			counter = 0
			var measuredLatency_ms float64
			Eventually(func() error {
				err := sendRegistration(6000+counter, "http://something")
				if err != nil {
					return err
				}
				counter++
				metricEvents := fakeMetron.AllEvents()
				for _, event := range metricEvents {
					if event.Name != "route_registration_latency" {
						continue
					}
					measuredLatency_ms = event.Value
					return nil
				}
				return fmt.Errorf("expected metric not found yet")
			}, "4s", "100ms").Should(Succeed())

			Expect(measuredLatency_ms).To(BeNumerically(">=", 10000))
			Expect(measuredLatency_ms).To(BeNumerically("<=", 14000))
		})
	})

	Describe("route services", func() {
		var (
			clientTLSConfig *tls.Config
			routeServiceSrv *httptest.Server
			client          http.Client
			routeServiceURL string
		)

		BeforeEach(func() {

			cfg, clientTLSConfig = createSSLConfig(statusPort, proxyPort, sslPort, natsPort)
			cfg.RouteServiceSecret = "route-service-secret"
			cfg.RouteServiceSecretPrev = "my-previous-route-service-secret"

			client = http.Client{
				Transport: &http.Transport{
					TLSClientConfig: clientTLSConfig,
				},
			}
		})

		verifyAppRunning := func(runningApp *common.TestApp) {
			runningApp.WaitUntilReady()

			routesUri := fmt.Sprintf("http://%s:%s@%s:%d/routes", cfg.Status.User, cfg.Status.Pass, localIP, statusPort)
			Eventually(func() bool { return appRegistered(routesUri, runningApp) }).Should(BeTrue())
		}

		JustBeforeEach(func() {
			writeConfig(cfg, cfgFile)
			gorouterSession = startGorouterSession(cfgFile)

			mbusClient, err := newMessageBus(cfg)
			Expect(err).ToNot(HaveOccurred())

			runningApp := common.NewTestApp([]route.Uri{"demo." + test_util.LocalhostDNS}, proxyPort, mbusClient, nil, routeServiceURL)
			runningApp.Register()
			runningApp.Listen()
			verifyAppRunning(runningApp)
		})

		AfterEach(func() {
			stopGorouter(gorouterSession)
		})

		Context("when the route service is hosted on the platform (internal, has a route as an app)", func() {
			const TEST_STATUS_CODE = 477
			var routeSvcApp *common.TestApp

			BeforeEach(func() {
				mbusClient, err := newMessageBus(cfg)
				Expect(err).ToNot(HaveOccurred())

				routeSvcApp = common.NewTestApp([]route.Uri{"some-route-service." + test_util.LocalhostDNS}, proxyPort, mbusClient, nil, "")
				routeSvcApp.AddHandler("/rs", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(TEST_STATUS_CODE)
				}))
				routeServiceURL = fmt.Sprintf("https://some-route-service.%s:%d/rs", test_util.LocalhostDNS, cfg.SSLPort)
			})

			It("successfully connects to the route service", func() {
				routeSvcApp.Register()
				routeSvcApp.Listen()
				verifyAppRunning(routeSvcApp)

				req, err := http.NewRequest("GET", fmt.Sprintf("http://%s:%d", localIP, proxyPort), nil)
				Expect(err).ToNot(HaveOccurred())
				req.Host = "demo." + test_util.LocalhostDNS
				res, err := client.Do(req)
				Expect(err).ToNot(HaveOccurred())
				Expect(res.StatusCode).To(Equal(TEST_STATUS_CODE))
			})

			Context("when the client connects with HTTPS", func() {
				It("successfully connects to the route service", func() {
					routeSvcApp.Register()
					routeSvcApp.Listen()
					verifyAppRunning(routeSvcApp)

					req, err := http.NewRequest("GET", fmt.Sprintf("https://%s:%d", localIP, sslPort), nil)
					Expect(err).ToNot(HaveOccurred())
					req.Host = "demo." + test_util.LocalhostDNS
					res, err := client.Do(req)
					Expect(err).ToNot(HaveOccurred())
					Expect(res.StatusCode).To(Equal(TEST_STATUS_CODE))
				})

				Context("when the gorouter has http disabled", func() {
					BeforeEach(func() {
						cfg.DisableHTTP = true
					})

					It("successfully connects to the route service", func() {
						routeSvcApp.Register()
						routeSvcApp.Listen()
						verifyAppRunning(routeSvcApp)

						req, err := http.NewRequest("GET", fmt.Sprintf("https://%s:%d", localIP, sslPort), nil)
						Expect(err).ToNot(HaveOccurred())
						req.Host = "demo." + test_util.LocalhostDNS
						res, err := client.Do(req)
						Expect(err).ToNot(HaveOccurred())
						Expect(res.StatusCode).To(Equal(TEST_STATUS_CODE))
					})
				})
			})
		})

		Context("when the route service is not hosted on the platform (external)", func() {
			BeforeEach(func() {
				routeServiceSrv = httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(http.StatusTeapot)
				}))

				rsKey, rsCert := test_util.CreateKeyPair("test.routeservice.com")
				cfg.CACerts = string(rsCert)
				rsTLSCert, err := tls.X509KeyPair(rsCert, rsKey)
				Expect(err).ToNot(HaveOccurred())

				routeServiceSrv.TLS = &tls.Config{
					Certificates: []tls.Certificate{rsTLSCert},
					ServerName:   "test.routeservice.com",
				}
				routeServiceSrv.StartTLS()

				routeServiceURL = routeServiceSrv.URL
			})

			AfterEach(func() {
				routeServiceSrv.Close()
			})

			It("successfully connects to the route service", func() {
				req, err := http.NewRequest("GET", fmt.Sprintf("http://%s:%d", localIP, proxyPort), nil)
				Expect(err).ToNot(HaveOccurred())
				req.Host = "demo." + test_util.LocalhostDNS
				res, err := client.Do(req)
				Expect(err).ToNot(HaveOccurred())
				Expect(res.StatusCode).To(Equal(http.StatusTeapot))
			})

			Context("when the client connects with HTTPS", func() {
				It("successfully connects to the route service", func() {
					req, err := http.NewRequest("GET", fmt.Sprintf("https://%s:%d", localIP, sslPort), nil)
					Expect(err).ToNot(HaveOccurred())
					req.Host = "demo." + test_util.LocalhostDNS
					res, err := client.Do(req)
					Expect(err).ToNot(HaveOccurred())
					Expect(res.StatusCode).To(Equal(http.StatusTeapot))
				})

				Context("when the gorouter has http disabled", func() {
					BeforeEach(func() {
						cfg.DisableHTTP = true
					})

					It("successfully connects to the route service", func() {
						req, err := http.NewRequest("GET", fmt.Sprintf("https://%s:%d", localIP, sslPort), nil)
						Expect(err).ToNot(HaveOccurred())
						req.Host = "demo." + test_util.LocalhostDNS
						res, err := client.Do(req)
						Expect(err).ToNot(HaveOccurred())
						Expect(res.StatusCode).To(Equal(http.StatusTeapot))
					})
				})
			})
		})
	})

	Context("when no oauth config is specified", func() {
		Context("and routing api is disabled", func() {
			It("is able to start up", func() {
				tempCfg := createConfig(statusPort, proxyPort, cfgFile, defaultPruneInterval, defaultPruneThreshold, 0, false, 0, natsPort)
				tempCfg.OAuth = config.OAuthConfig{}
				writeConfig(tempCfg, cfgFile)

				// The process should not have any error.
				gorouterSession = startGorouterSession(cfgFile)
				stopGorouter(gorouterSession)
			})
		})
	})

	Context("when routing api is disabled", func() {
		BeforeEach(func() {
			cfg = createConfig(statusPort, proxyPort, cfgFile, defaultPruneInterval, defaultPruneThreshold, 0, false, 0, natsPort)
			writeConfig(cfg, cfgFile)
		})

		It("doesn't start the route fetcher", func() {
			gorouterSession = startGorouterSession(cfgFile)
			Eventually(gorouterSession).ShouldNot(Say("setting-up-routing-api"))
			stopGorouter(gorouterSession)
		})
	})

	Context("when the routing api is enabled", func() {
		var (
			routingApiServer *ghttp.Server
			responseBytes    []byte
			verifyAuthHeader http.HandlerFunc
		)

		BeforeEach(func() {
			cfg = createConfig(statusPort, proxyPort, cfgFile, defaultPruneInterval, defaultPruneThreshold, 0, false, 0, natsPort)

			responseBytes = []byte(`[{
				"guid": "abc123",
				"name": "router_group_name",
				"type": "http"
			}]`)
		})

		JustBeforeEach(func() {
			// server
			serverCAPath, _, _, serverCert := tls_helpers.GenerateCaAndMutualTlsCerts()
			// client
			clientCAPath, clientCertPath, clientKeyPath, _ := tls_helpers.GenerateCaAndMutualTlsCerts()

			tlsConfig, err := tlsconfig.Build(
				tlsconfig.WithInternalServiceDefaults(),
				tlsconfig.WithIdentity(serverCert),
			).Server(
				tlsconfig.WithClientAuthenticationFromFile(clientCAPath),
			)
			Expect(err).ToNot(HaveOccurred())

			routingApiServer = ghttp.NewUnstartedServer()
			routingApiServer.HTTPTestServer.TLS = tlsConfig
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
			routingApiServer.HTTPTestServer.StartTLS()

			cfg.RoutingApi.Uri, cfg.RoutingApi.Port = uriAndPort(routingApiServer.URL())
			caCerts, err := ioutil.ReadFile(serverCAPath)
			Expect(err).NotTo(HaveOccurred())
			cfg.RoutingApi.CACerts = string(caCerts)

			clientCert, err := ioutil.ReadFile(clientCertPath)
			Expect(err).NotTo(HaveOccurred())
			cfg.RoutingApi.CertChain = string(clientCert)

			clientKey, err := ioutil.ReadFile(clientKeyPath)
			Expect(err).NotTo(HaveOccurred())
			cfg.RoutingApi.PrivateKey = string(clientKey)
		})
		AfterEach(func() {
			routingApiServer.Close()
		})

		Context("when the routing api auth is disabled ", func() {
			BeforeEach(func() {
				verifyAuthHeader = func(rw http.ResponseWriter, r *http.Request) {}
			})
			It("uses the no-op token fetcher", func() {
				cfg.RoutingApi.AuthDisabled = true
				writeConfig(cfg, cfgFile)

				// note, this will start with routing api, but will not be able to connect
				gorouterSession = startGorouterSession(cfgFile)
				defer stopGorouter(gorouterSession)
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
					cfg.OAuth.TokenEndpoint, cfg.OAuth.Port = hostnameAndPort(oauthServerURL)
				})

				It("fetches a token from uaa", func() {
					writeConfig(cfg, cfgFile)

					gorouterSession = startGorouterSession(cfgFile)
					defer stopGorouter(gorouterSession)
					Eventually(gorouterSession.Out.Contents).Should(ContainSubstring("started-fetching-token"))
				})
				It("does not exit", func() {
					writeConfig(cfg, cfgFile)

					gorouterCmd := exec.Command(gorouterPath, "-c", cfgFile)
					session, err := Start(gorouterCmd, GinkgoWriter, GinkgoWriter)
					Expect(err).ToNot(HaveOccurred())
					defer session.Kill()
					Consistently(session, 5*time.Second).ShouldNot(Exit(1))
				})
			})

			Context("when the uaa is not available", func() {
				BeforeEach(func() {
					cfg.TokenFetcherRetryInterval = 100 * time.Millisecond
				})
				It("gorouter exits with non-zero code", func() {
					writeConfig(cfg, cfgFile)

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
					cfg.OAuth.TokenEndpoint, cfg.OAuth.Port = hostnameAndPort(oauthServerURL)
				})
				It("gorouter exits with non-zero code", func() {
					routingApiServer.Close()
					writeConfig(cfg, cfgFile)

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
				cfg.OAuth.Port = -1
				writeConfig(cfg, cfgFile)

				gorouterCmd := exec.Command(gorouterPath, "-c", cfgFile)
				session, err := Start(gorouterCmd, GinkgoWriter, GinkgoWriter)
				Expect(err).ToNot(HaveOccurred())
				defer session.Kill()
				Eventually(session, 30*time.Second).Should(Say("tls-not-enabled"))
				Eventually(session, 5*time.Second).Should(Exit(1))
			})
		})
	})

	Describe("caching", func() {
		var (
			goRouterClient    *http.Client
			mbusClient        *nats.Conn
			privateInstanceId string
			done              chan bool
			appRoute          string
			goRoutine         sync.WaitGroup
		)

		BeforeEach(func() {
			var clientTLSConfig *tls.Config
			cfg, clientTLSConfig = createSSLConfig(statusPort, proxyPort, sslPort, natsPort)
			writeConfig(cfg, cfgFile)
			var err error
			mbusClient, err = newMessageBus(cfg)
			Expect(err).ToNot(HaveOccurred())
			gorouterSession = startGorouterSession(cfgFile)
			goRouterClient = &http.Client{Transport: &http.Transport{TLSClientConfig: clientTLSConfig}}

			appRoute = "test." + test_util.LocalhostDNS
			runningApp1 := test.NewGreetApp([]route.Uri{route.Uri(appRoute)}, proxyPort, mbusClient, nil)
			runningApp1.Register()
			runningApp1.Listen()
			routesUri := fmt.Sprintf("http://%s:%s@%s:%d/routes", cfg.Status.User, cfg.Status.Pass, localIP, statusPort)
			privateInstanceId = runningApp1.AppGUID()

			heartbeatInterval := 200 * time.Millisecond
			runningTicker := time.NewTicker(heartbeatInterval)
			done = make(chan bool, 1)
			goRoutine.Add(1)
			go func() {
				defer goRoutine.Done()
				for {
					select {
					case <-runningTicker.C:
						runningApp1.Register()
					case <-done:
						return
					}
				}
			}()
			Eventually(func() bool { return appRegistered(routesUri, runningApp1) }).Should(BeTrue())
		})

		AfterEach(func() {
			goRoutine.Wait()
		})

		It("does not cache a 400", func() {
			defer func() { done <- true }()
			req, err := http.NewRequest("GET", fmt.Sprintf("https://%s:%d", appRoute, cfg.SSLPort), nil)
			Expect(err).ToNot(HaveOccurred())
			req.Header.Add("X-CF-APP-INSTANCE", "$^%*&%:!@#(*&$")
			resp, err := goRouterClient.Do(req)
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.StatusCode).To(Equal(http.StatusBadRequest))
			Expect(resp.Header.Get("Cache-Control")).To(Equal("no-cache, no-store"))
		})

		It("does not cache a 404", func() {
			defer func() { done <- true }()
			req, err := http.NewRequest("GET", fmt.Sprintf("https://does-not-exist.%s:%d", test_util.LocalhostDNS, cfg.SSLPort), nil)
			Expect(err).ToNot(HaveOccurred())
			resp, err := goRouterClient.Do(req)
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.StatusCode).To(Equal(http.StatusNotFound))
			Expect(resp.Header.Get("Cache-Control")).To(Equal("no-cache, no-store"))
		})

		Context("when the route exists, but the guid in the header does not", func() {
			It("does not cache a 400", func() {
				defer func() { done <- true }()
				req, err := http.NewRequest("GET", fmt.Sprintf("https://%s:%d", appRoute, cfg.SSLPort), nil)
				req.Header.Add("X-CF-APP-INSTANCE", privateInstanceId+":1")
				Expect(err).ToNot(HaveOccurred())
				resp, err := goRouterClient.Do(req)
				Expect(err).NotTo(HaveOccurred())
				Expect(resp.StatusCode).To(Equal(http.StatusBadRequest))
				Expect(resp.Header.Get("Cache-Control")).To(Equal("no-cache, no-store"))
			})
		})
	})

	Context("when instance id header is specified", func() {
		var (
			clientTLSConfig   *tls.Config
			mbusClient        *nats.Conn
			privateInstanceId string
			done              chan bool
			goRoutine         sync.WaitGroup
		)

		BeforeEach(func() {
			cfg, clientTLSConfig = createSSLConfig(statusPort, proxyPort, sslPort, natsPort)
			writeConfig(cfg, cfgFile)
			var err error
			mbusClient, err = newMessageBus(cfg)
			Expect(err).ToNot(HaveOccurred())
			startGorouterSession(cfgFile)
			runningApp1 := test.NewGreetApp([]route.Uri{"test." + test_util.LocalhostDNS}, proxyPort, mbusClient, nil)
			runningApp1.Register()
			runningApp1.Listen()
			routesUri := fmt.Sprintf("http://%s:%s@%s:%d/routes", cfg.Status.User, cfg.Status.Pass, localIP, statusPort)
			privateInstanceId = runningApp1.AppGUID()

			heartbeatInterval := 200 * time.Millisecond
			runningTicker := time.NewTicker(heartbeatInterval)
			done = make(chan bool, 1)
			goRoutine.Add(1)
			go func() {
				defer goRoutine.Done()
				for {
					select {
					case <-runningTicker.C:
						runningApp1.Register()
					case <-done:
						return
					}
				}
			}()
			Eventually(func() bool { return appRegistered(routesUri, runningApp1) }).Should(BeTrue())
		})

		AfterEach(func() {
			goRoutine.Wait()
		})

		Context("when it is syntactically invalid", func() {
			It("returns a 400 Bad Request", func() {
				defer func() { done <- true }()
				client := &http.Client{Transport: &http.Transport{TLSClientConfig: clientTLSConfig}}
				req, err := http.NewRequest("GET", fmt.Sprintf("https://test.%s:%d", test_util.LocalhostDNS, cfg.SSLPort), nil)
				Expect(err).ToNot(HaveOccurred())
				req.Header.Add("X-CF-APP-INSTANCE", "$^%*&%:!@#(*&$")
				resp, err := client.Do(req)
				Expect(err).NotTo(HaveOccurred())
				Expect(resp.StatusCode).To(Equal(http.StatusBadRequest))
			})
		})

		Context("when the instance doesn't exist", func() {
			It("returns a 400 Bad Request", func() {
				defer func() { done <- true }()
				client := &http.Client{Transport: &http.Transport{TLSClientConfig: clientTLSConfig}}
				req, err := http.NewRequest("GET", fmt.Sprintf("https://test.%s:%d", test_util.LocalhostDNS, cfg.SSLPort), nil)
				Expect(err).ToNot(HaveOccurred())
				req.Header.Add("X-CF-APP-INSTANCE", privateInstanceId+":1")
				resp, err := client.Do(req)
				Expect(err).NotTo(HaveOccurred())
				Expect(resp.StatusCode).To(Equal(http.StatusBadRequest))
			})
		})

		Context("when the instance does exist and is valid", func() {
			It("returns a ", func() {
				defer func() { done <- true }()
				client := &http.Client{Transport: &http.Transport{TLSClientConfig: clientTLSConfig}}
				req, err := http.NewRequest("GET", fmt.Sprintf("https://test.%s:%d", test_util.LocalhostDNS, cfg.SSLPort), nil)
				Expect(err).ToNot(HaveOccurred())
				req.Header.Add("X-CF-APP-INSTANCE", privateInstanceId+":0")
				resp, err := client.Do(req)
				Expect(err).NotTo(HaveOccurred())
				Expect(resp.StatusCode).To(Equal(http.StatusOK))
			})
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
	options.PingInterval = 200 * time.Millisecond
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
