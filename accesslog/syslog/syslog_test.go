package syslog_test

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"code.cloudfoundry.org/gorouter/accesslog/syslog"
	"code.cloudfoundry.org/gorouter/test/common"

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
		"<90>1 1970-01-01T00:00:00Z %s gorouter %d - \ufefffoobar", // line feed is stripped, but if there is none at all the log will not be returned
	}, {
		"ensure TCP syslog does not append additional line feeds at the end",
		"tcp",
		syslog.SeverityCrit,
		syslog.FacilityFtp,
		"gorouter",
		"foobar\n",
		"<90>1 1970-01-01T00:00:00Z %s gorouter %d - \ufefffoobar",
	}}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			RegisterTestingT(t)
			done := make(chan bool)
			defer close(done)

			var (
				addr string
				logs <-chan string
			)
			// we only support tcp and udp
			switch tt.network {
			case "tcp":
				addr, logs = common.TestTcp(done)
			case "udp":
				addr, logs = common.TestUdp(done)
			default:
				t.Fatalf("invalid network: %s", tt.network)
			}

			w, err := syslog.Dial(tt.network, addr, tt.severity, tt.facility, tt.appName)
			Expect(err).NotTo(HaveOccurred())
			defer func() { _ = w.Close() }()

			err = w.Log(tt.message)
			Expect(err).NotTo(HaveOccurred())

			want := fmt.Sprintf(tt.want, must(os.Hostname), os.Getpid())
			Expect(cutTimestamp(<-logs)).To(Equal(cutTimestamp(want)))
		})
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
