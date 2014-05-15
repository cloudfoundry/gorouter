package log_test

import (
	"github.com/cloudfoundry/gorouter/config"
	. "github.com/cloudfoundry/gorouter/log"
	steno "github.com/cloudfoundry/gosteno"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("Log", func() {
	It("Setup logger from config", func() {
		cfg := config.DefaultConfig()
		cfg.Logging.File = "/tmp/gorouter.log"

		SetupLoggerFromConfig(cfg)

		count := Counter.GetCount("info")
		logger := steno.NewLogger("test")
		logger.Info("Hello")
		Ω(Counter.GetCount("info")).To(Equal(count + 1))
	})
})
