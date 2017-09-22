package test_util

import (
	. "github.com/onsi/gomega"

	"code.cloudfoundry.org/localip"

	"fmt"
	"sync"
	"time"
)

var portLockedTime = 2 * time.Second

type UsedPorts struct {
	sync.RWMutex
	portSet map[uint16]bool
}

var usedPorts *UsedPorts

func NextAvailPort() uint16 {
	if usedPorts == nil {
		usedPorts = &UsedPorts{
			portSet: make(map[uint16]bool),
		}
	}

	var port uint16
	var err error
	for {
		port, err = localip.LocalPort()
		Expect(err).ToNot(HaveOccurred())
		usedPorts.Lock()
		if ok, _ := usedPorts.portSet[port]; !ok {
			usedPorts.portSet[port] = true
			usedPorts.Unlock()
			go func() {
				time.Sleep(portLockedTime)
				FreePort(port)
			}()
			return port
		} else {
			fmt.Printf("Port %d was taken, looking for a new one\n", port)
			usedPorts.Unlock()
		}
	}
}

func FreePort(port uint16) {
	usedPorts.Lock()
	delete(usedPorts.portSet, port)
	usedPorts.Unlock()
}
