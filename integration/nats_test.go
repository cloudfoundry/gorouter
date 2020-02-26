package integration

import (
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"code.cloudfoundry.org/gorouter/config"
	"code.cloudfoundry.org/gorouter/handlers"
	"code.cloudfoundry.org/gorouter/route"
	"code.cloudfoundry.org/gorouter/test"
	"code.cloudfoundry.org/gorouter/test_util"
	"code.cloudfoundry.org/localip"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	. "github.com/onsi/gomega/gbytes"
	. "github.com/onsi/gomega/gexec"
)

var _ = Describe("NATS Integration", func() {

	var (
		cfg                             *config.Config
		cfgFile                         string
		tmpdir                          string
		natsPort, statusPort, proxyPort uint16
		natsRunner                      *test_util.NATSRunner
		gorouterSession                 *Session
	)

	BeforeEach(func() {
		var err error
		tmpdir, err = ioutil.TempDir("", "gorouter")
		Expect(err).ToNot(HaveOccurred())
		cfgFile = filepath.Join(tmpdir, "config.yml")

		statusPort = test_util.NextAvailPort()
		proxyPort = test_util.NextAvailPort()
		natsPort = test_util.NextAvailPort()

		natsRunner = test_util.NewNATSRunner(int(natsPort))
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

	It("has Nats connectivity", func() {
		SetDefaultEventuallyTimeout(5 * time.Second)
		defer SetDefaultEventuallyTimeout(1 * time.Second)

		tempCfg := createConfig(statusPort, proxyPort, cfgFile, defaultPruneInterval, defaultPruneThreshold, 0, false, 0, natsPort)

		gorouterSession = startGorouterSession(cfgFile)

		mbusClient, err := newMessageBus(tempCfg)
		Expect(err).ToNot(HaveOccurred())

		zombieApp := test.NewGreetApp([]route.Uri{"zombie." + test_util.LocalhostDNS}, proxyPort, mbusClient, nil)
		zombieApp.Register()
		zombieApp.Listen()

		runningApp := test.NewGreetApp([]route.Uri{"innocent.bystander." + test_util.LocalhostDNS}, proxyPort, mbusClient, nil)
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

		routesUri := fmt.Sprintf("http://%s:%s@%s:%d/routes", tempCfg.Status.User, tempCfg.Status.Pass, localIP, statusPort)

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
		time.Sleep(heartbeatInterval * 2)

		// kill registration ticker => kill app (must be before stopping NATS since app.Register is fake and queues messages in memory)
		zombieTicker.Stop()

		natsRunner.Stop()

		staleCheckInterval := tempCfg.PruneStaleDropletsInterval
		staleThreshold := tempCfg.DropletStaleThreshold
		// Give router time to make a bad decision (i.e. prune routes)
		time.Sleep(3 * (staleCheckInterval + staleThreshold))

		// While NATS is down all routes should go down
		zombieApp.VerifyAppStatus(404)
		runningApp.VerifyAppStatus(404)

		natsRunner.Start()

		// After NATS starts up the zombie should stay gone
		zombieApp.VerifyAppStatus(404)
		runningApp.VerifyAppStatus(200)

		uri := fmt.Sprintf("http://%s:%d/%s", "innocent.bystander."+test_util.LocalhostDNS, proxyPort, "some-path")
		_, err = http.Get(uri)
		Expect(err).ToNot(HaveOccurred())
	})

	Context("when nats server shuts down and comes back up", func() {
		It("should not panic, log the disconnection, and reconnect", func() {
			tempCfg := createConfig(statusPort, proxyPort, cfgFile, defaultPruneInterval, defaultPruneThreshold, 0, false, 0, natsPort)
			tempCfg.NatsClientPingInterval = 100 * time.Millisecond
			writeConfig(tempCfg, cfgFile)
			gorouterSession = startGorouterSession(cfgFile)
			natsRunner.Stop()
			Eventually(gorouterSession).Should(Say("nats-connection-disconnected"))
			Eventually(gorouterSession).Should(Say("nats-connection-still-disconnected"))
			natsRunner.Start()
			Eventually(gorouterSession, 2*time.Second).Should(Say("nats-connection-reconnected"))
			Consistently(gorouterSession, 500*time.Millisecond).ShouldNot(Say("nats-connection-still-disconnected"))
			Consistently(gorouterSession.ExitCode, 2*time.Second).Should(Equal(-1))
		})
	})

	Context("multiple nats server", func() {
		var (
			natsPort2      uint16
			natsRunner2    *test_util.NATSRunner
			pruneInterval  time.Duration
			pruneThreshold time.Duration
		)

		BeforeEach(func() {
			natsPort2 = test_util.NextAvailPort()
			natsRunner2 = test_util.NewNATSRunner(int(natsPort2))

			pruneInterval = 2 * time.Second
			pruneThreshold = 10 * time.Second
			cfg = createConfig(statusPort, proxyPort, cfgFile, pruneInterval, pruneThreshold, 0, false, 0, natsPort, natsPort2)
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

			mbusClient, err := newMessageBus(cfg)
			Expect(err).ToNot(HaveOccurred())

			runningApp := test.NewGreetApp([]route.Uri{"demo." + test_util.LocalhostDNS}, proxyPort, mbusClient, nil)
			runningApp.Register()
			runningApp.Listen()

			routesUri := fmt.Sprintf("http://%s:%s@%s:%d/routes", cfg.Status.User, cfg.Status.Pass, localIP, statusPort)

			Eventually(func() bool { return appRegistered(routesUri, runningApp) }).Should(BeTrue())

			heartbeatInterval := defaultPruneThreshold / 2
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
			time.Sleep(heartbeatInterval * 2)

			natsRunner.Stop()
			natsRunner2.Start()

			// Give router time to make a bad decision (i.e. prune routes)
			sleepTime := (2 * defaultPruneInterval) + (2 * defaultPruneThreshold)
			time.Sleep(sleepTime)

			// Expect not to have pruned the routes as it fails over to next NAT server
			runningApp.VerifyAppStatus(200)

			natsRunner.Start()

		})

		Context("when suspend_pruning_if_nats_unavailable enabled", func() {

			BeforeEach(func() {
				natsPort2 = test_util.NextAvailPort()
				natsRunner2 = test_util.NewNATSRunner(int(natsPort2))

				pruneInterval = 200 * time.Millisecond
				pruneThreshold = 1000 * time.Millisecond
				suspendPruningIfNatsUnavailable := true
				cfg = createConfig(statusPort, proxyPort, cfgFile, pruneInterval, pruneThreshold, 0, suspendPruningIfNatsUnavailable, 0, natsPort, natsPort2)
				cfg.NatsClientPingInterval = 200 * time.Millisecond
			})

			It("does not prune routes when nats is unavailable", func() {
				localIP, err := localip.LocalIP()
				Expect(err).ToNot(HaveOccurred())

				mbusClient, err := newMessageBus(cfg)
				Expect(err).ToNot(HaveOccurred())

				runningApp := test.NewGreetApp([]route.Uri{"demo." + test_util.LocalhostDNS}, proxyPort, mbusClient, nil)
				runningApp.Register()
				runningApp.Listen()

				routesUri := fmt.Sprintf("http://%s:%s@%s:%d/routes", cfg.Status.User, cfg.Status.Pass, localIP, statusPort)

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
				staleCheckInterval := cfg.PruneStaleDropletsInterval
				staleThreshold := cfg.DropletStaleThreshold

				// Give router time to make a bad decision (i.e. prune routes)
				sleepTime := (2 * staleCheckInterval) + (2 * staleThreshold)
				time.Sleep(sleepTime)
				// Expect not to have pruned the routes after nats goes away
				runningApp.VerifyAppStatus(200)
			})
		})
	})
})
