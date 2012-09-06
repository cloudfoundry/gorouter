package common

import (
	"fmt"
	"net"
	"os"
)

func LocalIP() (string, error) {
	addr, err := net.ResolveUDPAddr("udp", "1.2.3.4:1")
	if err != nil {
		return "", err
	}

	conn, err := net.DialUDP("udp", nil, addr)
	if err != nil {
		return "", err
	}

	host, _, err := net.SplitHostPort(conn.LocalAddr().String())
	if err != nil {
		return "", err
	}

	return host, nil
}

func GrabEphemeralPort() (string, error) {
	listener, err := net.Listen("tcp", ":0")
	if err != nil {
		return "", err
	}

	_, port, err := net.SplitHostPort(listener.Addr().String())

	listener.Close()

	return port, err
}

func GenerateUUID() string {
	file, _ := os.Open("/dev/urandom")
	b := make([]byte, 16)
	file.Read(b)
	file.Close()

	uuid := fmt.Sprintf("%x", b)
	return uuid
}
