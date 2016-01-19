package access_log_test

import (
	"io/ioutil"
	"os"
	"path/filepath"

	"github.com/cloudfoundry/dropsonde/log_sender/fake"
	"github.com/cloudfoundry/dropsonde/logs"
	. "github.com/cloudfoundry/gorouter/access_log"
	"github.com/cloudfoundry/gorouter/route"
	"github.com/cloudfoundry/gorouter/test_util"
	"github.com/pivotal-golang/lager"
	"github.com/pivotal-golang/lager/lagertest"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	"net/http"
	"net/url"
	"time"
)

var _ = Describe("AccessLog", func() {

	var (
		logger lager.Logger
	)
	Context("with a dropsonde source instance", func() {

		BeforeEach(func() {
			logger = lagertest.NewTestLogger("test")

		})
		It("logs to dropsonde", func() {

			fakeLogSender := fake.NewFakeLogSender()
			logs.Initialize(fakeLogSender)
			accessLogger := NewFileAndLoggregatorAccessLogger(logger, "42")
			go accessLogger.Run()

			accessLogger.Log(*CreateAccessLogRecord())

			Eventually(fakeLogSender.GetLogs).Should(HaveLen(1))
			Expect(fakeLogSender.GetLogs()[0].AppId).To(Equal("my_awesome_id"))
			Expect(fakeLogSender.GetLogs()[0].Message).To(MatchRegexp("^.*foo.bar.*\n"))
			Expect(fakeLogSender.GetLogs()[0].SourceType).To(Equal("RTR"))
			Expect(fakeLogSender.GetLogs()[0].SourceInstance).To(Equal("42"))
			Expect(fakeLogSender.GetLogs()[0].MessageType).To(Equal("OUT"))

			accessLogger.Stop()
		})

		It("a record with no app id is not logged to dropsonde", func() {

			fakeLogSender := fake.NewFakeLogSender()
			logs.Initialize(fakeLogSender)

			accessLogger := NewFileAndLoggregatorAccessLogger(logger, "43")

			routeEndpoint := route.NewEndpoint("", "127.0.0.1", 4567, "", nil, -1, "")

			accessLogRecord := CreateAccessLogRecord()
			accessLogRecord.RouteEndpoint = routeEndpoint
			accessLogger.Log(*accessLogRecord)
			go accessLogger.Run()

			Consistently(fakeLogSender.GetLogs).Should(HaveLen(0))

			accessLogger.Stop()
		})

	})

	Context("created with access log file", func() {
		It("writes to the log file and Stdout", func() {
			var fakeAccessFile = new(test_util.FakeFile)
			fname := filepath.Join(os.TempDir(), "stdout")
			oldStdout := os.Stdout
			tempStdout, _ := os.Create(fname)
			defer tempStdout.Close()
			os.Stdout = tempStdout
			accessLogger := NewFileAndLoggregatorAccessLogger(logger, "", fakeAccessFile, os.Stdout)

			go accessLogger.Run()
			accessLogger.Log(*CreateAccessLogRecord())

			os.Stdout = oldStdout
			var stdoutPayload []byte
			Eventually(func() int {
				stdoutPayload, _ = ioutil.ReadFile(fname)
				return len(stdoutPayload)
			}).ShouldNot(Equal(0))
			Expect(string(stdoutPayload)).To(MatchRegexp("^.*foo.bar.*\n"))

			var payload []byte
			Eventually(func() int {
				n, _ := fakeAccessFile.Read(&payload)
				return n
			}).ShouldNot(Equal(0))
			Expect(string(payload)).To(MatchRegexp("^.*foo.bar.*\n"))

			accessLogger.Stop()
		})
	})

	Measure("Log write speed", func(b Benchmarker) {
		w := nullWriter{}

		b.Time("writeTime", func() {
			for i := 0; i < 500; i++ {
				r := CreateAccessLogRecord()
				r.WriteTo(w)
				r.WriteTo(w)
			}
		})
	}, 500)
})

func CreateAccessLogRecord() *AccessLogRecord {
	u, err := url.Parse("http://foo.bar:1234/quz?wat")
	if err != nil {
		panic(err)
	}

	req := &http.Request{
		Method:     "GET",
		URL:        u,
		Proto:      "HTTP/1.1",
		Header:     make(http.Header),
		Host:       "foo.bar",
		RemoteAddr: "1.2.3.4:5678",
	}

	req.Header.Set("Referer", "referer")
	req.Header.Set("User-Agent", "user-agent")

	res := &http.Response{
		StatusCode: http.StatusOK,
	}

	b := route.NewEndpoint("my_awesome_id", "127.0.0.1", 4567, "", nil, -1, "")

	r := AccessLogRecord{
		Request:       req,
		StatusCode:    res.StatusCode,
		RouteEndpoint: b,
		StartedAt:     time.Unix(10, 100000000),
		FirstByteAt:   time.Unix(10, 200000000),
		FinishedAt:    time.Unix(10, 300000000),
		BodyBytesSent: 42,
	}

	return &r
}

type nullWriter struct{}

func (n nullWriter) Write(b []byte) (int, error) {
	return len(b), nil
}
