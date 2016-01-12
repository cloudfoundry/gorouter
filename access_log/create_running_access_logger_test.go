package access_log_test

import (
	. "github.com/cloudfoundry/gorouter/access_log"
	"github.com/pivotal-golang/lager"
	"github.com/pivotal-golang/lager/lagertest"

	"github.com/cloudfoundry/gorouter/config"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("AccessLog", func() {

	var logger lager.Logger
	BeforeEach(func() {
		logger = lagertest.NewTestLogger("test")
	})

	It("creates null access loger if no access log and loggregator is disabled", func() {
		config := config.DefaultConfig(logger)

		Expect(CreateRunningAccessLogger(logger, config)).To(BeAssignableToTypeOf(&NullAccessLogger{}))
	})

	It("creates an access log when loggegrator is enabled", func() {
		config := config.DefaultConfig(logger)
		config.Logging.LoggregatorEnabled = true
		config.AccessLog = ""

		accessLogger, _ := CreateRunningAccessLogger(logger, config)
		Expect(accessLogger.(*FileAndLoggregatorAccessLogger).FileWriter()).To(BeNil())
		Expect(accessLogger.(*FileAndLoggregatorAccessLogger).DropsondeSourceInstance()).To(Equal("0"))

	})

	It("creates an access log if an access log is specified", func() {
		config := config.DefaultConfig(logger)
		config.AccessLog = "/dev/null"

		accessLogger, _ := CreateRunningAccessLogger(logger, config)
		Expect(accessLogger.(*FileAndLoggregatorAccessLogger).FileWriter()).ToNot(BeNil())
		Expect(accessLogger.(*FileAndLoggregatorAccessLogger).DropsondeSourceInstance()).To(BeEmpty())

	})

	It("creates an AccessLogger if both access log and loggregator is enabled", func() {
		config := config.DefaultConfig(logger)
		config.Logging.LoggregatorEnabled = true
		config.AccessLog = "/dev/null"

		accessLogger, _ := CreateRunningAccessLogger(logger, config)
		Expect(accessLogger.(*FileAndLoggregatorAccessLogger).FileWriter()).ToNot(BeNil())
		Expect(accessLogger.(*FileAndLoggregatorAccessLogger).DropsondeSourceInstance()).ToNot(BeEmpty())

	})

	It("reports an error if the access log location is invalid", func() {
		config := config.DefaultConfig(logger)
		config.AccessLog = "/this\\is/illegal"

		a, err := CreateRunningAccessLogger(logger, config)
		Expect(err).To(HaveOccurred())
		Expect(a).To(BeNil())
	})
})
