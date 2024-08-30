package accesslog_test

import (
	"code.cloudfoundry.org/gorouter/accesslog"
	"code.cloudfoundry.org/gorouter/accesslog/fakes"
	"code.cloudfoundry.org/gorouter/accesslog/schema"
	"code.cloudfoundry.org/gorouter/config"
	"code.cloudfoundry.org/gorouter/test_util"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"go.uber.org/zap/zapcore"
	"google.golang.org/protobuf/proto"
)

//go:generate counterfeiter -o fakes/eventemitter.go github.com/cloudfoundry/dropsonde.EventEmitter

var _ = Describe("DropsondeLogSender", func() {
	Describe("SendAppLog", func() {
		var (
			logSender    schema.LogSender
			conf         *config.Config
			eventEmitter *fakes.FakeEventEmitter
			logger       *test_util.TestLogger
		)

		BeforeEach(func() {
			var err error
			conf, err = config.DefaultConfig()
			Expect(err).ToNot(HaveOccurred())
			conf.Logging.LoggregatorEnabled = true
			eventEmitter = &fakes.FakeEventEmitter{}
			logger = test_util.NewTestLogger("test")

			logSender = accesslog.NewLogSender(conf, eventEmitter, logger.Logger)

			eventEmitter.OriginReturns("someOrigin")
		})

		It("emits an envelope", func() {
			logSender.SendAppLog("someID", "someMessage", nil)
			Expect(logger.Lines(zapcore.ErrorLevel)).To(HaveLen(0))
			Expect(eventEmitter.EmitEnvelopeCallCount()).To(Equal(1))
			logMessage := eventEmitter.EmitEnvelopeArgsForCall(0).LogMessage
			Expect(logMessage.AppId).To(Equal(proto.String("someID")))
			Expect(logMessage.Message).To(Equal([]byte("someMessage")))
		})

		It("emits an envelope with tags", func() {
			tags := map[string]string{
				"foo": "bar",
				"baz": "fuz",
			}
			logSender.SendAppLog("someID", "someMessage", tags)

			Expect(logger.Lines(zapcore.ErrorLevel)).To(HaveLen(0))
			Expect(eventEmitter.EmitEnvelopeCallCount()).To(Equal(1))
			envelope := eventEmitter.EmitEnvelopeArgsForCall(0)
			Expect(envelope.Tags).To(Equal(map[string]string{
				"foo": "bar",
				"baz": "fuz",
			}))
		})

		Context("when app id is empty", func() {
			It("does not emit an envelope", func() {
				logSender.SendAppLog("", "someMessage", nil)

				Expect(eventEmitter.EmitEnvelopeCallCount()).To(Equal(0))
			})
		})
	})
})
