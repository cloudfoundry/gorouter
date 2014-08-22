package dropsonde_unmarshaller_test

import (
	"code.google.com/p/gogoprotobuf/proto"
	"github.com/cloudfoundry/dropsonde/dropsonde_unmarshaller"
	"github.com/cloudfoundry/dropsonde/events"
	"github.com/cloudfoundry/dropsonde/factories"
	"github.com/cloudfoundry/loggregatorlib/cfcomponent/instrumentation/testhelpers"
	"github.com/cloudfoundry/loggregatorlib/loggertesthelper"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("DropsondeUnmarshaller", func() {
	var (
		inputChan    chan []byte
		outputChan   chan *events.Envelope
		runComplete  chan struct{}
		unmarshaller dropsonde_unmarshaller.DropsondeUnmarshaller
	)

	Context("Unmarshall", func() {
		BeforeEach(func() {
			unmarshaller = dropsonde_unmarshaller.NewDropsondeUnmarshaller(loggertesthelper.Logger())
		})
		It("unmarshalls bytes", func() {
			input := &events.Envelope{
				Origin:    proto.String("fake-origin-3"),
				EventType: events.Envelope_Heartbeat.Enum(),
				Heartbeat: factories.NewHeartbeat(1, 2, 3),
			}
			message, _ := proto.Marshal(input)

			output, _ := unmarshaller.UnmarshallMessage(message)

			Expect(output).To(Equal(input))
		})
		It("handles bad input gracefully", func() {
			output, err := unmarshaller.UnmarshallMessage(make([]byte, 4))
			Expect(output).To(BeNil())
			Expect(err).To(HaveOccurred())
		})
	})

	Context("Run", func() {

		BeforeEach(func() {
			inputChan = make(chan []byte, 10)
			outputChan = make(chan *events.Envelope, 10)
			runComplete = make(chan struct{})
			unmarshaller = dropsonde_unmarshaller.NewDropsondeUnmarshaller(loggertesthelper.Logger())

			go func() {
				unmarshaller.Run(inputChan, outputChan)
				close(runComplete)
			}()
		})

		AfterEach(func() {
			close(inputChan)
			Eventually(runComplete).Should(BeClosed())
		})

		It("unmarshals bytes into envelopes", func() {
			envelope := &events.Envelope{
				Origin:    proto.String("fake-origin-3"),
				EventType: events.Envelope_Heartbeat.Enum(),
				Heartbeat: factories.NewHeartbeat(1, 2, 3),
			}
			message, _ := proto.Marshal(envelope)

			inputChan <- message
			outputEnvelope := <-outputChan
			Expect(outputEnvelope).To(Equal(envelope))
		})
	})

	Context("metrics", func() {
		BeforeEach(func() {
			inputChan = make(chan []byte, 10)
			outputChan = make(chan *events.Envelope, 10)
			runComplete = make(chan struct{})
			unmarshaller = dropsonde_unmarshaller.NewDropsondeUnmarshaller(loggertesthelper.Logger())

			go func() {
				unmarshaller.Run(inputChan, outputChan)
				close(runComplete)
			}()
		})

		AfterEach(func() {
			close(inputChan)
			Eventually(runComplete).Should(BeClosed())
		})
		It("emits the correct metrics context", func() {
			Expect(unmarshaller.Emit().Name).To(Equal("dropsondeUnmarshaller"))
		})

		It("emits a heartbeat counter", func() {
			envelope := &events.Envelope{
				Origin:    proto.String("fake-origin-3"),
				EventType: events.Envelope_Heartbeat.Enum(),
				Heartbeat: factories.NewHeartbeat(1, 2, 3),
			}
			message, _ := proto.Marshal(envelope)

			inputChan <- message
			testhelpers.EventuallyExpectMetric(unmarshaller, "heartbeatReceived", 1)
		})

		It("emits an unmarshal error counter", func() {
			inputChan <- []byte{1, 2, 3}
			testhelpers.EventuallyExpectMetric(unmarshaller, "unmarshalErrors", 1)
		})
	})
})
