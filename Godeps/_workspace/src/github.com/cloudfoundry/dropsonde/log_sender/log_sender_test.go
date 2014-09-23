package log_sender_test

import (
	"bytes"
	"errors"
	"github.com/cloudfoundry/dropsonde/emitter/fake"
	"github.com/cloudfoundry/dropsonde/events"
	"github.com/cloudfoundry/dropsonde/log_sender"
	"github.com/cloudfoundry/loggregatorlib/loggertesthelper"
	"io"
	"strings"

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
		sender = log_sender.NewLogSender(emitter, nil)
	})

	Describe("SendAppLog", func() {
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
	})

	Describe("SendAppErrorLog", func() {
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

	})

	Context("when messages cannot be emitted", func() {
		BeforeEach(func() {
			emitter.ReturnError = errors.New("expected error")
		})

		Describe("SendAppLog", func() {
			It("sends an error when log messages cannot be emitted", func() {
				err := sender.SendAppLog("app-id", "custom-log-message", "App", "0")
				Expect(err).To(HaveOccurred())
			})

		})

		Describe("SendAppErrorLog", func() {
			It("sends an error when log error messages cannot be emitted", func() {
				err := sender.SendAppErrorLog("app-id", "custom-log-error-message", "App", "0")
				Expect(err).To(HaveOccurred())
			})

		})
	})
})

var _ = Describe("ScanLogStream", func() {
	var (
		emitter *fake.FakeEventEmitter
		sender  log_sender.LogSender
	)

	BeforeEach(func() {
		emitter = fake.NewFakeEventEmitter("origin")
		sender = log_sender.NewLogSender(emitter, loggertesthelper.Logger())
	})

	It("sends lines from stream to emitter", func() {
		buf := bytes.NewBufferString("line 1\nline 2\n")

		sender.ScanLogStream("someId", "app", "0", buf, nil)

		messages := emitter.GetMessages()
		Expect(messages).To(HaveLen(2))

		log := emitter.Messages[0].Event.(*events.LogMessage)
		Expect(log.GetMessage()).To(BeEquivalentTo("line 1"))
		Expect(log.GetMessageType()).To(Equal(events.LogMessage_OUT))
		Expect(log.GetAppId()).To(Equal("someId"))
		Expect(log.GetSourceType()).To(Equal("app"))
		Expect(log.GetSourceInstance()).To(Equal("0"))

		log = emitter.Messages[1].Event.(*events.LogMessage)
		Expect(log.GetMessage()).To(BeEquivalentTo("line 2"))
		Expect(log.GetMessageType()).To(Equal(events.LogMessage_OUT))
	})

	It("emits an error message and reconnects on read errors", func() {
		var errReader fakeReader
		sender.ScanLogStream("someId", "app", "0", &errReader, nil)

		messages := emitter.GetMessages()
		Expect(messages).To(HaveLen(3))

		log := emitter.Messages[0].Event.(*events.LogMessage)
		Expect(log.GetMessageType()).To(Equal(events.LogMessage_OUT))
		Expect(log.GetMessage()).To(BeEquivalentTo("one"))

		log = emitter.Messages[1].Event.(*events.LogMessage)
		Expect(log.GetMessageType()).To(Equal(events.LogMessage_ERR))
		Expect(log.GetMessage()).To(ContainSubstring("Read Error"))

		log = emitter.Messages[2].Event.(*events.LogMessage)
		Expect(log.GetMessageType()).To(Equal(events.LogMessage_OUT))
		Expect(log.GetMessage()).To(BeEquivalentTo("two"))
	})

	It("stops when reader returns EOF", func(done Done) {
		var reader infiniteReader
		reader.stopChan = make(chan struct{})

		go func() {
			sender.ScanLogStream("someId", "app", "0", reader, nil)
			close(done)
		}()

		Eventually(func() int { return len(emitter.GetMessages()) }).Should(BeNumerically(">", 1))

		close(reader.stopChan)
	})

	It("stops when stopChan is closed", func() {
		var reader infiniteReader

		stopChan := make(chan struct{})
		done := make(chan struct{})
		go func() {
			sender.ScanLogStream("someId", "app", "0", reader, stopChan)
			close(done)
		}()

		Eventually(func() int { return len(emitter.GetMessages()) }).Should(BeNumerically(">", 1))

		close(stopChan)
		Eventually(done).Should(BeClosed())
	})

	It("drops over-length messages and resumes scanning", func(done Done) {
		// Scanner can't handle tokens over 64K
		bigReader := strings.NewReader(strings.Repeat("x", 64*1024+1) + "\nsmall message\n")
		sender.ScanLogStream("someId", "app", "0", bigReader, nil)

		Expect(emitter.GetMessages()).To(HaveLen(3))

		messages := emitter.GetMessages()

		Expect(getLogmessage(messages[0].Event)).To(ContainSubstring("Dropped log message due to read error:"))
		Expect(getLogmessage(messages[1].Event)).To(Equal("x"))
		Expect(getLogmessage(messages[2].Event)).To(Equal("small message"))
		close(done)
	})

	It("ignores empty lines", func() {
		reader := strings.NewReader("one\n\ntwo\n")

		sender.ScanLogStream("someId", "app", "0", reader, nil)

		Expect(emitter.GetMessages()).To(HaveLen(2))
		messages := emitter.GetMessages()

		Expect(getLogmessage(messages[0].Event)).To(Equal("one"))
		Expect(getLogmessage(messages[1].Event)).To(Equal("two"))
	})
})

