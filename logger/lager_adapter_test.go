package logger_test

import (
	log "code.cloudfoundry.org/gorouter/logger"
	"code.cloudfoundry.org/gorouter/test_util"
	"code.cloudfoundry.org/lager/v3"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gbytes"
	"go.uber.org/zap/zapcore"
)

var _ = Describe("LagerAdapter", func() {
	var (
		testSink     *test_util.TestSink
		prefix       = "my-prefix"
		component    = "my-component"
		subcomponent = "my-subcomponent"
		message      = "my-message"
		logKey       = "my-key"
		logValue     = "my-value"
		lagerLogger  lager.Logger
	)

	BeforeEach(func() {
		testSink = &test_util.TestSink{Buffer: gbytes.NewBuffer()}
		log.SetLoggingLevel("DEBUG")
		log.SetDynamicWriteSyncer(zapcore.NewMultiWriteSyncer(testSink, zapcore.AddSync(GinkgoWriter)))
		lagerLogger = log.NewLagerAdapter(prefix)

	})

	Describe("NewLagerAdapter", func() {
		Context("when logging messages with data", func() {
			It("adds outputs a properly formatted message", func() {
				lagerLogger.Info(message, lager.Data{logKey: logValue})
				Expect(testSink.Lines()).To(HaveLen(1))
				Expect(testSink.Lines()[0]).To(MatchRegexp(
					`{"log_level":[0-9]*,"timestamp":[0-9]+[.][0-9]+,"message":"%s","source":"%s","data":{"%s":"%s"}}`,
					message,
					prefix,
					logKey,
					logValue,
				))
			})
		})
	})

	Describe("Session", func() {
		Context("when calling Session oce", func() {
			It("adds the components as 'source' to the log's root", func() {
				lagerLogger = lagerLogger.Session(component)
				lagerLogger.Info(message)
				Expect(testSink.Lines()).To(HaveLen(1))
				Expect(testSink.Lines()[0]).To(MatchRegexp(
					`{"log_level":[0-9]*,"timestamp":[0-9]+[.][0-9]+,"message":"%s","source":"%s.%s","data":{}}`,
					message,
					prefix,
					component,
				))
			})
		})

		Context("when calling Session multiple times", func() {
			It("adds the concatenated components as 'source' to the log's root", func() {
				lagerLogger = lagerLogger.Session(component)
				lagerLogger = lagerLogger.Session(subcomponent)
				lagerLogger.Info(message)
				Expect(testSink.Lines()).To(HaveLen(1))
				Expect(testSink.Lines()[0]).To(MatchRegexp(
					`{"log_level":[0-9]*,"timestamp":[0-9]+[.][0-9]+,"message":"%s","source":"%s.%s.%s","data":{}}`,
					message,
					prefix,
					component,
					subcomponent,
				))
			})
		})

		Context("when calling Session with data", func() {
			It("adds the component as 'source' to the log's root, and provided data to the 'data' field", func() {
				lagerLogger = lagerLogger.Session(component, lager.Data{logKey: logValue})
				lagerLogger.Info(message)
				Expect(testSink.Lines()).To(HaveLen(1))
				Expect(testSink.Lines()[0]).To(MatchRegexp(
					`{"log_level":[0-9]*,"timestamp":[0-9]+[.][0-9]+,"message":"%s","source":"%s.%s","data":{"%s":"%s"}}`,
					message,
					prefix,
					component,
					logKey,
					logValue,
				))
			})
		})
	})

})
