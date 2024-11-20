package syslog_test

import (
	"bytes"
	"fmt"
	"io"
	"net"
	"os"
	"strings"
	"testing"

	"code.cloudfoundry.org/gorouter/accesslog/syslog"

	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/format"
)

func init() {
	format.TruncatedDiff = false
}

func TestLogger(t *testing.T) {
	tests := []struct {
		name     string
		network  string
		severity syslog.Priority
		facility syslog.Priority
		appName  string
		message  string
		// Since the syslog message contains dynamic parts there is a bit of magic around this
		// variable. It has two formatting directives: the first is the hostname as a string, the
		// second the pid as an int. The timestamp will be cut from both the returned and the
		// provided output to not make this test depend on time.
		want string
	}{{
		"ensure UDP syslog works and the BOM is properly set",
		"udp",
		syslog.SeverityCrit,
		syslog.FacilityDaemon,
		"vcap.gorouter",
		"foobar",
		"<26>1 1970-01-01T00:00:00Z %s vcap.gorouter %d - \ufefffoobar",
	}, {
		"ensure UDP syslog does not mangle trailing newlines",
		"udp",
		syslog.SeverityCrit,
		syslog.FacilityFtp,
		"gorouter",
		"foobar\n",
		"<90>1 1970-01-01T00:00:00Z %s gorouter %d - \ufefffoobar\n",
	}, {
		"ensure TCP syslog appends a line feed at the end",
		"tcp",
		syslog.SeverityCrit,
		syslog.FacilityFtp,
		"gorouter",
		"foobar",
		"<90>1 1970-01-01T00:00:00Z %s gorouter %d - \ufefffoobar\n",
	}, {
		"ensure TCP syslog does not append additional line feeds at the end",
		"tcp",
		syslog.SeverityCrit,
		syslog.FacilityFtp,
		"gorouter",
		"foobar\n",
		"<90>1 1970-01-01T00:00:00Z %s gorouter %d - \ufefffoobar\n",
	}}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)

			var (
				addr   string
				result func() string
			)
			// we only support tcp and udp
			switch tt.network {
			case "tcp":
				addr, result = testTcp(t)
			case "udp":
				addr, result = testUdp(t)
			default:
				t.Fatalf("invalid network: %s", tt.network)
			}

			w, err := syslog.Dial(tt.network, addr, tt.severity, tt.facility, tt.appName)
			g.Expect(err).NotTo(HaveOccurred())
			defer func() { _ = w.Close() }()

			err = w.Log(tt.message)
			g.Expect(err).NotTo(HaveOccurred())

			want := fmt.Sprintf(tt.want, must(os.Hostname), os.Getpid())
			g.Eventually(func() string {
				return cutTimestamp(result())
			}).Should(Equal(cutTimestamp(want)))
		})
	}
}

// testUdp sets up a UDP listener which makes the payload of the first received datagram available
// via the returned function.
func testUdp(t *testing.T) (addr string, result func() string) {
	t.Helper()
	g := NewWithT(t)

	conn, err := net.ListenUDP("udp", &net.UDPAddr{
		IP:   net.IP{127, 0, 0, 1},
		Port: 0,
	})
	g.Expect(err).NotTo(HaveOccurred())

	out := make([]byte, 65507)
	read := 0
	go func() {
		defer conn.Close()
		read, _, _ = conn.ReadFrom(out)
	}()

	return conn.LocalAddr().String(), func() string {
		return string(out[:read])
	}
}

// testTcp sets up a TCP listener which accepts the first connection and makes data sent via that
// connection available via the returned function.
func testTcp(t *testing.T) (addr string, result func() string) {
	t.Helper()
	g := NewWithT(t)

	l, err := net.ListenTCP("tcp", &net.TCPAddr{
		IP:   net.IP{127, 0, 0, 1},
		Port: 0,
	})
	g.Expect(err).NotTo(HaveOccurred())

	out := &bytes.Buffer{}
	go func() {
		defer l.Close()

		conn, err := l.Accept()
		g.Expect(err).NotTo(HaveOccurred())
		defer conn.Close()

		_, _ = io.Copy(out, conn)
	}()

	return l.Addr().String(), func() string {
		return out.String()
	}
}

func cutTimestamp(in string) string {
	parts := strings.SplitN(in, " ", 3)
	if len(parts) < 3 {
		return ""
	}
	return parts[0] + " 1970-01-01T00:00:00Z " + parts[2]
}

func must[T any, F func() (T, error)](f F) T {
	t, err := f()
	if err != nil {
		panic(err.Error())
	}

	return t
}
