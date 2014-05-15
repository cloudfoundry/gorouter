package router_test

import (
	"net"
	"time"

	"github.com/cloudfoundry/gorouter/config"
	"github.com/cloudfoundry/gorouter/registry"
	"github.com/cloudfoundry/gorouter/test"
)

func SpecConfig(natsPort, statusPort, proxyPort uint16) *config.Config {
	c := config.DefaultConfig()

	c.Port = proxyPort
	c.Index = 2
	c.TraceKey = "my_trace_key"

	// Hardcode the IP to localhost to avoid leaving the machine while running tests
	c.Ip = "127.0.0.1"

	c.StartResponseDelayInterval = 10 * time.Millisecond
	c.PublishStartMessageIntervalInSeconds = 10
	c.PruneStaleDropletsInterval = 0
	c.DropletStaleThreshold = 0
	c.PublishActiveAppsInterval = 0

	c.EndpointTimeout = 500 * time.Millisecond

	c.Status = config.StatusConfig{
		Port: statusPort,
		User: "user",
		Pass: "pass",
	}

	c.Nats = []config.NatsConfig{
		{
			Host: "localhost",
			Port: natsPort,
			User: "nats",
			Pass: "nats",
		},
	}

	c.Logging = config.LoggingConfig{
		File:  "/dev/stderr",
		Level: "info",
	}

	return c
}

func waitMsgReceived(registry *registry.CFRegistry, app *test.TestApp, expectedToBeFound bool, timeout time.Duration) bool {
	interval := time.Millisecond * 50
	repetitions := int(timeout / interval)

	for j := 0; j < repetitions; j++ {
		received := true
		for _, url := range app.Urls() {
			_, ok := registry.Lookup(url)
			if ok != expectedToBeFound {
				received = false
				break
			}
		}
		if received {
			return true
		}
		time.Sleep(interval)
	}

	return false
}

func waitAppRegistered(registry *registry.CFRegistry, app *test.TestApp, timeout time.Duration) bool {
	return waitMsgReceived(registry, app, true, timeout)
}

func waitAppUnregistered(registry *registry.CFRegistry, app *test.TestApp, timeout time.Duration) bool {
	return waitMsgReceived(registry, app, false, timeout)
}

func timeoutDialler() func(net, addr string) (c net.Conn, err error) {
	return func(netw, addr string) (net.Conn, error) {
		c, err := net.DialTimeout(netw, addr, 10*time.Second)
		c.SetDeadline(time.Now().Add(2 * time.Second))
		return c, err
	}
}
