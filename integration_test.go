package router

import (
	mbus "github.com/cloudfoundry/go_cfmessagebus"
	"github.com/cloudfoundry/gorouter/test"
	. "launchpad.net/gocheck"
	"time"
)

type IntegrationSuite struct {
	Config     *Config
	mbusClient mbus.MessageBus
	router     *Router
}

var _ = Suite(&IntegrationSuite{})

func (s *IntegrationSuite) TestNatsConnectivity(c *C) {
	natsPort := nextAvailPort()
	cmd := StartNats(int(natsPort))

	proxyPort := nextAvailPort()
	statusPort := nextAvailPort()

	s.Config = SpecConfig(natsPort, statusPort, proxyPort)
	s.Config.PruneStaleDropletsInterval = 1 * time.Second

	s.router = NewRouter(s.Config)
	go s.router.Run()

	natsConnected := make(chan bool, 1)
	go func() {
		for {
			if s.router.mbusClient.Publish("Ping", []byte("data")) == nil {
				break
			}
			time.Sleep(500 * time.Millisecond)
		}
		natsConnected <- true
	}()

	<-natsConnected
	s.mbusClient = s.router.mbusClient

	heartbeatInterval := 200 * time.Millisecond

	// ensure the threshold is longer than the interval that we check;
	// because we set the route's timestamp to time.Now() on the interval
	// as part of pausing
	staleCheckInterval := s.router.registry.pruneStaleDropletsInterval
	staleThreshold := 2 * staleCheckInterval

	s.router.registry.dropletStaleThreshold = staleThreshold

	zombieApp := test.NewGreetApp([]string{"test.nats.dying.and.app.dies.while.nats.is.down.vcap.me"}, proxyPort, s.mbusClient, nil)
	zombieApp.Listen()

	runningApp := test.NewGreetApp([]string{"test.nats.dying.and.app.stays.alive.vcap.me"}, proxyPort, s.mbusClient, nil)
	runningApp.Listen()

	c.Assert(s.waitAppRegistered(zombieApp, 2*time.Second), Equals, true)
	c.Assert(s.waitAppRegistered(runningApp, 2*time.Second), Equals, true)

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

	zombieApp.VerifyAppStatus(200, c)

	// kill registration ticker => kill app (must be before stopping NATS since app.Register is fake and queues messages in memory)
	zombieTicker.Stop()

	StopNats(cmd)

	time.Sleep(staleCheckInterval + staleThreshold + 250*time.Millisecond)

	// While NATS is down no routes should go down
	zombieApp.VerifyAppStatus(200, c)
	runningApp.VerifyAppStatus(200, c)

	cmd = StartNats(int(natsPort))

	// Right after NATS starts up all routes should stay up
	zombieApp.VerifyAppStatus(200, c)
	runningApp.VerifyAppStatus(200, c)

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

	select {
	case <-zombieGone:
	case <-time.After(staleCheckInterval + staleThreshold + 5*time.Second):
		c.Error("Zombie app was not pruned.")
	}
}

func (s *IntegrationSuite) waitMsgReceived(a *test.TestApp, shouldBeRegistered bool, t time.Duration) bool {
	registeredOrUnregistered := make(chan bool)

	go func() {
		for {
			received := true
			for _, v := range a.Urls() {
				_, ok := s.router.registry.Lookup(v)
				if ok != shouldBeRegistered {
					received = false
					break
				}
			}

			if received {
				registeredOrUnregistered <- true
				break
			}

			time.Sleep(50 * time.Millisecond)
		}
	}()

	select {
	case <-registeredOrUnregistered:
		return true
	case <-time.After(t):
		return false
	}
}

func (s *IntegrationSuite) waitAppRegistered(app *test.TestApp, timeout time.Duration) bool {
	return s.waitMsgReceived(app, true, timeout)
}
