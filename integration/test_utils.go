package integration

import (
	"crypto/tls"
	"io/ioutil"
	"os"
	"os/exec"
	"syscall"
	"time"

	"code.cloudfoundry.org/gorouter/config"
	"code.cloudfoundry.org/gorouter/test_util"

	yaml "gopkg.in/yaml.v2"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	. "github.com/onsi/gomega/gexec"
)

const defaultPruneInterval = 50 * time.Millisecond
const defaultPruneThreshold = 100 * time.Millisecond
const localIP = "127.0.0.1"

func createConfig(statusPort, proxyPort uint16, cfgFile string, pruneInterval time.Duration, pruneThreshold time.Duration, drainWait int, suspendPruning bool, maxBackendConns int64, natsPorts ...uint16) *config.Config {
	tempCfg := test_util.SpecConfig(statusPort, proxyPort, natsPorts...)

	configDrainSetup(tempCfg, pruneInterval, pruneThreshold, drainWait)

	tempCfg.SuspendPruningIfNatsUnavailable = suspendPruning
	tempCfg.LoadBalancerHealthyThreshold = 0
	tempCfg.OAuth = config.OAuthConfig{
		TokenEndpoint: "127.0.0.1",
		Port:          8443,
		ClientName:    "client-id",
		ClientSecret:  "client-secret",
		CACerts:       caCertsPath,
	}
	tempCfg.Backends.MaxConns = maxBackendConns

	writeConfig(tempCfg, cfgFile)
	return tempCfg
}

func configDrainSetup(cfg *config.Config, pruneInterval, pruneThreshold time.Duration, drainWait int) {
	// ensure the threshold is longer than the interval that we check,
	// because we set the route's timestamp to time.Now() on the interval
	// as part of pausing
	cfg.PruneStaleDropletsInterval = pruneInterval
	cfg.DropletStaleThreshold = pruneThreshold
	cfg.StartResponseDelayInterval = 1 * time.Second
	cfg.EndpointTimeout = 5 * time.Second
	cfg.EndpointDialTimeout = 10 * time.Millisecond
	cfg.DrainTimeout = 200 * time.Millisecond
	cfg.DrainWait = time.Duration(drainWait) * time.Second
}

func writeConfig(cfg *config.Config, cfgFile string) {
	cfgBytes, err := yaml.Marshal(cfg)
	Expect(err).ToNot(HaveOccurred())
	_ = ioutil.WriteFile(cfgFile, cfgBytes, os.ModePerm)
}

func startGorouterSession(cfgFile string) *Session {
	gorouterCmd := exec.Command(gorouterPath, "-c", cfgFile)
	session, err := Start(gorouterCmd, GinkgoWriter, GinkgoWriter)
	Expect(err).ToNot(HaveOccurred())
	var eventsSessionLogs []byte
	Eventually(func() string {
		logAdd, err := ioutil.ReadAll(session.Out)
		if err != nil {
			if session.ExitCode() >= 0 {
				Fail("gorouter quit early!")
			}
			return ""
		}
		eventsSessionLogs = append(eventsSessionLogs, logAdd...)
		return string(eventsSessionLogs)
	}, 70*time.Second).Should(SatisfyAll(
		ContainSubstring(`starting`),
		MatchRegexp(`Successfully-connected-to-nats.*localhost:\d+`),
		ContainSubstring(`gorouter.started`),
	))
	return session
}

func stopGorouter(gorouterSession *Session) {
	gorouterSession.Command.Process.Signal(syscall.SIGTERM)
	Eventually(gorouterSession, 5).Should(Exit(0))
}

func createCustomSSLConfig(onlyTrustClientCACerts bool, TLSClientConfigOption int, statusPort, proxyPort, sslPort uint16, natsPorts ...uint16) (*config.Config, *tls.Config) {
	tempCfg, clientTLSConfig := test_util.CustomSpecSSLConfig(onlyTrustClientCACerts, TLSClientConfigOption, statusPort, proxyPort, sslPort, natsPorts...)

	configDrainSetup(tempCfg, defaultPruneInterval, defaultPruneThreshold, 0)
	return tempCfg, clientTLSConfig
}

func createSSLConfig(statusPort, proxyPort, sslPort uint16, natsPorts ...uint16) (*config.Config, *tls.Config) {
	tempCfg, clientTLSConfig := test_util.SpecSSLConfig(statusPort, proxyPort, sslPort, natsPorts...)

	configDrainSetup(tempCfg, defaultPruneInterval, defaultPruneThreshold, 0)
	return tempCfg, clientTLSConfig
}

func createIsoSegConfig(statusPort, proxyPort uint16, cfgFile string, pruneInterval, pruneThreshold time.Duration, drainWait int, suspendPruning bool, isoSegs []string, natsPorts ...uint16) *config.Config {
	tempCfg := test_util.SpecConfig(statusPort, proxyPort, natsPorts...)

	configDrainSetup(tempCfg, pruneInterval, pruneThreshold, drainWait)

	tempCfg.SuspendPruningIfNatsUnavailable = suspendPruning
	tempCfg.LoadBalancerHealthyThreshold = 0
	tempCfg.OAuth = config.OAuthConfig{
		TokenEndpoint: "127.0.0.1",
		Port:          8443,
		ClientName:    "client-id",
		ClientSecret:  "client-secret",
		CACerts:       caCertsPath,
	}
	tempCfg.IsolationSegments = isoSegs

	writeConfig(tempCfg, cfgFile)
	return tempCfg
}
