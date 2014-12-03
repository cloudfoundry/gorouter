package localip_test

import (
	"net"

	"github.com/pivotal-golang/localip"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("Localip", func() {
	Describe("LocalIP", func() {
		It("returns a local IP", func() {
			ip, err := localip.LocalIP()
			Ω(err).ShouldNot(HaveOccurred())

			// http://golang.org/pkg/net/#ParseIP
			// If s is not a valid textual representation of an IP address, ParseIP returns nil.
			Ω(net.ParseIP(ip)).ShouldNot(BeNil())
		})
	})

	Describe("LocalPort", func() {
		It("returns a local port", func() {
			port, err := localip.LocalPort()
			Ω(err).ShouldNot(HaveOccurred())
			Ω(port).Should(BeNumerically(">", 0))
		})
	})
})
