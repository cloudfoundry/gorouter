package router_test

import (
	"time"

	"github.com/cloudfoundry/gorouter/log"
	"github.com/cloudfoundry/gorouter/registry"
	"github.com/cloudfoundry/gorouter/route"
	. "github.com/cloudfoundry/gorouter/router"
	"github.com/cloudfoundry/gorouter/test"
	"github.com/cloudfoundry/gorouter/test_util"
	"github.com/cloudfoundry/gorouter/varz"
	"github.com/cloudfoundry/yagnats"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("Router Integration", func() {
	var nats *test_util.Nats

	BeforeEach(func() {
		nats = test_util.NewNatsOnRandomPort()
		nats.Start()
	})

	AfterEach(func() {
		if nats != nil {
			nats.Stop()
		}
	})

	It("has Nats connectivity", func() {
		proxyPort := test_util.NextAvailPort()
		statusPort := test_util.NextAvailPort()

		config := SpecConfig(nats.Port(), statusPort, proxyPort)

		// ensure the threshold is longer than the interval that we check,
		// because we set the route's timestamp to time.Now() on the interval
		// as part of pausing
		config.PruneStaleDropletsInterval = 1 * time.Second
		config.DropletStaleThreshold = 2 * config.PruneStaleDropletsInterval

		log.SetupLoggerFromConfig(config)

		mbusClient := yagnats.NewClient()
		registry := registry.NewCFRegistry(config, mbusClient)
		varz := varz.NewVarz(registry)
		router, err := NewRouter(config, mbusClient, registry, varz)
		Ω(err).ToNot(HaveOccurred())

		router.Run()

		staleCheckInterval := config.PruneStaleDropletsInterval
		staleThreshold := config.DropletStaleThreshold

		config.DropletStaleThreshold = staleThreshold

		zombieApp := test.NewGreetApp([]route.Uri{"zombie.vcap.me"}, proxyPort, mbusClient, nil)
		zombieApp.Listen()

		runningApp := test.NewGreetApp([]route.Uri{"innocent.bystander.vcap.me"}, proxyPort, mbusClient, nil)
		runningApp.Listen()

		Ω(waitAppRegistered(registry, zombieApp, 2*time.Second)).To(BeTrue())
		Ω(waitAppRegistered(registry, runningApp, 2*time.Second)).To(BeTrue())

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

		// Give enough time to register multiple times
		time.Sleep(heartbeatInterval * 3)

		// kill registration ticker => kill app (must be before stopping NATS since app.Register is fake and queues messages in memory)
		zombieTicker.Stop()

		nats.Stop()

		// Give router time to make a bad decision (i.e. prune routes)
		time.Sleep(staleCheckInterval + staleThreshold + 250*time.Millisecond)

		// While NATS is down no routes should go down
		zombieApp.VerifyAppStatus(200)
		runningApp.VerifyAppStatus(200)

		nats.Start()

		// Right after NATS starts up all routes should stay up
		zombieApp.VerifyAppStatus(200)
		runningApp.VerifyAppStatus(200)

		zombieGone := make(chan bool)

		go func() {
			for {
				// Finally the zombie is cleaned up. Maybe proactively enqueue Unregister events in DEA's.
				err := zombieApp.CheckAppStatus(404)
				if err != nil {
					time.Sleep(100 * time.Millisecond)
					continue
				}

				err = runningApp.CheckAppStatus(200)
				if err != nil {
					time.Sleep(100 * time.Millisecond)
					continue
				}

				zombieGone <- true

				break
			}
		}()

		waitTime := staleCheckInterval + staleThreshold + 5*time.Second
		Eventually(zombieGone, waitTime.Seconds()).Should(Receive())
	})
})
