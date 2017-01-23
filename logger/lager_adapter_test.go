package logger_test

import (
	"errors"

	"code.cloudfoundry.org/gorouter/logger/fakes"
	"code.cloudfoundry.org/lager"

	. "code.cloudfoundry.org/gorouter/logger"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/uber-go/zap"
)

var _ = Describe("LagerAdapter", func() {
	var (
		zapLogger   *fakes.FakeLogger
		lagerLogger lager.Logger
	)

	BeforeEach(func() {
		zapLogger = &fakes.FakeLogger{}
		lagerLogger = NewLagerAdapter(zapLogger)

		zapLogger.SessionReturns(zapLogger)
		zapLogger.WithReturns(zapLogger)
	})

	Describe("Session", func() {
		Context("when there is no data", func() {
			var sessionString = "test"
			It("sets the session on the original logger", func() {
				lagerLogger.Session("test")
				Expect(zapLogger.SessionCallCount()).To(Equal(1))
				Expect(zapLogger.SessionArgsForCall(0)).To(Equal(sessionString))
			})
		})

		Context("when there is data", func() {
			var sessionString = "test"
			It("sets the session on the original logger", func() {
				lagerLogger.Session("test", lager.Data{"foo": "bar", "bar": "baz"})

				Expect(zapLogger.SessionCallCount()).To(Equal(1))
				Expect(zapLogger.SessionArgsForCall(0)).To(Equal(sessionString))

				Expect(zapLogger.WithCallCount()).To(Equal(1))
				fields := zapLogger.WithArgsForCall(0)
				Expect(fields).To(HaveLen(2))
				Expect(fields).To(ConsistOf(zap.String("foo", "bar"), zap.String("bar", "baz")))
			})
		})
	})

	Describe("SessionName", func() {
		Context("when session has been called", func() {
			var (
				sessionName = "subcomponent"
			)

			It("provides the name of the session", func() {
				lagerLogger = lagerLogger.Session(sessionName)
				lagerLogger.SessionName()

				Expect(zapLogger.SessionNameCallCount()).To(Equal(1))
			})
		})

		Context("when session has not been called", func() {
			It("provides the name of the session", func() {
				lagerLogger.SessionName()

				Expect(zapLogger.SessionNameCallCount()).To(Equal(1))
			})
		})
	})

	Describe("Debug", func() {
		Context("when there is no data", func() {
			It("logs on the zapLogger at DebugLevel", func() {
				debugMessage := "my-debug-message"
				lagerLogger.Debug(debugMessage)
				Expect(zapLogger.DebugCallCount()).To(Equal(1))

				message, fields := zapLogger.DebugArgsForCall(0)
				Expect(message).To(Equal(debugMessage))
				Expect(fields).To(BeEmpty())
			})
		})

		Context("when there is data", func() {
			It("logs on the zapLogger at DebugLevel", func() {
				debugMessage := "my-debug-message"
				debugData := lager.Data{"foo": "bar", "bar": "baz"}
				lagerLogger.Debug(debugMessage, debugData)
				Expect(zapLogger.DebugCallCount()).To(Equal(1))

				message, fields := zapLogger.DebugArgsForCall(0)
				Expect(message).To(Equal(debugMessage))
				Expect(fields).To(HaveLen(2))
				Expect(fields).To(ConsistOf(zap.String("foo", "bar"), zap.String("bar", "baz")))
			})
		})
	})

	Describe("Info", func() {
		Context("when there is no data", func() {
			It("logs on the zapLogger at InfoLevel", func() {
				infoMessage := "my-info-message"
				lagerLogger.Info(infoMessage)
				Expect(zapLogger.InfoCallCount()).To(Equal(1))

				message, fields := zapLogger.InfoArgsForCall(0)
				Expect(message).To(Equal(infoMessage))
				Expect(fields).To(BeEmpty())
			})
		})

		Context("when there is data", func() {
			It("logs on the zapLogger at InfoLevel", func() {
				infoMessage := "my-info-message"
				infoData := lager.Data{"foo": "bar", "bar": "baz"}
				lagerLogger.Info(infoMessage, infoData)
				Expect(zapLogger.InfoCallCount()).To(Equal(1))

				message, fields := zapLogger.InfoArgsForCall(0)
				Expect(message).To(Equal(infoMessage))
				Expect(fields).To(HaveLen(2))
				Expect(fields).To(ConsistOf(zap.String("foo", "bar"), zap.String("bar", "baz")))
			})
		})
	})

	Describe("Error", func() {
		var err error

		BeforeEach(func() {
			err = errors.New("fake-error")
		})

		Context("when there is no data", func() {
			It("logs on the zapLogger at ErrorLevel", func() {
				errorMessage := "my-error-message"
				lagerLogger.Error(errorMessage, err)
				Expect(zapLogger.ErrorCallCount()).To(Equal(1))

				message, fields := zapLogger.ErrorArgsForCall(0)
				Expect(message).To(Equal(errorMessage))
				Expect(fields).To(HaveLen(1))
				Expect(fields[0]).To(Equal(zap.Error(err)))
			})
		})

		Context("when there is data", func() {
			It("logs on the zapLogger at ErrorLevel", func() {
				errorMessage := "my-error-message"
				errorData := lager.Data{"foo": "bar", "bar": "baz"}
				lagerLogger.Error(errorMessage, err, errorData)
				Expect(zapLogger.ErrorCallCount()).To(Equal(1))

				message, fields := zapLogger.ErrorArgsForCall(0)
				Expect(message).To(Equal(errorMessage))
				Expect(fields).To(HaveLen(3))
				Expect(fields).To(ConsistOf(
					zap.Error(err),
					zap.String("foo", "bar"),
					zap.String("bar", "baz"),
				))
			})
		})
	})

	Describe("Fatal", func() {
		var err error

		BeforeEach(func() {
			err = errors.New("fake-error")
		})

		Context("when there is no data", func() {
			It("logs on the zapLogger at FatalLevel", func() {
				errorMessage := "my-error-message"
				lagerLogger.Fatal(errorMessage, err)
				Expect(zapLogger.FatalCallCount()).To(Equal(1))

				message, fields := zapLogger.FatalArgsForCall(0)
				Expect(message).To(Equal(errorMessage))
				Expect(fields).To(HaveLen(1))
				Expect(fields[0]).To(Equal(zap.Error(err)))
			})
		})

		Context("when there is data", func() {
			It("logs on the zapLogger at FatalLevel", func() {
				errorMessage := "my-error-message"
				errorData := lager.Data{"foo": "bar", "bar": "baz"}
				lagerLogger.Fatal(errorMessage, err, errorData)
				Expect(zapLogger.FatalCallCount()).To(Equal(1))

				message, fields := zapLogger.FatalArgsForCall(0)
				Expect(message).To(Equal(errorMessage))
				Expect(fields).To(HaveLen(3))
				Expect(fields).To(ConsistOf(
					zap.Error(err),
					zap.String("foo", "bar"),
					zap.String("bar", "baz"),
				))
			})
		})
	})

	Describe("WithData", func() {
		It("returns the original logger with the new fields", func() {
			fields := lager.Data{"foo": "bar", "bar": "baz"}

			lagerLogger.WithData(fields)
			Expect(zapLogger.WithCallCount()).To(Equal(1))
			zapFields := zapLogger.WithArgsForCall(0)
			Expect(zapFields).To(HaveLen(2))
			Expect(zapFields).To(ConsistOf(zap.String("foo", "bar"), zap.String("bar", "baz")))
		})
	})
})
