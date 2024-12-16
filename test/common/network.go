package common

import (
	"bufio"
	"io"
	"net"

	. "github.com/onsi/gomega"
)

// TestUdp sets up a UDP listener which accepts the first connection and reads individual datagrams
// sent over it into the returned channel. The channel is buffered. The listen address is returned
// as well.
func TestUdp(done <-chan bool) (string, <-chan string) {
	conn, err := net.ListenUDP("udp", &net.UDPAddr{
		IP:   net.IP{127, 0, 0, 1},
		Port: 0,
	})
	Expect(err).NotTo(HaveOccurred())
	go closeDone(done, conn)

	out := make(chan string, 10)
	go func() {
		var (
			n   int
			err error
			buf = make([]byte, 65_535)
		)
		for err == nil {
			n, _, err = conn.ReadFrom(buf)
			out <- string(buf[:n])
		}
	}()

	return conn.LocalAddr().String(), out
}

// TestTcp sets up a TCP listener which accepts the first connection and reads individual lines
// sent over it into the returned channel. The channel is buffered. The listen address is returned
// as well.
func TestTcp(done <-chan bool) (string, <-chan string) {
	l, err := net.ListenTCP("tcp", &net.TCPAddr{
		IP:   net.IP{127, 0, 0, 1},
		Port: 0,
	})
	Expect(err).NotTo(HaveOccurred())
	go closeDone(done, l)

	out := make(chan string, 10)
	go func() {
		conn, err := l.Accept()
		Expect(err).NotTo(HaveOccurred())
		go closeDone(done, conn)

		scanner := bufio.NewScanner(conn)
		for scanner.Scan() {
			out <- scanner.Text()
		}
	}()

	return l.Addr().String(), out
}

func closeDone(done <-chan bool, closer io.Closer) {
	<-done
	_ = closer.Close()
}
