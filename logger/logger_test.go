package logger_test

import (
	"errors"
	"log/slog"

	log "code.cloudfoundry.org/gorouter/logger"
	"code.cloudfoundry.org/gorouter/test_util"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gbytes"
	"go.uber.org/zap/zapcore"
)

var _ = Describe("Logger", func() {
	var (
		logger    *slog.Logger
		testSink  *test_util.TestSink
		action    = "my-action"
		prefix    = "my-prefix"
		component = "my-component"
		logKey    = "my-key"
		logValue  = "my-value"
	)

	Describe("CreateLogger", func() {
		Context("when logger is created", func() {
			BeforeEach(func() {
				testSink = &test_util.TestSink{Buffer: gbytes.NewBuffer()}
				logger = log.CreateLogger()
				log.SetTimeEncoder("epoch")
				log.SetDynamicWriteSyncer(zapcore.NewMultiWriteSyncer(testSink, zapcore.AddSync(GinkgoWriter)))
			})
			It("outputs a properly-formatted message without source attribute", func() {
				logger.Info(action, slog.String(logKey, logValue))
				Expect(testSink.Lines()).To(HaveLen(1))

				Expect(testSink.Lines()[0]).To(MatchRegexp(
					`{"log_level":[0-9]*,"timestamp":[0-9]+[.][0-9]+,"message":"%s","data":{"%s":"%s"}}`,
					action, logKey, logValue,
				))
			})
		})
	})

	Describe("CreateLoggerWithSource", func() {
		Context("when prefix without component is provided", func() {
			BeforeEach(func() {
				testSink = &test_util.TestSink{Buffer: gbytes.NewBuffer()}
				logger = log.CreateLoggerWithSource(prefix, "")
				log.SetTimeEncoder("epoch")
				log.SetDynamicWriteSyncer(zapcore.NewMultiWriteSyncer(testSink, zapcore.AddSync(GinkgoWriter)))
			})
			It("outputs a properly-formatted message with prefix as source", func() {
				logger.Info(action, slog.String(logKey, logValue))
				Expect(testSink.Lines()).To(HaveLen(1))
				Expect(testSink.Lines()[0]).To(MatchRegexp(
					`{"log_level":[0-9]*,"timestamp":[0-9]+[.][0-9]+,"message":"%s","source":"%s","data":{"%s":"%s"}}`,
					action, prefix, logKey, logValue,
				))
			})
		})

		Context("when prefix and component are provided", func() {
			BeforeEach(func() {
				testSink = &test_util.TestSink{Buffer: gbytes.NewBuffer()}
				logger = log.CreateLoggerWithSource(prefix, component)
				log.SetTimeEncoder("epoch")
				log.SetDynamicWriteSyncer(zapcore.NewMultiWriteSyncer(testSink, zapcore.AddSync(GinkgoWriter)))
			})
			It("outputs a properly-formatted message with 'prefix.component' as source", func() {
				logger.Info(action, slog.String(logKey, logValue))
				Expect(testSink.Lines()).To(HaveLen(1))
				Expect(testSink.Lines()[0]).To(MatchRegexp(
					`{"log_level":[0-9]*,"timestamp":[0-9]+[.][0-9]+,"message":"%s","source":"%s.%s","data":{"%s":"%s"}}`,
					action, prefix, component, logKey, logValue,
				))
			})
		})
	})

	Describe("SetTimeEncoder", func() {
		Context("when rfc3339 is provided as time encoder", func() {
			BeforeEach(func() {
				testSink = &test_util.TestSink{Buffer: gbytes.NewBuffer()}
				logger = log.CreateLogger()
				log.SetDynamicWriteSyncer(zapcore.NewMultiWriteSyncer(testSink, zapcore.AddSync(GinkgoWriter)))
				log.SetTimeEncoder("rfc3339")
			})
			It("outputs a properly-formatted message with timestamp in rfc3339 format", func() {
				logger.Info(action, slog.String(logKey, logValue))
				Expect(testSink.Lines()).To(HaveLen(1))

				Expect(testSink.Lines()[0]).To(MatchRegexp(
					`{"log_level":[0-9]*,"timestamp":"\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}\.\d{9}Z","message":"%s","data":{"%s":"%s"}}`,
					action, logKey, logValue,
				))
			})
		})
		Context("when epoch is provided as time encoder", func() {
			BeforeEach(func() {
				testSink = &test_util.TestSink{Buffer: gbytes.NewBuffer()}
				logger = log.CreateLogger()
				log.SetDynamicWriteSyncer(zapcore.NewMultiWriteSyncer(testSink, zapcore.AddSync(GinkgoWriter)))
				log.SetTimeEncoder("rfc3339")
				log.SetTimeEncoder("epoch")
			})
			It("outputs a properly-formatted message with timestamp in epoch format", func() {
				logger.Info(action, slog.String(logKey, logValue))
				Expect(testSink.Lines()).To(HaveLen(1))

				Expect(testSink.Lines()[0]).To(MatchRegexp(
					`{"log_level":[0-9]*,"timestamp":[0-9]+[.][0-9]+,"message":"%s","data":{"%s":"%s"}}`,
					action, logKey, logValue,
				))
			})
		})
	})

	Describe("SetLoggingLevel", func() {
		Context("when DEBUG is provided as logging level", func() {
			BeforeEach(func() {
				testSink = &test_util.TestSink{Buffer: gbytes.NewBuffer()}
				logger = log.CreateLogger()
				log.SetDynamicWriteSyncer(zapcore.NewMultiWriteSyncer(testSink, zapcore.AddSync(GinkgoWriter)))
				log.SetLoggingLevel("DEBUG")
				log.SetTimeEncoder("epoch")
			})
			It("outputs messages with DEBUG level", func() {
				logger.Debug(action, slog.String(logKey, logValue))
				logger.Info(action, slog.String(logKey, logValue))

				Expect(testSink.Lines()).To(HaveLen(2))
				Expect(testSink.Lines()[0]).To(MatchRegexp(
					`{"log_level":0,"timestamp":[0-9]+[.][0-9]+,"message":"%s","data":{"%s":"%s"}}`,
					action, logKey, logValue,
				))
				Expect(testSink.Lines()[1]).To(MatchRegexp(
					`{"log_level":1,"timestamp":[0-9]+[.][0-9]+,"message":"%s","data":{"%s":"%s"}}`,
					action, logKey, logValue,
				))
			})
		})
		Context("when DEBUG is provided as logging level", func() {
			BeforeEach(func() {
				testSink = &test_util.TestSink{Buffer: gbytes.NewBuffer()}
				logger = log.CreateLogger()
				log.SetDynamicWriteSyncer(zapcore.NewMultiWriteSyncer(testSink, zapcore.AddSync(GinkgoWriter)))
				log.SetLoggingLevel("DEBUG")
				log.SetLoggingLevel("INFO")
				log.SetTimeEncoder("epoch")
			})
			It("only outputs messages with level INFO and above", func() {
				logger.Debug(action, slog.String(logKey, logValue))
				logger.Info(action, slog.String(logKey, logValue))
				Expect(testSink.Lines()).To(HaveLen(1))

				Expect(testSink.Lines()[0]).To(MatchRegexp(
					`{"log_level":1,"timestamp":[0-9]+[.][0-9]+,"message":"%s","data":{"%s":"%s"}}`,
					action, logKey, logValue,
				))
			})
		})
	})

	Describe("Panic", func() {
		Context("when an error is logged with 'Panic'", func() {
			BeforeEach(func() {
				testSink = &test_util.TestSink{Buffer: gbytes.NewBuffer()}
				logger = log.CreateLogger()
				log.SetDynamicWriteSyncer(zapcore.NewMultiWriteSyncer(testSink, zapcore.AddSync(GinkgoWriter)))
				log.SetTimeEncoder("epoch")
			})
			It("outputs an error log message and panics", func() {
				Expect(func() { log.Panic(logger, action) }).To(Panic())

				Expect(testSink.Lines()).To(HaveLen(1))
				Expect(testSink.Lines()[0]).To(MatchRegexp(
					`{"log_level":3,"timestamp":[0-9]+[.][0-9]+,"message":"%s"`,
					action,
				))
			})
		})
	})

	Describe("ErrAttr", func() {
		Context("when appending an error created by ErrAttr ", func() {
			BeforeEach(func() {
				testSink = &test_util.TestSink{Buffer: gbytes.NewBuffer()}
				logger = log.CreateLogger()
				log.SetDynamicWriteSyncer(zapcore.NewMultiWriteSyncer(testSink, zapcore.AddSync(GinkgoWriter)))
			})
			It("outputs log messages with 'error' attribute", func() {
				err := errors.New("this-is-an-error")
				logger.Error(action, log.ErrAttr(err))

				Expect(testSink.Lines()).To(HaveLen(1))
				Expect(testSink.Lines()[0]).To(MatchRegexp(
					`{"log_level":3,"timestamp":[0-9]+[.][0-9]+,"message":"%s","data":{"error":"%s"}}`, action, err.Error(),
				))
			})
		})
	})

})
