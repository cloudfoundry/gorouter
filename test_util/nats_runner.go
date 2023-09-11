package test_util

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gexec"
)

type NATSRunner struct {
	port        int
	natsSession *gexec.Session
	natsUrls    []string
	MessageBus  *nats.Conn
}

func NewNATSRunner(port int) *NATSRunner {
	return &NATSRunner{
		port: port,
	}
}

func (runner *NATSRunner) Start() {
	if runner.natsSession != nil {
		panic("starting an already started NATS runner!!!")
	}

	natsServer, exists := os.LookupEnv("NATS_SERVER_BINARY")
	if !exists {
		fmt.Println("You need nats-server installed and set NATS_SERVER_BINARY env variable")
		os.Exit(1)
	}

	cmd := exec.Command(natsServer, "-p", strconv.Itoa(runner.port))
	sess, err := gexec.Start(
		cmd,
		gexec.NewPrefixedWriter("\x1b[32m[o]\x1b[34m[nats-server]\x1b[0m ", ginkgo.GinkgoWriter),
		gexec.NewPrefixedWriter("\x1b[91m[e]\x1b[34m[nats-server]\x1b[0m ", ginkgo.GinkgoWriter),
	)
	Expect(err).NotTo(HaveOccurred(), "Make sure to have nats-server on your path")

	runner.natsSession = sess

	Expect(err).NotTo(HaveOccurred())

	var messageBus *nats.Conn
	Eventually(func() error {
		messageBus, err = nats.Connect(fmt.Sprintf("nats://127.0.0.1:%d", runner.port))
		return err
	}, 5, 0.1).ShouldNot(HaveOccurred())

	runner.MessageBus = messageBus
}

func (runner *NATSRunner) Stop() {
	runner.KillWithFire()
}

func (runner *NATSRunner) KillWithFire() {
	if runner.natsSession != nil {
		runner.natsSession.Kill().Wait(5 * time.Second)
		runner.MessageBus = nil
		runner.natsSession = nil
	}
}