var _ = Describe("ScanErrorLogStream", func() {
	var (
		emitter *fake.FakeEventEmitter
		sender  log_sender.LogSender
	)

	BeforeEach(func() {
		emitter = fake.NewFakeEventEmitter("origin")
		sender = log_sender.NewLogSender(emitter, loggertesthelper.Logger())
	})

	It("sends lines from stream to emitter", func() {
		buf := bytes.NewBufferString("line 1\nline 2\n")

		sender.ScanErrorLogStream("someId", "app", "0", buf, nil)

		messages := emitter.GetMessages()
		Expect(messages).To(HaveLen(2))

		log := emitter.Messages[0].Event.(*events.LogMessage)
		Expect(log.GetMessage()).To(BeEquivalentTo("line 1"))
		Expect(log.GetMessageType()).To(Equal(events.LogMessage_ERR))
		Expect(log.GetAppId()).To(Equal("someId"))
		Expect(log.GetSourceType()).To(Equal("app"))
		Expect(log.GetSourceInstance()).To(Equal("0"))

		log = emitter.Messages[1].Event.(*events.LogMessage)
		Expect(log.GetMessage()).To(BeEquivalentTo("line 2"))
		Expect(log.GetMessageType()).To(Equal(events.LogMessage_ERR))
	})

	It("emits an error message and reconnects on read errors", func() {
		var errReader fakeReader
		sender.ScanErrorLogStream("someId", "app", "0", &errReader, nil)

		messages := emitter.GetMessages()
		Expect(messages).To(HaveLen(3))

		log := emitter.Messages[0].Event.(*events.LogMessage)
		Expect(log.GetMessageType()).To(Equal(events.LogMessage_ERR))
		Expect(log.GetMessage()).To(BeEquivalentTo("one"))

		log = emitter.Messages[1].Event.(*events.LogMessage)
		Expect(log.GetMessageType()).To(Equal(events.LogMessage_ERR))
		Expect(log.GetMessage()).To(ContainSubstring("Read Error"))

		log = emitter.Messages[2].Event.(*events.LogMessage)
		Expect(log.GetMessageType()).To(Equal(events.LogMessage_ERR))
		Expect(log.GetMessage()).To(BeEquivalentTo("two"))
	})

	It("stops when reader returns EOF", func(done Done) {
		var reader infiniteReader
		reader.stopChan = make(chan struct{})

		go func() {
			sender.ScanErrorLogStream("someId", "app", "0", reader, nil)
			close(done)
		}()

		Eventually(func() int { return len(emitter.GetMessages()) }).Should(BeNumerically(">", 1))

		close(reader.stopChan)
	})

	It("stops when stopChan is closed", func() {
		var reader infiniteReader

		stopChan := make(chan struct{})
		done := make(chan struct{})
		go func() {
			sender.ScanErrorLogStream("someId", "app", "0", reader, stopChan)
			close(done)
		}()

		Eventually(func() int { return len(emitter.GetMessages()) }).Should(BeNumerically(">", 1))

		close(stopChan)
		Eventually(done).Should(BeClosed())
	})

	It("drops over-length messages and resumes scanning", func(done Done) {
		// Scanner can't handle tokens over 64K
		bigReader := strings.NewReader(strings.Repeat("x", 64*1024+1) + "\nsmall message\n")
		sender.ScanErrorLogStream("someId", "app", "0", bigReader, nil)

		Expect(emitter.GetMessages()).To(HaveLen(3))

		messages := emitter.GetMessages()

		Expect(getLogmessage(messages[0].Event)).To(ContainSubstring("Dropped log message due to read error:"))
		Expect(getLogmessage(messages[1].Event)).To(Equal("x"))
		Expect(getLogmessage(messages[2].Event)).To(Equal("small message"))
		close(done)
	})

	It("ignores empty lines", func() {
		reader := strings.NewReader("one\n\ntwo\n")

		sender.ScanErrorLogStream("someId", "app", "0", reader, nil)

		Expect(emitter.GetMessages()).To(HaveLen(2))
		messages := emitter.GetMessages()

		Expect(getLogmessage(messages[0].Event)).To(Equal("one"))
		Expect(getLogmessage(messages[1].Event)).To(Equal("two"))
	})
})

type fakeReader struct {
	counter int
}

func (f *fakeReader) Read(p []byte) (int, error) {
	f.counter++

	switch f.counter {
	case 1: // message
		return copy(p, "one\n"), nil
	case 2: // read error
		return 0, errors.New("Read Error")
	case 3: // message
		return copy(p, "two\n"), nil
	default: // eof
		return 0, io.EOF
	}
}

type infiniteReader struct {
	stopChan chan struct{}
}

func (i infiniteReader) Read(p []byte) (int, error) {
	select {
	case <-i.stopChan:
		return 0, io.EOF
	default:
	}

	return copy(p, "hello\n"), nil
}

func getLogmessage(e events.Event) string {
	log, ok := e.(*events.LogMessage)
	if !ok {
		panic("Could not cast to events.LogMessage")
	}
	return string(log.GetMessage())
}
