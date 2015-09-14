package main_test

import (
	"errors"

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

	"io"
	"net"
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

	configDrainSetup := func(config *config.Config) {
		// ensure the threshold is longer than the interval that we check,
		// because we set the route's timestamp to time.Now() on the interval
		// as part of pausing
		config.PruneStaleDropletsIntervalInSeconds = 1
		config.DropletStaleThresholdInSeconds = 2
		config.StartResponseDelayIntervalInSeconds = 1
		config.EndpointTimeoutInSeconds = 5
		config.DrainTimeoutInSeconds = 1
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
				conn, err := net.Dial("tcp", fmt.Sprintf("%s:%d", localIP, proxyPort))
				Expect(err).NotTo(HaveOccurred())
				err = conn.Close()
				Expect(err).NotTo(HaveOccurred())

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

		It("returns EOF error when the gorouter terminates before a request completes", func() {
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
			Expect(result).To(BeAssignableToTypeOf(&url.Error{}))
			urlErr := result.(*url.Error)
			Expect(urlErr.Err).To(Equal(io.EOF))
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
			Eventually(gorouterSession, 5).Should(Exit(1))
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

	Context("when the route_services_secret is misconfigured", func() {
		It("fails to start", func() {
			statusPort := test_util.NextAvailPort()
			proxyPort := test_util.NextAvailPort()

			cfgFile := filepath.Join(tmpdir, "config.yml")
			config := createConfig(cfgFile, statusPort, proxyPort)
			config.RouteServiceSecret = "invalid secret"
			writeConfig(config, cfgFile)

			gorouterCmd := exec.Command(gorouterPath, "-c", cfgFile)
			gorouterSession, _ = Start(gorouterCmd, GinkgoWriter, GinkgoWriter)
			Eventually(gorouterSession, 5).Should(Exit(1))
		})
	})

	Context("when the route_services_secret_decrypt_only value is misconfigured", func() {
		It("fails to start", func() {
			statusPort := test_util.NextAvailPort()
			proxyPort := test_util.NextAvailPort()

			cfgFile := filepath.Join(tmpdir, "config.yml")
			config := createConfig(cfgFile, statusPort, proxyPort)
			config.RouteServiceSecret = "YP2air+sHzCrILg3XASrTHpyUVLF2WYlN1DYz854ZIc="
			config.RouteServiceSecretPrev = "invalid secret"
			writeConfig(config, cfgFile)

			gorouterCmd := exec.Command(gorouterPath, "-c", cfgFile)
			gorouterSession, _ = Start(gorouterCmd, GinkgoWriter, GinkgoWriter)
			Eventually(gorouterSession, 5).Should(Exit(1))
		})
	})

	Context("when the route_services_secret and the route_services_secret_decrypt_only are valid", func() {
		It("starts fine", func() {
			statusPort := test_util.NextAvailPort()
			proxyPort := test_util.NextAvailPort()

			cfgFile := filepath.Join(tmpdir, "config.yml")
			config := createConfig(cfgFile, statusPort, proxyPort)
			config.RouteServiceSecret = "GRSAt5/9O2cdUcuORdYRnNQkYFTpsqCpX7gaCWLayeM="
			config.RouteServiceSecretPrev = "ebag0InVm03No+vkWK3qVbFUWvimAcPLZo09q5Mf8qQ="
			writeConfig(config, cfgFile)

			// The process should not have any error.
			session := startGorouterSession(cfgFile)
			stopGorouter(session)
		})
	})
})

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
