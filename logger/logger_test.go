package logger_test

import (
	"errors"
	"log/slog"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gbytes"
	"go.uber.org/zap/zapcore"

	log "code.cloudfoundry.org/gorouter/logger"
	"code.cloudfoundry.org/gorouter/test_util"
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
	BeforeEach(func() {
		testSink = &test_util.TestSink{Buffer: gbytes.NewBuffer()}
		log.SetTimeEncoder("epoch")
		log.SetDynamicWriteSyncer(zapcore.NewMultiWriteSyncer(testSink, zapcore.AddSync(GinkgoWriter)))
	})

	Describe("CreateLogger", func() {
		Context("when logger is created", func() {
			JustBeforeEach(func() {
				logger = log.CreateLogger()
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
			JustBeforeEach(func() {
				logger = log.CreateLoggerWithSource(prefix, "")
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
			JustBeforeEach(func() {
				logger = log.CreateLoggerWithSource(prefix, component)
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
			JustBeforeEach(func() {
				logger = log.CreateLogger()
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
			JustBeforeEach(func() {
				logger = log.CreateLogger()
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
			JustBeforeEach(func() {
				logger = log.CreateLogger()
				log.SetLoggingLevel("DEBUG")
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
			JustBeforeEach(func() {
				logger = log.CreateLogger()
				log.SetLoggingLevel("DEBUG")
				log.SetLoggingLevel("INFO")
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
			JustBeforeEach(func() {
				logger = log.CreateLogger()
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
			JustBeforeEach(func() {
				logger = log.CreateLogger()
			})
			It("outputs log messages with 'error' attribute", func() {
				err := errors.New("this-is-an-error")
				logger.Error(action, log.ErrAttr(err))

				Expect(testSink.Lines()).To(HaveLen(1))
				Expect(testSink.Lines()[0]).To(MatchRegexp(
					`{"log_level":3,"timestamp":[0-9]+[.][0-9]+,"message":"%s","data":{"error":"%s"}`, action, err.Error(),
				))
			})
		})
	})

	Describe("StructValue", func() {

		Context("when creating an slog value created by StructValue and json tag is present", func() {

			type Extras struct {
				Drink   string `json:"drink"`
				Dessert string `json:"dessert"`
			}

			type Menu struct {
				Menu   string `json:"menu"`
				Extras Extras `json:"extras"`
			}

			JustBeforeEach(func() {
				logger = log.CreateLogger()
			})
			It("takes the keys from json tags", func() {
				extras := Extras{
					Drink:   "coke",
					Dessert: "icecream",
				}
				menu := Menu{
					Menu:   "cheeseburger",
					Extras: extras,
				}
				logger.Info(action, slog.Any("order", log.StructValue(menu)))

				Expect(testSink.Lines()).To(HaveLen(1))
				Expect(testSink.Lines()[0]).To(MatchRegexp(
					`{"log_level":1,"timestamp":[0-9]+[.][0-9]+,"message":"%s","data":{"order":{"menu":"cheeseburger","extras":{"drink":"coke","dessert":"icecream"}}}}`, action,
				))
			})
		})

		Context("when creating an slog value created by StructValue and json tag is missing", func() {

			type Extras struct {
				Drink   string
				Dessert string
			}

			type Menu struct {
				Menu   string
				Extras Extras
			}

			JustBeforeEach(func() {
				logger = log.CreateLogger()
			})
			It("takes the keys from field names", func() {
				extras := Extras{
					Drink:   "coke",
					Dessert: "icecream",
				}
				menu := Menu{
					Menu:   "cheeseburger",
					Extras: extras,
				}
				logger.Info(action, slog.Any("order", log.StructValue(menu)))

				Expect(testSink.Lines()).To(HaveLen(1))
				Expect(testSink.Lines()[0]).To(MatchRegexp(
					`{"log_level":1,"timestamp":[0-9]+[.][0-9]+,"message":"%s","data":{"order":{"Menu":"cheeseburger","Extras":{"Drink":"coke","Dessert":"icecream"}}}}`, action,
				))
			})
		})

	})

})
