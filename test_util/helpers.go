package test_util

import (
	"path/filepath"
	"runtime"

	"github.com/cloudfoundry-incubator/uaa-token-fetcher"
	"github.com/cloudfoundry/gorouter/config"

	"time"

	. "github.com/onsi/gomega"
)

func SpecConfig(natsPort, statusPort, proxyPort uint16) *config.Config {
	return generateConfig(natsPort, statusPort, proxyPort)
}

func SpecSSLConfig(natsPort, statusPort, proxyPort, SSLPort uint16) *config.Config {
	c := generateConfig(natsPort, statusPort, proxyPort)

	c.EnableSSL = true

	_, filename, _, _ := runtime.Caller(0)
	testPath, err := filepath.Abs(filepath.Join(filename, "..", "..", "test", "assets"))
	Expect(err).NotTo(HaveOccurred())

	c.SSLKeyPath = filepath.Join(testPath, "private.pem")
	c.SSLCertPath = filepath.Join(testPath, "public.pem")
	c.SSLPort = SSLPort

	return c
}

func generateConfig(natsPort, statusPort, proxyPort uint16) *config.Config {
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
	c.Zone = "z1"

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
		File:          "/dev/stdout",
		Level:         "info",
		MetronAddress: "localhost:3457",
		JobName:       "router_test_z1_0",
	}

	c.OAuth = token_fetcher.OAuthConfig{
		TokenEndpoint: "http://localhost",
		Port:          8080,
	}

	c.RouteServiceSecret = "kCvXxNMB0JO2vinxoru9Hg=="

	return c
}
