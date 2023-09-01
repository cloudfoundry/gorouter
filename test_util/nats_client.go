package test_util

import (
	"os"

	. "github.com/onsi/gomega"

	"errors"
	"fmt"
	"net"
	"os/exec"
	"strconv"
	"time"
)

type Nats struct {
	port    uint16
	cmd     *exec.Cmd
	address string
}

func NewNats(port uint16) *Nats {
	return &Nats{
		port:    port,
		address: fmt.Sprintf("127.0.0.1:%d", port),
	}
}

func (n *Nats) Port() uint16 {
	return n.port
}

func (n *Nats) Start() {
	natsServer, exists := os.LookupEnv("NATS_SERVER_BINARY")
	if !exists {
		fmt.Println("You need nats-server installed and set NATS_SERVER_BINARY env variable")
		os.Exit(1)
	}
	cmd := exec.Command(natsServer, "-p", strconv.Itoa(int(n.port)), "--user", "nats", "--pass", "nats")
	err := cmd.Start()
	Expect(err).ToNot(HaveOccurred())
	n.cmd = cmd

	err = n.waitUntilNatsUp()
	Expect(err).ToNot(HaveOccurred())
}

func (n *Nats) Stop() {
	n.cmd.Process.Kill()
	n.cmd.Wait()

	err := n.waitUntilNatsDown()
	Expect(err).ToNot(HaveOccurred())
}

func (n *Nats) waitUntilNatsUp() error {
	maxWait := 10
	for i := 0; i < maxWait; i++ {
		time.Sleep(500 * time.Millisecond)
		_, err := net.Dial("tcp", n.address)
		if err == nil {
			return nil
		}
	}

	return errors.New("Waited too long for NATS to start")
}

func (n *Nats) waitUntilNatsDown() error {
	maxWait := 10
	for i := 0; i < maxWait; i++ {
		time.Sleep(500 * time.Millisecond)
		_, err := net.Dial("tcp", n.address)
		if err != nil {
			return nil
		}
	}

	return errors.New("Waited too long for NATS to stop")
}
