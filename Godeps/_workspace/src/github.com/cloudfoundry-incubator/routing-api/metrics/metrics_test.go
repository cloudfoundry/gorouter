package metrics_test

import (
	"os"
	"time"

	"github.com/cloudfoundry-incubator/routing-api/db"
	fake_db "github.com/cloudfoundry-incubator/routing-api/db/fakes"
	. "github.com/cloudfoundry-incubator/routing-api/metrics"
	fake_statsd "github.com/cloudfoundry-incubator/routing-api/metrics/fakes"
	"github.com/cloudfoundry-incubator/routing-api/models"
	"github.com/coreos/etcd/Godeps/_workspace/src/golang.org/x/net/context"
	"github.com/coreos/etcd/client"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("Metrics", func() {
	Describe("Watch", func() {

		var (
			database       *fake_db.FakeDB
			reporter       *MetricsReporter
			stats          *fake_statsd.FakePartialStatsdClient
			resultsChan    chan db.Event
			tcpResultsChan chan db.Event
			sigChan        chan os.Signal
			readyChan      chan struct{}
			tickChan       chan time.Time
		)

		BeforeEach(func() {
			database = &fake_db.FakeDB{}
			stats = &fake_statsd.FakePartialStatsdClient{}

			tickChan = make(chan time.Time, 1)
			reporter = NewMetricsReporter(database, stats, &time.Ticker{C: tickChan})

			sigChan = make(chan os.Signal, 1)
			readyChan = make(chan struct{}, 1)
			resultsChan = make(chan db.Event, 1)
			tcpResultsChan = make(chan db.Event, 1)
			database.WatchRouteChangesStub = func(filter string) (<-chan db.Event, <-chan error, context.CancelFunc) {
				if filter == db.HTTP_ROUTE_BASE_KEY {
					return resultsChan, nil, nil
				} else {
					return tcpResultsChan, nil, nil
				}
			}
			database.ReadRoutesReturns([]models.Route{
				models.Route{},
				models.Route{},
				models.Route{},
				models.Route{},
				models.Route{},
			}, nil)

			database.ReadTcpRouteMappingsReturns([]models.TcpRouteMapping{
				models.TcpRouteMapping{},
				models.TcpRouteMapping{},
				models.TcpRouteMapping{},
			}, nil)
		})

		JustBeforeEach(func() {
			go reporter.Run(sigChan, readyChan)
		})

		AfterEach(func() {
			sigChan <- nil
		})

		verifyGaugeCall := func(statKey string, expectedCount int64, expectedRate float32, index int) {
			totalStat, count, rate := stats.GaugeArgsForCall(index)
			Expect(totalStat).To(Equal(statKey))
			Expect(count).To(BeNumerically("==", expectedCount))
			Expect(rate).To(BeNumerically("==", expectedRate))
		}

		verifyGaugeDeltaCall := func(statKey string, expectedCount int64, expectedRate float32, index int) {
			totalStat, count, rate := stats.GaugeDeltaArgsForCall(index)
			Expect(totalStat).To(Equal(statKey))
			Expect(count).To(BeNumerically("==", expectedCount))
			Expect(rate).To(BeNumerically("==", expectedRate))
		}

		It("emits total_http_subscriptions on start", func() {
			Eventually(stats.GaugeCallCount).Should(Equal(2))
			verifyGaugeCall(TotalHttpSubscriptions, 0, 1.0, 0)
			verifyGaugeCall(TotalTcpSubscriptions, 0, 1.0, 1)
		})

		It("periodically sends a delta of 0 to total_http_subscriptions", func() {
			tickChan <- time.Now()

			Eventually(stats.GaugeDeltaCallCount).Should(Equal(2))
			verifyGaugeDeltaCall(TotalHttpSubscriptions, 0, 1.0, 0)
			verifyGaugeDeltaCall(TotalTcpSubscriptions, 0, 1.0, 1)
		})

		It("periodically gets total routes", func() {
			tickChan <- time.Now()

			Eventually(stats.GaugeCallCount).Should(Equal(6))

			verifyGaugeCall(TotalHttpRoutes, 5, 1.0, 2)
			verifyGaugeCall(TotalTcpRoutes, 3, 1.0, 3)
		})

		Context("When a create event happens", func() {
			Context("when event is for http route", func() {
				BeforeEach(func() {
					storeNode := client.Node{Value: "valuable-string"}
					resultsChan <- db.Event{Type: db.UpdateEvent, Node: &storeNode}
				})

				It("increments the gauge", func() {
					Eventually(stats.GaugeDeltaCallCount).Should(Equal(1))
					verifyGaugeDeltaCall(TotalHttpRoutes, 1, 1.0, 0)
				})
			})

			Context("when event is for tcp route", func() {
				BeforeEach(func() {
					storeNode := client.Node{Value: "invaluable-string"}
					tcpResultsChan <- db.Event{Type: db.UpdateEvent, Node: &storeNode}
				})

				It("increments the gauge", func() {
					Eventually(stats.GaugeDeltaCallCount).Should(Equal(1))
					verifyGaugeDeltaCall(TotalTcpRoutes, 1, 1.0, 0)
				})
			})
		})

		Context("When a update event happens", func() {
			Context("when event is for http route", func() {
				BeforeEach(func() {
					storeNode := client.Node{Value: "valuable-string"}
					prevNode := client.Node{Value: "older-valuable-string"}
					resultsChan <- db.Event{Type: db.UpdateEvent, Node: &storeNode, PrevNode: &prevNode}
				})

				It("doesn't modify the gauge", func() {
					Eventually(stats.GaugeDeltaCallCount).Should(Equal(1))
					verifyGaugeDeltaCall(TotalHttpRoutes, 0, 1.0, 0)
				})
			})

			Context("when event is for tcp route", func() {
				BeforeEach(func() {
					storeNode := client.Node{Value: "invaluable-string"}
					prevNode := client.Node{Value: "older-invaluable-string"}
					tcpResultsChan <- db.Event{Type: db.UpdateEvent, Node: &storeNode, PrevNode: &prevNode}
				})

				It("doesn't modify the gauge", func() {
					Eventually(stats.GaugeDeltaCallCount).Should(Equal(1))
					verifyGaugeDeltaCall(TotalTcpRoutes, 0, 1.0, 0)
				})
			})
		})

		Context("When a expire event happens", func() {
			BeforeEach(func() {
				storeNode := client.Node{Value: "valuable-string"}
				resultsChan <- db.Event{Type: db.ExpireEvent, Node: &storeNode}
			})

			It("decrements the gauge", func() {
				Eventually(stats.GaugeDeltaCallCount).Should(Equal(1))

				updatedStat, count, rate := stats.GaugeDeltaArgsForCall(0)
				Expect(updatedStat).To(Equal(TotalHttpRoutes))
				Expect(count).To(BeNumerically("==", -1))
				Expect(rate).To(BeNumerically("==", 1.0))
			})
		})

		Context("When a delete event happens", func() {
			Context("when event is for http route", func() {
				BeforeEach(func() {
					storeNode := client.Node{Value: "valuable-string"}
					resultsChan <- db.Event{Type: db.DeleteEvent, Node: &storeNode}
				})

				It("decrements the gauge", func() {
					Eventually(stats.GaugeDeltaCallCount).Should(Equal(1))
					verifyGaugeDeltaCall(TotalHttpRoutes, -1, 1.0, 0)
				})
			})

			Context("when event is for tcp route", func() {
				BeforeEach(func() {
					storeNode := client.Node{Value: "invaluable-string"}
					tcpResultsChan <- db.Event{Type: db.DeleteEvent, Node: &storeNode}
				})

				It("decrements the gauge", func() {
					Eventually(stats.GaugeDeltaCallCount).Should(Equal(1))
					verifyGaugeDeltaCall(TotalTcpRoutes, -1, 1.0, 0)
				})
			})
		})

		Context("When the token error counter is incremented", func() {
			var (
				currentTokenErrors int64
			)

			BeforeEach(func() {
				currentTokenErrors = GetTokenErrors()
				IncrementTokenError()
			})

			It("emits the incremented token error metric", func() {
				tickChan <- time.Now()
				Eventually(stats.GaugeCallCount).Should(Equal(6))
				verifyGaugeCall("total_token_errors", currentTokenErrors+1, 1.0, 4)
			})
		})

		Context("When the key verification refreshed counter is incremented", func() {
			var (
				currentKeyRefreshEventCount int64
			)

			BeforeEach(func() {
				currentKeyRefreshEventCount = GetKeyVerificationRefreshCount()
				IncrementKeyVerificationRefreshCount()
			})

			It("emits token error metrics", func() {
				tickChan <- time.Now()
				Eventually(stats.GaugeCallCount).Should(Equal(6))
				verifyGaugeCall("key_refresh_events", currentKeyRefreshEventCount+1, 1.0, 5)
			})
		})

	})
})
