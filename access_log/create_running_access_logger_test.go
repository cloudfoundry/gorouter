package access_log_test

import (
	"github.com/cloudfoundry/gorouter/config"

	. "github.com/cloudfoundry/gorouter/access_log"
	"github.com/pivotal-golang/lager"
	"github.com/pivotal-golang/lager/lagertest"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("AccessLog", func() {

	var (
		logger lager.Logger
		cfg    *config.Config
	)

	BeforeEach(func() {
		logger = lagertest.NewTestLogger("test")

		cfg = config.DefaultConfig()
	})

	It("creates null access loger if no access log and loggregator is disabled", func() {
		Expect(CreateRunningAccessLogger(logger, cfg)).To(BeAssignableToTypeOf(&NullAccessLogger{}))
	})

	It("creates an access log when loggegrator is enabled", func() {
		cfg.Logging.LoggregatorEnabled = true
		cfg.AccessLog.File = ""

		accessLogger, _ := CreateRunningAccessLogger(logger, cfg)
		Expect(accessLogger.(*FileAndLoggregatorAccessLogger).FileWriter()).To(BeNil())
		Expect(accessLogger.(*FileAndLoggregatorAccessLogger).WriterCount()).To(Equal(0))
		Expect(accessLogger.(*FileAndLoggregatorAccessLogger).DropsondeSourceInstance()).To(Equal("0"))
	})

	It("creates an access log if an access log is specified", func() {
		cfg.AccessLog.File = "/dev/null"

		accessLogger, _ := CreateRunningAccessLogger(logger, cfg)
		Expect(accessLogger.(*FileAndLoggregatorAccessLogger).FileWriter()).ToNot(BeNil())
		Expect(accessLogger.(*FileAndLoggregatorAccessLogger).DropsondeSourceInstance()).To(BeEmpty())
	})

	It("creates an AccessLogger if both access log and loggregator is enabled", func() {
		cfg.Logging.LoggregatorEnabled = true
		cfg.AccessLog.File = "/dev/null"

		accessLogger, _ := CreateRunningAccessLogger(logger, cfg)
		Expect(accessLogger.(*FileAndLoggregatorAccessLogger).FileWriter()).ToNot(BeNil())
		Expect(accessLogger.(*FileAndLoggregatorAccessLogger).WriterCount()).To(Equal(1))
		Expect(accessLogger.(*FileAndLoggregatorAccessLogger).DropsondeSourceInstance()).ToNot(BeEmpty())
	})

	It("should have two writers configured if access log file and enable_streaming are enabled", func() {
		cfg.Logging.LoggregatorEnabled = true
		cfg.AccessLog.File = "/dev/null"
		cfg.AccessLog.EnableStreaming = true

		accessLogger, _ := CreateRunningAccessLogger(logger, cfg)
		Expect(accessLogger.(*FileAndLoggregatorAccessLogger).FileWriter()).ToNot(BeNil())
		Expect(accessLogger.(*FileAndLoggregatorAccessLogger).WriterCount()).To(Equal(2))
		Expect(accessLogger.(*FileAndLoggregatorAccessLogger).DropsondeSourceInstance()).ToNot(BeEmpty())
	})

	It("should have one writer configured if access log file set but enable_streaming is disabled", func() {
		cfg.Logging.LoggregatorEnabled = true
		cfg.AccessLog.File = "/dev/null"
		cfg.AccessLog.EnableStreaming = false

		accessLogger, _ := CreateRunningAccessLogger(logger, cfg)
		Expect(accessLogger.(*FileAndLoggregatorAccessLogger).FileWriter()).ToNot(BeNil())
		Expect(accessLogger.(*FileAndLoggregatorAccessLogger).WriterCount()).To(Equal(1))
		Expect(accessLogger.(*FileAndLoggregatorAccessLogger).DropsondeSourceInstance()).ToNot(BeEmpty())
	})

	It("should have one writer configured if access log file not set but enable_streaming is enabled", func() {
		cfg.Logging.LoggregatorEnabled = true
		cfg.AccessLog.File = ""
		cfg.AccessLog.EnableStreaming = true

		accessLogger, _ := CreateRunningAccessLogger(logger, cfg)
		Expect(accessLogger.(*FileAndLoggregatorAccessLogger).FileWriter()).ToNot(BeNil())
		Expect(accessLogger.(*FileAndLoggregatorAccessLogger).WriterCount()).To(Equal(1))
		Expect(accessLogger.(*FileAndLoggregatorAccessLogger).DropsondeSourceInstance()).ToNot(BeEmpty())
	})

	It("reports an error if the access log location is invalid", func() {
		cfg.AccessLog.File = "/this\\is/illegal"

		a, err := CreateRunningAccessLogger(logger, cfg)
		Expect(err).To(HaveOccurred())
		Expect(a).To(BeNil())
	})
})
