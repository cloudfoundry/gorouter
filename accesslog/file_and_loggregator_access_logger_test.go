package accesslog_test

import (
	"bufio"
	"io/ioutil"
	"net"
	"net/http"
	"net/url"
	"os"
	"time"

	"code.cloudfoundry.org/gorouter/accesslog"
	"code.cloudfoundry.org/gorouter/accesslog/schema"
	schemaFakes "code.cloudfoundry.org/gorouter/accesslog/schema/fakes"
	"code.cloudfoundry.org/gorouter/config"
	"code.cloudfoundry.org/gorouter/logger"
	"code.cloudfoundry.org/gorouter/route"
	"code.cloudfoundry.org/gorouter/test_util"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("AccessLog", func() {

	Describe("LoggregatorAccessLogger", func() {
		var (
			logger logger.Logger
			cfg    *config.Config
			ls     *schemaFakes.FakeLogSender
		)

		Context("log sender", func() {
			BeforeEach(func() {
				logger = test_util.NewTestZapLogger("test")
				ls = &schemaFakes.FakeLogSender{}

				var err error
				cfg, err = config.DefaultConfig()
				Expect(err).ToNot(HaveOccurred())
			})

			It("logs", func() {
				cfg.Logging.LoggregatorEnabled = true
				cfg.Index = 42

				accessLogger, err := accesslog.CreateRunningAccessLogger(logger, ls, cfg)
				Expect(err).ToNot(HaveOccurred())

				record := *CreateAccessLogRecord()
				accessLogger.Log(record)

				Eventually(ls.SendAppLogCallCount).Should(Equal(1))
				appID, message, tags := ls.SendAppLogArgsForCall(0)
				Expect(appID).To(Equal("my_awesome_id"))
				Expect(message).To(MatchRegexp("^.*foo.bar.*"))
				Expect(tags).To(BeNil())

				accessLogger.Stop()
			})
		})

		Context("When created without access log file", func() {
			var (
				syslogServer net.Listener
				serverAddr   string
			)

			BeforeEach(func() {
				logger = test_util.NewTestZapLogger("test")
				ls = &schemaFakes.FakeLogSender{}

				var err error
				cfg, err = config.DefaultConfig()
				Expect(err).ToNot(HaveOccurred())

				syslogServer, err = net.Listen("tcp", ":0")
				Expect(err).NotTo(HaveOccurred())
				serverAddr = syslogServer.Addr().String()
			})

			AfterEach(func() {
				syslogServer.Close()
			})

			It("writes to the log file and Stdout", func() {
				cfg.Index = 42
				cfg.AccessLog.EnableStreaming = true
				cfg.Logging = config.LoggingConfig{
					Syslog:             "foo",
					SyslogAddr:         serverAddr,
					SyslogNetwork:      "tcp",
					LoggregatorEnabled: true,
				}

				accessLogger, err := accesslog.CreateRunningAccessLogger(logger, ls, cfg)
				Expect(err).ToNot(HaveOccurred())

				contents := make(chan string, 1)
				go runSyslogServer(syslogServer, contents)

				accessLogger.Log(*CreateAccessLogRecord())

				Eventually(contents).Should(Receive(ContainSubstring("foo.bar")))
				accessLogger.Stop()
			})
		})

		Context("when created with access log file", func() {
			BeforeEach(func() {
				logger = test_util.NewTestZapLogger("test")
				ls = &schemaFakes.FakeLogSender{}
				var err error
				cfg, err = config.DefaultConfig()
				Expect(err).ToNot(HaveOccurred())
			})

			It("writes to the log file and Stdout", func() {
				stdout, err := ioutil.TempFile("", "stdout")
				Expect(err).NotTo(HaveOccurred())
				defer os.Remove(stdout.Name())

				cfg.AccessLog.File = stdout.Name()
				accessLogger, err := accesslog.CreateRunningAccessLogger(logger, ls, cfg)
				Expect(err).ToNot(HaveOccurred())

				accessLogger.Log(*CreateAccessLogRecord())

				Eventually(func() (string, error) {
					b, err := ioutil.ReadFile(stdout.Name())
					return string(b), err
				}).Should(ContainSubstring("foo.bar"))

				accessLogger.Stop()
			})
		})

		Context("when DisableLogForwardedFor is set to true", func() {
			var (
				syslogServer net.Listener
				serverAddr   string
			)

			BeforeEach(func() {
				logger = test_util.NewTestZapLogger("test")
				ls = &schemaFakes.FakeLogSender{}

				var err error
				cfg, err = config.DefaultConfig()
				Expect(err).ToNot(HaveOccurred())

				syslogServer, err = net.Listen("tcp", ":0")
				Expect(err).NotTo(HaveOccurred())
				serverAddr = syslogServer.Addr().String()
			})

			AfterEach(func() {
				syslogServer.Close()
			})

			It("does not include X-Forwarded-For header in the records", func() {
				cfg.Index = 42
				cfg.AccessLog.EnableStreaming = true
				cfg.Logging = config.LoggingConfig{
					Syslog:                 "foo",
					SyslogAddr:             serverAddr,
					SyslogNetwork:          "tcp",
					LoggregatorEnabled:     true,
					DisableLogForwardedFor: true,
				}

				accessLogger, err := accesslog.CreateRunningAccessLogger(logger, ls, cfg)
				Expect(err).ToNot(HaveOccurred())

				contents := make(chan string, 1)
				go runSyslogServer(syslogServer, contents)

				accessLogger.Log(*CreateAccessLogRecord())

				Eventually(contents).Should(Receive(ContainSubstring(`x_forwarded_for:"-"`)))

				accessLogger.Stop()
			})
		})

		Context("when DisableLogSourceIP is set to true", func() {
			var (
				syslogServer net.Listener
				serverAddr   string
			)

			BeforeEach(func() {
				logger = test_util.NewTestZapLogger("test")
				ls = &schemaFakes.FakeLogSender{}

				var err error
				cfg, err = config.DefaultConfig()
				Expect(err).ToNot(HaveOccurred())

				syslogServer, err = net.Listen("tcp", ":0")
				Expect(err).NotTo(HaveOccurred())
				serverAddr = syslogServer.Addr().String()
			})

			AfterEach(func() {
				syslogServer.Close()
			})

			It("does not include RemoteAddr header in the records", func() {
				cfg.Index = 42
				cfg.AccessLog.EnableStreaming = true
				cfg.Logging = config.LoggingConfig{
					Syslog:             "foo",
					SyslogAddr:         serverAddr,
					SyslogNetwork:      "tcp",
					LoggregatorEnabled: true,
					DisableLogSourceIP: true,
				}

				accessLogger, err := accesslog.CreateRunningAccessLogger(logger, ls, cfg)
				Expect(err).ToNot(HaveOccurred())

				b := make(chan string, 1)
				go runSyslogServer(syslogServer, b)

				accessLogger.Log(*CreateAccessLogRecord())

				contents := <-b
				Expect(contents).NotTo(ContainSubstring("1.2.3.4:5678"))
				Expect(contents).To(ContainSubstring(`"user-agent" "-"`))

				accessLogger.Stop()
			})
		})

		Context("redactQueryParameters options", func() {
			var (
				syslogServer net.Listener
				serverAddr   string
			)

			BeforeEach(func() {
				logger = test_util.NewTestZapLogger("test")
				ls = &schemaFakes.FakeLogSender{}

				var err error
				cfg, err = config.DefaultConfig()
				Expect(err).ToNot(HaveOccurred())

				syslogServer, err = net.Listen("tcp", ":0")
				Expect(err).NotTo(HaveOccurred())
				serverAddr = syslogServer.Addr().String()
			})

			AfterEach(func() {
				syslogServer.Close()
			})

			Context("when redactQueryParameters is set to all", func() {
				It("does not include any query parameters in the records", func() {
					cfg.Index = 42
					cfg.AccessLog.EnableStreaming = true
					cfg.Logging = config.LoggingConfig{
						Syslog:             "foo",
						SyslogAddr:         serverAddr,
						SyslogNetwork:      "tcp",
						LoggregatorEnabled: true,
						RedactQueryParams:  "all",
					}

					accessLogger, err := accesslog.CreateRunningAccessLogger(logger, ls, cfg)
					Expect(err).ToNot(HaveOccurred())

					b := make(chan string, 1)
					go runSyslogServer(syslogServer, b)

					accessLogger.Log(*CreateAccessLogRecord())

					contents := <-b
					Expect(contents).NotTo(ContainSubstring("?wat"))

					accessLogger.Stop()
				})
			})
			Context("when redactQueryParameters is set to none", func() {
				It("does include all query parameters in the records", func() {
					cfg.Index = 42
					cfg.AccessLog.EnableStreaming = true
					cfg.Logging = config.LoggingConfig{
						Syslog:             "foo",
						SyslogAddr:         serverAddr,
						SyslogNetwork:      "tcp",
						LoggregatorEnabled: true,
						RedactQueryParams:  "none",
					}

					accessLogger, err := accesslog.CreateRunningAccessLogger(logger, ls, cfg)
					Expect(err).ToNot(HaveOccurred())

					b := make(chan string, 1)
					go runSyslogServer(syslogServer, b)

					accessLogger.Log(*CreateAccessLogRecord())

					contents := <-b
					Expect(contents).To(ContainSubstring("?wat"))

					accessLogger.Stop()
				})
			})
			Context("when redactQueryParameters is set to hash", func() {
				It("does hash all query parameters in the records", func() {
					cfg.Index = 42
					cfg.AccessLog.EnableStreaming = true
					cfg.Logging = config.LoggingConfig{
						Syslog:             "foo",
						SyslogAddr:         serverAddr,
						SyslogNetwork:      "tcp",
						LoggregatorEnabled: true,
						RedactQueryParams:  "hash",
					}

					accessLogger, err := accesslog.CreateRunningAccessLogger(logger, ls, cfg)
					Expect(err).ToNot(HaveOccurred())

					b := make(chan string, 1)
					go runSyslogServer(syslogServer, b)

					accessLogger.Log(*CreateAccessLogRecord())

					contents := <-b
					Expect(contents).To(ContainSubstring("?hash=a3bbe1a8f2f025b8b6c5b66937763bb2b9bebdf2"))

					accessLogger.Stop()
				})
			})

		})

		Measure("log write speed", func(b Benchmarker) {
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

	Describe("FileLogger", func() {
		var (
			baseLogger logger.Logger
			cfg        *config.Config
			ls         *schemaFakes.FakeLogSender
		)

		BeforeEach(func() {
			baseLogger = test_util.NewTestZapLogger("test")
			ls = &schemaFakes.FakeLogSender{}

			var err error
			cfg, err = config.DefaultConfig()
			Expect(err).ToNot(HaveOccurred())
		})

		It("creates null access loger if no access log and loggregator is disabled", func() {
			Expect(accesslog.CreateRunningAccessLogger(baseLogger, ls, cfg)).To(BeAssignableToTypeOf(&accesslog.NullAccessLogger{}))
		})

		It("does not creates an a file AccessLogger when only loggegrator is enabled", func() {
			cfg.Logging.LoggregatorEnabled = true
			cfg.AccessLog.File = ""
			cfg.AccessLog.EnableStreaming = false

			accessLogger, _ := accesslog.CreateRunningAccessLogger(baseLogger, ls, cfg)
			Expect(accessLogger.(*accesslog.FileAndLoggregatorAccessLogger).FileWriters()).To(BeEmpty())
			Expect(accessLogger.(*accesslog.FileAndLoggregatorAccessLogger).WriterCount()).To(Equal(0))
		})

		It("creates a file AccessLogger when an access log is specified", func() {
			cfg.AccessLog.File = "/dev/null"
			cfg.AccessLog.EnableStreaming = false

			accessLogger, _ := accesslog.CreateRunningAccessLogger(baseLogger, ls, cfg)
			Expect(accessLogger.(*accesslog.FileAndLoggregatorAccessLogger).FileWriters()).ToNot(BeEmpty())
			Expect(accessLogger.(*accesslog.FileAndLoggregatorAccessLogger).WriterCount()).To(Equal(1))
		})

		It("creates a file AccessLogger if both access log and loggregator is enabled", func() {
			cfg.Logging.LoggregatorEnabled = true
			cfg.AccessLog.File = "/dev/null"
			cfg.AccessLog.EnableStreaming = false

			accessLogger, _ := accesslog.CreateRunningAccessLogger(baseLogger, ls, cfg)
			Expect(accessLogger.(*accesslog.FileAndLoggregatorAccessLogger).FileWriters()).ToNot(BeEmpty())
			Expect(accessLogger.(*accesslog.FileAndLoggregatorAccessLogger).WriterCount()).To(Equal(1))
		})

		It("should have two writers configured if access log file and enable_streaming are enabled", func() {
			cfg.Logging.LoggregatorEnabled = true
			cfg.AccessLog.File = "/dev/null"
			cfg.AccessLog.EnableStreaming = true

			accessLogger, _ := accesslog.CreateRunningAccessLogger(baseLogger, ls, cfg)
			Expect(accessLogger.(*accesslog.FileAndLoggregatorAccessLogger).FileWriters()).ToNot(BeNil())
			Expect(accessLogger.(*accesslog.FileAndLoggregatorAccessLogger).WriterCount()).To(Equal(2))
		})

		It("should have one writer configured if access log file set but enable_streaming is disabled", func() {
			cfg.Logging.LoggregatorEnabled = true
			cfg.AccessLog.File = "/dev/null"
			cfg.AccessLog.EnableStreaming = false

			accessLogger, _ := accesslog.CreateRunningAccessLogger(baseLogger, ls, cfg)
			Expect(accessLogger.(*accesslog.FileAndLoggregatorAccessLogger).FileWriters()).ToNot(BeNil())
			Expect(accessLogger.(*accesslog.FileAndLoggregatorAccessLogger).WriterCount()).To(Equal(1))
		})

		It("should have one writer configured if access log file not set but enable_streaming is enabled", func() {
			cfg.Logging.LoggregatorEnabled = true
			cfg.AccessLog.File = ""
			cfg.AccessLog.EnableStreaming = true

			accessLogger, _ := accesslog.CreateRunningAccessLogger(baseLogger, ls, cfg)
			Expect(accessLogger.(*accesslog.FileAndLoggregatorAccessLogger).FileWriters()).ToNot(BeNil())
			Expect(accessLogger.(*accesslog.FileAndLoggregatorAccessLogger).WriterCount()).To(Equal(1))
		})

		It("reports an error if the access log location is invalid", func() {
			cfg.AccessLog.File = "/this\\is/illegal"

			a, err := accesslog.CreateRunningAccessLogger(baseLogger, ls, cfg)
			Expect(err).To(HaveOccurred())
			Expect(a).To(BeNil())
		})
	})
})

func runSyslogServer(l net.Listener, logContents chan<- string) {
	for {
		conn, err := l.Accept()
		if err != nil {
			return
		}

		go func(c net.Conn) {
			b := bufio.NewReader(c)
			s, err := b.ReadString('\n')
			if err != nil {
				c.Close()
				return
			}
			logContents <- s
		}(conn)
	}

}

func CreateAccessLogRecord() *schema.AccessLogRecord {
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

	b := route.NewEndpoint(&route.EndpointOpts{
		AppId:  "my_awesome_id",
		Host:   "127.0.0.1",
		Port:   4567,
		UseTLS: false,
	})

	r := schema.AccessLogRecord{
		Request:       req,
		StatusCode:    res.StatusCode,
		RouteEndpoint: b,
		ReceivedAt:    time.Unix(10, 100000000),
		FinishedAt:    time.Unix(10, 300000000),
		BodyBytesSent: 42,
	}

	return &r
}

type nullWriter struct{}

func (n nullWriter) Write(b []byte) (int, error) {
	return len(b), nil
}
