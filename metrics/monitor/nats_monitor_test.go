package monitor_test

import (
	"errors"
	"os"
	"time"

	"code.cloudfoundry.org/gorouter/logger"
	"code.cloudfoundry.org/gorouter/metrics/fakes"
	"code.cloudfoundry.org/gorouter/metrics/monitor"
	"code.cloudfoundry.org/gorouter/test_util"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gbytes"
	"github.com/tedsuo/ifrit"
)

var _ = Describe("NATSMonitor", func() {
	var (
		subscriber   *fakes.FakeSubscriber
		valueChainer *fakes.FakeValueChainer
		sender       *fakes.MetricSender
		ch           chan time.Time
		natsMonitor  *monitor.NATSMonitor
		logger       logger.Logger
		process      ifrit.Process
	)

	BeforeEach(func() {
		ch = make(chan time.Time)
		subscriber = new(fakes.FakeSubscriber)
		sender = new(fakes.MetricSender)
		valueChainer = new(fakes.FakeValueChainer)
		sender.ValueReturns(valueChainer)

		logger = test_util.NewTestZapLogger("test")

		natsMonitor = &monitor.NATSMonitor{
			Subscriber: subscriber,
			Sender:     sender,
			TickChan:   ch,
			Logger:     logger,
		}

		process = ifrit.Invoke(natsMonitor)
		Eventually(process.Ready()).Should(BeClosed())
	})

	It("exits when os signal is received", func() {
		process.Signal(os.Interrupt)
		var err error
		Eventually(process.Wait()).Should(Receive(&err))
		Expect(err).ToNot(HaveOccurred())
	})

	It("sends a metric on a time interval", func() {
		subscriber.PendingReturns(0, nil)
		ch <- time.Time{}
		ch <- time.Time{} // an extra tick is to make sure the time ticked at least once

		Expect(sender.ValueCallCount()).To(BeNumerically(">=", 1))
		name, _, unit := sender.ValueArgsForCall(0)
		Expect(name).To(Equal("buffered_messages"))
		Expect(unit).To(Equal(""))

		Expect(valueChainer.SendCallCount()).To(BeNumerically(">=", 1))
	})

	Context("when sending a metric fails", func() {
		BeforeEach(func() {
			valueChainer.SendReturns(errors.New("send failed"))
		})
		It("should log an error when Send fails", func() {
			ch <- time.Time{}
			ch <- time.Time{}

			Expect(logger).To(gbytes.Say("error-sending-nats-monitor-metric"))
		})
	})

	It("gets the number of queued messages for a given NATS subscription", func() {
		ch <- time.Time{}
		ch <- time.Time{}

		Expect(subscriber.PendingCallCount()).To(BeNumerically(">=", 1))
	})

	Context("when Pending returns a value", func() {
		BeforeEach(func() {
			subscriber.PendingReturns(1000, nil)
		})
		It("passes that value to the metric Sender", func() {
			ch <- time.Time{}
			ch <- time.Time{}

			Expect(sender.ValueCallCount()).To(BeNumerically(">=", 1))
			_, val, _ := sender.ValueArgsForCall(0)

			Expect(subscriber.PendingCallCount()).To(BeNumerically(">=", 1))
			Expect(val).To(Equal(float64(1000)))
		})
	})

	Context("when it fails to retrieve queued messages", func() {
		BeforeEach(func() {
			subscriber.PendingReturns(-1, errors.New("failed"))
		})

		It("should log an error when it fails to retrieve queued messages", func() {
			ch <- time.Time{}
			ch <- time.Time{}

			Expect(logger).To(gbytes.Say("error-retrieving-nats-subscription-pending-messages"))
		})
	})
})
