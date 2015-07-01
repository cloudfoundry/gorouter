package test_util

import (
	. "github.com/onsi/gomega"
	"github.com/pivotal-golang/localip"
)

func NextAvailPort() uint16 {
	port, err := localip.LocalPort()
	Expect(err).ToNot(HaveOccurred())

	return port
}
