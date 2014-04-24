package main_test

import (
	"syscall"

	"github.com/cloudfoundry-incubator/candiedyaml"
	vcap "github.com/cloudfoundry/gorouter/common"
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

	startGorouterSession := func(cfgFile string) *Session {
		gorouterCmd := exec.Command(gorouterPath, "-c", cfgFile)
		session, err := Start(gorouterCmd, GinkgoWriter, GinkgoWriter)
		Ω(err).ShouldNot(HaveOccurred())
		Eventually(session, 5).Should(Say("gorouter.started"))
		gorouterSession = session

		return session
	}

	stopGorouter := func(gorouterSession *Session) {
		err := gorouterSession.Command.Process.Signal(syscall.SIGTERM)
		Ω(err).ShouldNot(HaveOccurred())
		gorouterSession.Wait(5 * time.Second)
	}

	BeforeEach(func() {
		var err error
		tmpdir, err = ioutil.TempDir("", "gorouter")
		Ω(err).ShouldNot(HaveOccurred())

		natsPort = test_util.NextAvailPort()
		natsRunner = natsrunner.NewNATSRunner(int(natsPort))
		natsRunner.Start()
	})

	AfterEach(func() {
		if natsRunner != nil {
			natsRunner.Stop()
		}

		os.RemoveAll(tmpdir)

		if gorouterSession != nil {
			stopGorouter(gorouterSession)
		}
	})

	It("has Nats connectivity", func() {
		localip, err := vcap.LocalIP()
		Ω(err).ShouldNot(HaveOccurred())

		proxyPort := test_util.NextAvailPort()
		statusPort := test_util.NextAvailPort()

		config := test_util.SpecConfig(natsPort, statusPort, proxyPort)

		// ensure the threshold is longer than the interval that we check,
		// because we set the route's timestamp to time.Now() on the interval
		// as part of pausing
		config.PruneStaleDropletsIntervalInSeconds = 1
		config.PruneStaleDropletsInterval = 1 * time.Second
		config.DropletStaleThresholdInSeconds = 2 * config.PruneStaleDropletsIntervalInSeconds
		config.DropletStaleThreshold = 2 * config.PruneStaleDropletsInterval

		config.StartResponseDelayIntervalInSeconds = 1

		cfgFile := filepath.Join(tmpdir, "config.yml")
		cfgBytes, err := candiedyaml.Marshal(config)

		Ω(err).ShouldNot(HaveOccurred())
		ioutil.WriteFile(cfgFile, cfgBytes, os.ModePerm)

		gorouterSession = startGorouterSession(cfgFile)

		mbusClient, err := newMessageBus(config)

		zombieApp := test.NewGreetApp([]route.Uri{"zombie.vcap.me"}, proxyPort, mbusClient, nil)
		zombieApp.Listen()

		runningApp := test.NewGreetApp([]route.Uri{"innocent.bystander.vcap.me"}, proxyPort, mbusClient, nil)
		runningApp.Listen()

		routesUri := fmt.Sprintf("http://%s:%s@%s:%d/routes", config.Status.User, config.Status.Pass, localip, statusPort)

		Ω(waitAppRegistered(routesUri, zombieApp, 2*time.Second)).To(BeTrue())
		Ω(waitAppRegistered(routesUri, runningApp, 2*time.Second)).To(BeTrue())

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

		natsRunner.Stop()

		staleCheckInterval := config.PruneStaleDropletsInterval
		staleThreshold := config.DropletStaleThreshold
		// Give router time to make a bad decision (i.e. prune routes)
		time.Sleep(staleCheckInterval + staleThreshold + 250*time.Millisecond)

		// While NATS is down no routes should go down
		zombieApp.VerifyAppStatus(200)
		runningApp.VerifyAppStatus(200)

		natsRunner.Start()

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

func newMessageBus(c *config.Config) (yagnats.NATSClient, error) {
	natsClient := yagnats.NewClient()
	natsMembers := []yagnats.ConnectionProvider{}

	for _, info := range c.Nats {
		natsMembers = append(natsMembers, &yagnats.ConnectionInfo{
			Addr:     fmt.Sprintf("%s:%d", info.Host, info.Port),
			Username: info.User,
			Password: info.Pass,
		})
	}

	err := natsClient.Connect(&yagnats.ConnectionCluster{
		Members: natsMembers,
	})

	return natsClient, err
}

func waitAppRegistered(routesUri string, app *test.TestApp, timeout time.Duration) bool {
	return waitMsgReceived(routesUri, app, true, timeout)
}

func waitAppUnregistered(routesUri string, app *test.TestApp, timeout time.Duration) bool {
	return waitMsgReceived(routesUri, app, false, timeout)
}

func waitMsgReceived(uri string, app *test.TestApp, expectedToBeFound bool, timeout time.Duration) bool {
	interval := time.Millisecond * 50
	repetitions := int(timeout / interval)

	for j := 0; j < repetitions; j++ {
		resp, err := http.Get(uri)
		if err == nil {
			switch resp.StatusCode {
			case http.StatusOK:
				bytes, err := ioutil.ReadAll(resp.Body)
				resp.Body.Close()
				Ω(err).ShouldNot(HaveOccurred())
				routes := make(map[string][]string)
				err = json.Unmarshal(bytes, &routes)
				Ω(err).ShouldNot(HaveOccurred())
				route := routes[string(app.Urls()[0])]
				if expectedToBeFound {
					if route != nil {
						return true
					}
				} else {
					if route == nil {
						return true
					}
				}
			default:
				println("Failed to receive routes: ", resp.StatusCode, uri)
			}
		}

		time.Sleep(interval)
	}

	return false
}
