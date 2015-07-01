package access_log_test

import (
	. "github.com/cloudfoundry/gorouter/access_log"

	"github.com/cloudfoundry/gorouter/config"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("AccessLog", func() {

	It("creates null access loger if no access log and loggregator is disabled", func() {
		config := config.DefaultConfig()

		Expect(CreateRunningAccessLogger(config)).To(BeAssignableToTypeOf(&NullAccessLogger{}))
	})

	It("creates an access log when loggegrator is enabled", func() {
		config := config.DefaultConfig()
		config.Logging.LoggregatorEnabled = true
		config.AccessLog = ""

		accessLogger, _ := CreateRunningAccessLogger(config)
		Expect(accessLogger.(*FileAndLoggregatorAccessLogger).FileWriter()).To(BeNil())
		Expect(accessLogger.(*FileAndLoggregatorAccessLogger).DropsondeSourceInstance()).To(Equal("0"))

	})

	It("creates an access log if an access log is specified", func() {
		config := config.DefaultConfig()
		config.AccessLog = "/dev/null"

		accessLogger, _ := CreateRunningAccessLogger(config)
		Expect(accessLogger.(*FileAndLoggregatorAccessLogger).FileWriter()).ToNot(BeNil())
		Expect(accessLogger.(*FileAndLoggregatorAccessLogger).DropsondeSourceInstance()).To(BeEmpty())

	})

	It("creates an AccessLogger if both access log and loggregator is enabled", func() {
		config := config.DefaultConfig()
		config.Logging.LoggregatorEnabled = true
		config.AccessLog = "/dev/null"

		accessLogger, _ := CreateRunningAccessLogger(config)
		Expect(accessLogger.(*FileAndLoggregatorAccessLogger).FileWriter()).ToNot(BeNil())
		Expect(accessLogger.(*FileAndLoggregatorAccessLogger).DropsondeSourceInstance()).ToNot(BeEmpty())

	})

	It("reports an error if the access log location is invalid", func() {
		config := config.DefaultConfig()
		config.AccessLog = "/this\\is/illegal"

		a, err := CreateRunningAccessLogger(config)
		Expect(err).To(HaveOccurred())
		Expect(a).To(BeNil())
	})
})
