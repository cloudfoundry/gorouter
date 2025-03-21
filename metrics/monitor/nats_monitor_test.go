package monitor_test

import (
	"errors"
	"os"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gbytes"
	"github.com/tedsuo/ifrit"

	"code.cloudfoundry.org/gorouter/metrics/fakes"
	"code.cloudfoundry.org/gorouter/metrics/monitor"
	"code.cloudfoundry.org/gorouter/test_util"
)

var _ = Describe("NATSMonitor", func() {
	var (
		subscriber  *fakes.FakeSubscriber
		reporter    *fakes.FakeMetricReporter
		ch          chan time.Time
		natsMonitor *monitor.NATSMonitor
		logger      *test_util.TestLogger
		process     ifrit.Process
	)

	BeforeEach(func() {
		ch = make(chan time.Time)
		subscriber = new(fakes.FakeSubscriber)
		reporter = new(fakes.FakeMetricReporter)

		logger = test_util.NewTestLogger("test")

		natsMonitor = &monitor.NATSMonitor{
			Subscriber: subscriber,
			Reporter:   reporter,
			TickChan:   ch,
			Logger:     logger.Logger,
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

	It("sends a buffered_messages metric on a time interval", func() {
		subscriber.PendingReturns(1000, nil)
		ch <- time.Time{}
		ch <- time.Time{} // an extra tick is to make sure the time ticked at least once

		Expect(subscriber.PendingCallCount()).To(BeNumerically(">=", 1))
		Expect(reporter.CaptureNATSBufferedMessagesCallCount()).To(BeNumerically(">=", 1))
		messages := reporter.CaptureNATSBufferedMessagesArgsForCall(0)
		Expect(messages).To(Equal(1000))
	})

	It("sends a total_dropped_messages metric on a time interval", func() {
		subscriber.DroppedReturns(2000, nil)
		ch <- time.Time{}
		ch <- time.Time{} // an extra tick is to make sure the time ticked at least once

		Expect(subscriber.DroppedCallCount()).To(BeNumerically(">=", 1))
		Expect(reporter.CaptureNATSDroppedMessagesCallCount()).To(BeNumerically(">=", 1))
		messages := reporter.CaptureNATSDroppedMessagesArgsForCall(1)
		Expect(messages).To(Equal(2000))
	})

	Context("when it fails to retrieve queued messages", func() {
		BeforeEach(func() {
			subscriber.PendingReturns(-1, errors.New("failed"))
		})
		It("should log an error when it fails to retrieve queued messages", func() {
			ch <- time.Time{}
			ch <- time.Time{}

			Eventually(logger).Should(gbytes.Say("error-retrieving-nats-subscription-pending-messages"))
		})
	})

	Context("when it fails to retrieve dropped messages", func() {
		BeforeEach(func() {
			subscriber.DroppedReturns(-1, errors.New("failed"))
		})
		It("should log an error when it fails to retrieve queued messages", func() {
			ch <- time.Time{}
			ch <- time.Time{}

			Eventually(logger).Should(gbytes.Say("error-retrieving-nats-subscription-dropped-messages"))
		})
	})
})
