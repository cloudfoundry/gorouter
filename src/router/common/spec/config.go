package spec

import (
	"fmt"
	"router/config"
	"time"
)

func SpecConfig(natsPort, statusPort, proxyPort uint16) *config.Config {
	c := config.DefaultConfig()
	c.Port = proxyPort
	c.Index = 2
	c.TraceKey = "my_trace_key"

	// Hardcode the IP to localhost to avoid leaving the machine while running tests
	c.Ip = "127.0.0.1"

	c.PublishStartMessageInterval = 10 * time.Millisecond
	c.PruneStaleDropletsInterval = 0
	c.DropletStaleThreshold = 0
	c.PublishActiveAppsInterval = 0

	c.Status = config.StatusConfig{
		Port: statusPort,
		User: "user",
		Pass: "pass",
	}

	c.Nats = config.NatsConfig{
		Host: fmt.Sprintf("localhost:%d", natsPort),
	}

	c.Logging = config.LoggingConfig{
		File:  "/dev/null",
		Level: "info",
	}

	return c
}
