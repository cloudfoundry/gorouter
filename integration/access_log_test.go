package integration

import (
	"bufio"
	"io"
	"net"
	"net/http"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Access Log", func() {
	var (
		testState *testState
		done      chan bool
		logs      <-chan string
	)

	BeforeEach(func() {
		testState = NewTestState()
	})

	JustBeforeEach(func() {
		testState.StartGorouterOrFail()
	})

	AfterEach(func() {
		testState.StopAndCleanup()
	})

	Context("when using syslog", func() {
		BeforeEach(func() {
			// disable file logging
			testState.cfg.AccessLog.EnableStreaming = true
			testState.cfg.AccessLog.File = ""
			// generic tag
			testState.cfg.Logging.Syslog = "gorouter"
		})

		Context("via UDP", func() {
			BeforeEach(func() {
				testState.cfg.Logging.SyslogNetwork = "udp"
				done = make(chan bool)
				testState.cfg.Logging.SyslogAddr, logs = testUdp(done)
			})

			AfterEach(func() {
				close(done)
			})

			It("properly emits access logs", func() {
				req := testState.newGetRequest("https://foobar.cloudfoundry.org")
				res, err := testState.client.Do(req)
				Expect(err).NotTo(HaveOccurred())
				Expect(res.StatusCode).To(Equal(http.StatusNotFound))

				log := <-logs

				Expect(log).To(ContainSubstring(`x_cf_routererror:"unknown_route"`))
				Expect(log).To(ContainSubstring(`"GET / HTTP/1.1" 404`))
				Expect(log).To(ContainSubstring("foobar.cloudfoundry.org"))

				// ensure we don't see any excess access logs
				Consistently(func() int { return len(logs) }).Should(Equal(0))
			})
		})

		Context("via TCP", func() {
			BeforeEach(func() {
				testState.cfg.Logging.SyslogNetwork = "tcp"
				done = make(chan bool)
				testState.cfg.Logging.SyslogAddr, logs = testTcp(done)
			})

			AfterEach(func() {
				close(done)
			})

			It("properly emits successful requests", func() {
				req := testState.newGetRequest("https://foobar.cloudfoundry.org")
				res, err := testState.client.Do(req)
				Expect(err).NotTo(HaveOccurred())
				Expect(res.StatusCode).To(Equal(http.StatusNotFound))

				log := <-logs

				Expect(log).To(ContainSubstring(`x_cf_routererror:"unknown_route"`))
				Expect(log).To(ContainSubstring(`"GET / HTTP/1.1" 404`))
				Expect(log).To(ContainSubstring("foobar.cloudfoundry.org"))

				// ensure we don't see any excess access logs
				Consistently(func() int { return len(logs) }).Should(Equal(0))
			})
		})
	})
})

// testUdp sets up a UDP listener which accepts the first connection and reads individual datagrams
// sent over it into the returned channel. The channel is buffered. The listen address is returned
// as well.
func testUdp(done <-chan bool) (string, <-chan string) {
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

// testTcp sets up a TCP listener which accepts the first connection and reads individual lines
// sent over it into the returned channel. The channel is buffered. The listen address is returned
// as well.
func testTcp(done <-chan bool) (string, <-chan string) {
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
