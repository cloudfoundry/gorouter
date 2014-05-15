package test_util

import (
	. "github.com/onsi/gomega"

	"net"
	"strconv"
)

func NextAvailPort() uint16 {
	listener, err := net.Listen("tcp", ":0")
	Ω(err).ShouldNot(HaveOccurred())

	defer listener.Close()

	_, portStr, err := net.SplitHostPort(listener.Addr().String())
	Ω(err).ShouldNot(HaveOccurred())

	port, err := strconv.Atoi(portStr)
	Ω(err).ShouldNot(HaveOccurred())

	return uint16(port)
}
