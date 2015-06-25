package metrics_test

import (
	"os"
	"time"

	"github.com/cloudfoundry-incubator/routing-api/db"
	fake_db "github.com/cloudfoundry-incubator/routing-api/db/fakes"
	. "github.com/cloudfoundry-incubator/routing-api/metrics"
	fake_statsd "github.com/cloudfoundry-incubator/routing-api/metrics/fakes"
	"github.com/cloudfoundry/storeadapter"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("Metrics", func() {
	Describe("Watch", func() {

		var (
			database    *fake_db.FakeDB
			reporter    *MetricsReporter
			stats       *fake_statsd.FakePartialStatsdClient
			resultsChan chan storeadapter.WatchEvent
			sigChan     chan os.Signal
			readyChan   chan struct{}
			tickChan    chan time.Time
		)

		BeforeEach(func() {
			database = &fake_db.FakeDB{}
			stats = &fake_statsd.FakePartialStatsdClient{}

			tickChan = make(chan time.Time, 1)
			reporter = NewMetricsReporter(database, stats, &time.Ticker{C: tickChan})

			sigChan = make(chan os.Signal, 1)
			readyChan = make(chan struct{}, 1)
			resultsChan = make(chan storeadapter.WatchEvent, 1)
			database.WatchRouteChangesReturns(resultsChan, nil, nil)
			database.ReadRoutesReturns([]db.Route{
				db.Route{},
				db.Route{},
				db.Route{},
				db.Route{},
				db.Route{},
			}, nil)
		})

		JustBeforeEach(func() {
			go reporter.Run(sigChan, readyChan)
		})

		AfterEach(func() {
			sigChan <- nil
		})

		It("emits total_subscriptions on start", func() {
			Eventually(stats.GaugeCallCount).Should(Equal(1))

			totalStat, count, rate := stats.GaugeArgsForCall(0)
			Expect(totalStat).To(Equal("total_subscriptions"))
			Expect(count).To(BeNumerically("==", 0))
			Expect(rate).To(BeNumerically("==", 1.0))
		})

		It("periodically sends a delta of 0 to total_subscriptions", func() {
			tickChan <- time.Now()

			Eventually(stats.GaugeDeltaCallCount).Should(Equal(1))

			totalStat, count, rate := stats.GaugeDeltaArgsForCall(0)
			Expect(totalStat).To(Equal("total_subscriptions"))
			Expect(count).To(BeNumerically("==", 0))
			Expect(rate).To(BeNumerically("==", 1.0))
		})

		It("periodically gets total routes", func() {
			tickChan <- time.Now()

			Eventually(stats.GaugeCallCount).Should(Equal(2))

			totalStat, count, rate := stats.GaugeArgsForCall(1)
			Expect(totalStat).To(Equal("total_routes"))
			Expect(count).To(BeNumerically("==", 5))
			Expect(rate).To(BeNumerically("==", 1.0))
		})

		Context("When a create event happens", func() {
			BeforeEach(func() {
				storeNode := storeadapter.StoreNode{Value: []byte("valuable-string")}
				resultsChan <- storeadapter.WatchEvent{Type: storeadapter.UpdateEvent, Node: &storeNode}
			})

			It("increments the gauge", func() {
				Eventually(stats.GaugeDeltaCallCount).Should(Equal(1))

				updatedStat, count, rate := stats.GaugeDeltaArgsForCall(0)
				Expect(updatedStat).To(Equal("total_routes"))
				Expect(count).To(BeNumerically("==", 1))
				Expect(rate).To(BeNumerically("==", 1.0))
			})
		})

		Context("When a update event happens", func() {
			BeforeEach(func() {
				storeNode := storeadapter.StoreNode{Value: []byte("valuable-string")}
				prevNode := storeadapter.StoreNode{Value: []byte("older-valuable-string")}
				resultsChan <- storeadapter.WatchEvent{Type: storeadapter.UpdateEvent, Node: &storeNode, PrevNode: &prevNode}
			})

			It("doesn't modify the gauge", func() {
				Eventually(stats.GaugeDeltaCallCount).Should(Equal(1))

				updatedStat, count, rate := stats.GaugeDeltaArgsForCall(0)
				Expect(updatedStat).To(Equal("total_routes"))
				Expect(count).To(BeNumerically("==", 0))
				Expect(rate).To(BeNumerically("==", 1.0))
			})
		})

		Context("When a expire event happens", func() {
			BeforeEach(func() {
				storeNode := storeadapter.StoreNode{Value: []byte("valuable-string")}
				resultsChan <- storeadapter.WatchEvent{Type: storeadapter.ExpireEvent, Node: &storeNode}
			})

			It("decrements the gauge", func() {
				Eventually(stats.GaugeDeltaCallCount).Should(Equal(1))

				updatedStat, count, rate := stats.GaugeDeltaArgsForCall(0)
				Expect(updatedStat).To(Equal("total_routes"))
				Expect(count).To(BeNumerically("==", -1))
				Expect(rate).To(BeNumerically("==", 1.0))
			})
		})

		Context("When a delete event happens", func() {
			BeforeEach(func() {
				storeNode := storeadapter.StoreNode{Value: []byte("valuable-string")}
				resultsChan <- storeadapter.WatchEvent{Type: storeadapter.DeleteEvent, Node: &storeNode}
			})

			It("decrements the gauge", func() {
				Eventually(stats.GaugeDeltaCallCount).Should(Equal(1))

				updatedStat, count, rate := stats.GaugeDeltaArgsForCall(0)
				Expect(updatedStat).To(Equal("total_routes"))
				Expect(count).To(BeNumerically("==", -1))
				Expect(rate).To(BeNumerically("==", 1.0))
			})
		})
	})
})
