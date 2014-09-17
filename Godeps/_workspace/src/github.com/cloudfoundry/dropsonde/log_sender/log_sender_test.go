package log_sender_test

import (
	"errors"
	"github.com/cloudfoundry/dropsonde/emitter/fake"
	"github.com/cloudfoundry/dropsonde/events"
	"github.com/cloudfoundry/dropsonde/log_sender"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("LogSender", func() {
	var (
		emitter *fake.FakeEventEmitter
		sender  log_sender.LogSender
	)

	BeforeEach(func() {
		emitter = fake.NewFakeEventEmitter("origin")
		sender = log_sender.NewLogSender(emitter)
	})

	It("sends a log message event to its emitter", func() {
		err := sender.SendAppLog("app-id", "custom-log-message", "App", "0")
		Expect(err).NotTo(HaveOccurred())

		Expect(emitter.Messages).To(HaveLen(1))
		log := emitter.Messages[0].Event.(*events.LogMessage)
		Expect(log.GetMessageType()).To(Equal(events.LogMessage_OUT))
		Expect(log.GetMessage()).To(BeEquivalentTo("custom-log-message"))
		Expect(log.GetAppId()).To(Equal("app-id"))
		Expect(log.GetSourceType()).To(Equal("App"))
		Expect(log.GetSourceInstance()).To(Equal("0"))
		Expect(log.GetTimestamp()).ToNot(BeNil())
	})

	It("sends a log error message event to its emitter", func() {
		err := sender.SendAppErrorLog("app-id", "custom-log-error-message", "App", "0")
		Expect(err).NotTo(HaveOccurred())

		Expect(emitter.Messages).To(HaveLen(1))
		log := emitter.Messages[0].Event.(*events.LogMessage)
		Expect(log.GetMessageType()).To(Equal(events.LogMessage_ERR))
		Expect(log.GetMessage()).To(BeEquivalentTo("custom-log-error-message"))
		Expect(log.GetAppId()).To(Equal("app-id"))
		Expect(log.GetSourceType()).To(Equal("App"))
		Expect(log.GetSourceInstance()).To(Equal("0"))
		Expect(log.GetTimestamp()).ToNot(BeNil())
	})

	Context("when messages cannot be emitted", func() {
		BeforeEach(func() {
			emitter.ReturnError = errors.New("expected error")
		})

		It("sends an error when log messages cannot be emitted", func() {
			err := sender.SendAppLog("app-id", "custom-log-message", "App", "0")
			Expect(err).To(HaveOccurred())
		})

		It("sends an error when log error messages cannot be emitted", func() {
			err := sender.SendAppErrorLog("app-id", "custom-log-error-message", "App", "0")
			Expect(err).To(HaveOccurred())
		})
	})
})
