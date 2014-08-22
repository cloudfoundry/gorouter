package metric_sender_test

import (
	"errors"
	"github.com/cloudfoundry/dropsonde/emitter/fake"
	"github.com/cloudfoundry/dropsonde/events"
	"github.com/cloudfoundry/dropsonde/metric_sender"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("MetricSender", func() {
	var (
		emitter *fake.FakeEventEmitter
		sender  metric_sender.MetricSender
	)

	BeforeEach(func() {
		emitter = fake.NewFakeEventEmitter("origin")
		sender = metric_sender.NewMetricSender(emitter)
	})

	It("sends a metric to its emitter", func() {
		err := sender.SendValue("metric-name", 42, "answers")
		Expect(err).NotTo(HaveOccurred())

		Expect(emitter.Messages).To(HaveLen(1))
		metric := emitter.Messages[0].Event.(*events.ValueMetric)
		Expect(metric.GetName()).To(Equal("metric-name"))
		Expect(metric.GetValue()).To(BeNumerically("==", 42))
		Expect(metric.GetUnit()).To(Equal("answers"))
	})

	It("returns an error if it can't send metric value", func() {
		emitter.ReturnError = errors.New("some error")

		err := sender.SendValue("stuff", 12, "no answer")
		Expect(emitter.Messages).To(HaveLen(0))
		Expect(err.Error()).To(Equal("some error"))
	})

	It("sends an update counter event to its emitter", func() {
		err := sender.IncrementCounter("counter-strike")
		Expect(err).NotTo(HaveOccurred())

		Expect(emitter.Messages).To(HaveLen(1))
		counterEvent := emitter.Messages[0].Event.(*events.CounterEvent)
		Expect(counterEvent.GetName()).To(Equal("counter-strike"))

	})

	It("returns an error if it can't increment counter", func() {
		emitter.ReturnError = errors.New("some counter event error")

		err := sender.IncrementCounter("count me in")
		Expect(emitter.Messages).To(HaveLen(0))
		Expect(err.Error()).To(Equal("some counter event error"))
	})
})
