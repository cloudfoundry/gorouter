package logger_test

import (
	. "code.cloudfoundry.org/gorouter/logger"
	"code.cloudfoundry.org/gorouter/test_util"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gbytes"
	"github.com/uber-go/zap"
)

// Zap defaults to Info Level
var _ = Describe("Logger", func() {
	var logger Logger
	var testSink *test_util.TestZapSink

	var component = "my-component"
	var action = "my-action"

	BeforeEach(func() {
		testSink = &test_util.TestZapSink{Buffer: gbytes.NewBuffer()}
		logger = NewLogger(
			component,
			zap.Output(zap.MultiWriteSyncer(testSink, zap.AddSync(GinkgoWriter))),
			zap.ErrorOutput(zap.MultiWriteSyncer(testSink, zap.AddSync(GinkgoWriter))))
	})

	var TestCommonLogFeatures = func(sourceString string) {
		It("outputs a properly-formatted message", func() {
			Expect(testSink.Lines()).To(HaveLen(1))

			Expect(testSink.Lines()[0]).To(MatchRegexp(
				"{\"log_level\":[0-9]*,\"timestamp\":.*,\"message\":\"%s\",\"source\":\"%s\".*}",
				action,
				sourceString,
			))
		})
	}

	Describe("Session", func() {
		BeforeEach(func() {
			logger = logger.Session("my-subcomponent")
		})

		Context("when session is originally called", func() {
			BeforeEach(func() {
				logger.Info(action)
			})
			TestCommonLogFeatures("my-component.my-subcomponent")
		})

		Context("when session is called multiple times", func() {
			BeforeEach(func() {
				logger = logger.Session("my-sub-subcomponent")
				logger.Info(action)
			})

			TestCommonLogFeatures("my-component.my-subcomponent.my-sub-subcomponent")
		})
	})

	Describe("SessionName", func() {
		Context("when session has never been called", func() {
			It("returns the original component", func() {
				Expect(logger.SessionName()).To(Equal(component))
			})
		})

		Context("when session has been called", func() {
			var subcomponent = "my-subcomponent"
			BeforeEach(func() {
				logger = logger.Session(subcomponent)
			})

			It("returns the current session", func() {
				var sessionName = component + "." + subcomponent
				Expect(logger.SessionName()).To(Equal(sessionName))
			})
		})
	})

	Describe("With", func() {
		var (
			fieldKey   string
			fieldValue string
		)

		BeforeEach(func() {
			fieldKey = "new-key"
			fieldValue = "new-value"

			logger = logger.With(zap.String(fieldKey, fieldValue))
			logger.Info(action)
		})

		It("returns a logger that adds that field to every log line", func() {
			Expect(testSink.Lines()).To(HaveLen(1))
			Expect(testSink.Lines()[0]).To(MatchRegexp("{.*\"new-key\":\"new-value\".*}"))
		})

		Context("when Session is called with the new Logger", func() {
			BeforeEach(func() {
				logger = logger.Session("session-id")
				logger.Info(action)
			})
			It("has only one source key in the log, with the context added from the call to With", func() {
				Expect(testSink.Lines()).To(HaveLen(2))
				Expect(testSink.Lines()[1]).To(MatchRegexp("{.*\"new-key\":\"new-value\".*}"))
				Expect(testSink.Lines()[1]).To(MatchRegexp("{.*\"source\":\"my-component.session-id\".*}"))
			})
		})
	})
})
