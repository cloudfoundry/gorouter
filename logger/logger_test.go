package logger_test

import (
	"fmt"

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
	var testField = zap.String("new-key", "new-value")

	BeforeEach(func() {
		testSink = &test_util.TestZapSink{Buffer: gbytes.NewBuffer()}
		logger = NewLogger(
			component,
			"unix-epoch",
			zap.DebugLevel,
			zap.Output(zap.MultiWriteSyncer(testSink, zap.AddSync(GinkgoWriter))),
			zap.ErrorOutput(zap.MultiWriteSyncer(testSink, zap.AddSync(GinkgoWriter))))
	})

	Describe("Session", func() {
		Context("when configured to use unix epoch formatting", func() {
			Context("when session is originally called", func() {
				BeforeEach(func() {
					logger = logger.Session("my-subcomponent")
					logger.Info(action)
				})

				It("outputs a properly-formatted message with human readable timestamp", func() {
					Expect(testSink.Lines()).To(HaveLen(1))

					Expect(testSink.Lines()[0]).To(MatchRegexp(
						`{"log_level":[0-9]*,"timestamp":[0-9]+[.][0-9]+,"message":"%s","source":"my-component.my-subcomponent".*}`,
						action,
					))
				})
			})

			Context("when session is called multiple times", func() {
				BeforeEach(func() {
					logger = logger.Session("my-sub-subcomponent")
					logger.Info(action)
				})

				It("outputs a properly-formatted message with human readable timestamp", func() {
					Expect(testSink.Lines()).To(HaveLen(1))

					Expect(testSink.Lines()[0]).To(MatchRegexp(
						`{"log_level":[0-9]*,"timestamp":[0-9]+[.][0-9]+,"message":"%s","source":"my-component.my-sub-subcomponent".*}`,
						action,
					))
				})
			})
		})

		Context("when configured to use RFC3339 formatting", func() {
			BeforeEach(func() {
				testSink = &test_util.TestZapSink{Buffer: gbytes.NewBuffer()}
				logger = NewLogger(
					component,
					"rfc3339",
					zap.DebugLevel,
					zap.Output(zap.MultiWriteSyncer(testSink, zap.AddSync(GinkgoWriter))),
					zap.ErrorOutput(zap.MultiWriteSyncer(testSink, zap.AddSync(GinkgoWriter))))
			})

			Context("when session is originally called", func() {
				BeforeEach(func() {
					logger = logger.Session("my-subcomponent")
					logger.Info(action)
				})

				It("outputs a properly-formatted message with human readable timestamp", func() {
					Expect(testSink.Lines()).To(HaveLen(1))

					Expect(testSink.Lines()[0]).To(MatchRegexp(
						`{"log_level":[0-9]*,"timestamp":"\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}\.\d{9}Z","message":"%s","source":"my-component.my-subcomponent".*}`,
						action,
					))
				})
			})

			Context("when session is called multiple times", func() {
				BeforeEach(func() {
					logger = logger.Session("my-sub-subcomponent")
					logger.Info(action)
				})

				It("outputs a properly-formatted message with human readable timestamp", func() {
					Expect(testSink.Lines()).To(HaveLen(1))

					Expect(testSink.Lines()[0]).To(MatchRegexp(
						`{"log_level":[0-9]*,"timestamp":"\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}\.\d{9}Z","message":"%s","source":"my-component.my-sub-subcomponent".*}`,
						action,
					))
				})
			})
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
		BeforeEach(func() {
			logger = logger.With(testField)
			logger.Info(action)
		})

		It("returns a logger that adds that field to every log line", func() {
			Expect(testSink.Lines()).To(HaveLen(1))
			Expect(testSink.Lines()[0]).To(MatchRegexp(`{.*"data":{"new-key":"new-value"}}`))
		})

		Context("when Session is called with the new Logger", func() {
			BeforeEach(func() {
				logger = logger.Session("session-id")
				logger.Info(action)
			})
			It("has only one source key in the log, with the context added from the call to With", func() {
				Expect(testSink.Lines()).To(HaveLen(2))
				Expect(testSink.Lines()[1]).To(MatchRegexp(`{.*"data":{.*"new-key":"new-value".*}`))
				Expect(testSink.Lines()[1]).To(MatchRegexp(`{.*"source":"my-component.session-id".*}`))
			})
		})
	})

	Describe("Log", func() {
		It("formats the log line correctly", func() {
			logger.Log(zap.InfoLevel, action, testField)
			Expect(testSink.Lines()).To(HaveLen(1))
			Expect(testSink.Lines()[0]).To(MatchRegexp(fmt.Sprintf(`{.*"message":"%s".*}`, action)))
			Expect(testSink.Lines()[0]).To(MatchRegexp(`{.*"log_level":1.*}`))
			Expect(testSink.Lines()[0]).To(MatchRegexp(`{.*"data":{"new-key":"new-value"}}`))
		})
	})
	Describe("Debug", func() {
		It("formats the log line correctly", func() {
			logger.Debug(action, testField)
			Expect(testSink.Lines()).To(HaveLen(1))
			Expect(testSink.Lines()[0]).To(MatchRegexp(fmt.Sprintf(`{.*"message":"%s".*}`, action)))
			Expect(testSink.Lines()[0]).To(MatchRegexp(`{.*"log_level":0.*}`))
			Expect(testSink.Lines()[0]).To(MatchRegexp(`{.*"data":{"new-key":"new-value"}}`))
		})
	})
	Describe("Info", func() {
		It("formats the log line correctly", func() {
			logger.Info(action, testField)
			Expect(testSink.Lines()).To(HaveLen(1))
			Expect(testSink.Lines()[0]).To(MatchRegexp(fmt.Sprintf(`{.*"message":"%s".*}`, action)))
			Expect(testSink.Lines()[0]).To(MatchRegexp(`{.*"log_level":1.*}`))
			Expect(testSink.Lines()[0]).To(MatchRegexp(`{.*"data":{"new-key":"new-value"}}`))
		})
	})
	Describe("Warn", func() {
		It("formats the log line correctly", func() {
			logger.Warn(action, testField)
			Expect(testSink.Lines()).To(HaveLen(1))
			Expect(testSink.Lines()[0]).To(MatchRegexp(fmt.Sprintf(`{.*"message":"%s".*}`, action)))
			Expect(testSink.Lines()[0]).To(MatchRegexp(`{.*"log_level":2.*}`))
			Expect(testSink.Lines()[0]).To(MatchRegexp(`{.*"data":{"new-key":"new-value"}}`))
		})
	})
})
