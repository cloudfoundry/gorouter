package router

import (
	"fmt"
	steno "github.com/cloudfoundry/gosteno"
	. "launchpad.net/gocheck"
	"os/exec"
	"strconv"
	"testing"
	"time"
)

func Test(t *testing.T) {
	config := &steno.Config{
		Sinks: []steno.Sink{},
		Codec: steno.NewJsonCodec(),
		Level: steno.LOG_INFO,
	}

	steno.Init(config)

	log = steno.NewLogger("test")

	TestingT(t)
}

func SpecConfig(natsPort, statusPort, proxyPort uint16) *Config {
	c := DefaultConfig()
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

	c.Status = StatusConfig{
		Port: statusPort,
		User: "user",
		Pass: "pass",
	}

	c.Nats = NatsConfig{
		Host: "localhost",
		Port: natsPort,
		User: "nats",
		Pass: "nats",
	}

	c.Logging = LoggingConfig{
		File:  "/dev/null",
		Level: "info",
	}

	return c
}

func StartNats(port int) *exec.Cmd {
	cmd := exec.Command("nats-server", "-p", strconv.Itoa(port), "--user", "nats", "--pass", "nats")
	err := cmd.Start()
	if err != nil {
		panic(fmt.Sprintf("NATS failed to start: %v\n", err))
	}

	return cmd
}

func StopNats(cmd *exec.Cmd) {
	cmd.Process.Kill()
	cmd.Wait()
}
