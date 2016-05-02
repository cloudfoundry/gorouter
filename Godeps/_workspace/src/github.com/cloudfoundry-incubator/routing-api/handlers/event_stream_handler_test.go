package handlers_test

import (
	"errors"

	fake_client "github.com/cloudfoundry-incubator/uaa-go-client/fakes"
	"github.com/coreos/etcd/client"

	"io"
	"net/http"
	"net/http/httptest"

	"github.com/cloudfoundry-incubator/routing-api/db"
	fake_db "github.com/cloudfoundry-incubator/routing-api/db/fakes"
	"github.com/cloudfoundry-incubator/routing-api/handlers"
	"github.com/cloudfoundry-incubator/routing-api/metrics"
	fake_statsd "github.com/cloudfoundry-incubator/routing-api/metrics/fakes"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/pivotal-golang/lager/lagertest"
	"github.com/vito/go-sse/sse"
)

var _ = Describe("EventsHandler", func() {
	var (
		handler    handlers.EventStreamHandler
		database   *fake_db.FakeDB
		logger     *lagertest.TestLogger
		fakeClient *fake_client.FakeClient
		server     *httptest.Server
		stats      *fake_statsd.FakePartialStatsdClient
	)

	var emptyCancelFunc = func() {}

	BeforeEach(func() {
		fakeClient = &fake_client.FakeClient{}

		database = &fake_db.FakeDB{}
		database.WatchRouteChangesReturns(nil, nil, emptyCancelFunc)

		logger = lagertest.NewTestLogger("event-handler-test")
		stats = new(fake_statsd.FakePartialStatsdClient)
		handler = *handlers.NewEventStreamHandler(fakeClient, database, logger, stats)
	})

	AfterEach(func(done Done) {
		if server != nil {
			go func() {
				server.CloseClientConnections()
				server.Close()
				close(done)
			}()
		} else {
			close(done)
		}
	})

	Describe("EventStream", func() {
		var (
			response        *http.Response
			eventStreamDone chan struct{}
		)

		JustBeforeEach(func() {
			var err error
			response, err = http.Get(server.URL)
			Expect(err).NotTo(HaveOccurred())
		})

		Describe("HttpEventStream", func() {
			BeforeEach(func() {
				eventStreamDone = make(chan struct{})
				server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					handler.EventStream(w, r)
					close(eventStreamDone)
				}))
			})

			It("checks for routing.routes.read scope", func() {
				_, permission := fakeClient.DecodeTokenArgsForCall(0)
				Expect(permission).To(ConsistOf(handlers.RoutingRoutesReadScope))
			})

			Context("when the user has incorrect scopes", func() {
				var (
					currentCount int64
				)
				BeforeEach(func() {
					currentCount = metrics.GetTokenErrors()
					fakeClient.DecodeTokenReturns(errors.New("Not valid"))
				})

				It("returns an Unauthorized status code", func() {
					Expect(response.StatusCode).To(Equal(http.StatusUnauthorized))
					Expect(metrics.GetTokenErrors()).To(Equal(currentCount + 1))
				})
			})

			Context("when the user has routing.routes.read scope", func() {
				BeforeEach(func() {
					resultsChan := make(chan db.Event, 1)
					storeNode := client.Node{Value: "valuable-string"}
					resultsChan <- db.Event{Type: db.UpdateEvent, Node: &storeNode}
					database.WatchRouteChangesReturns(resultsChan, nil, emptyCancelFunc)
				})

				It("emits events from changes in the db", func() {
					reader := sse.NewReadCloser(response.Body)

					event, err := reader.Next()
					Expect(err).NotTo(HaveOccurred())

					expectedEvent := sse.Event{ID: "0", Name: "Upsert", Data: []byte("valuable-string")}

					Expect(event).To(Equal(expectedEvent))
					filterString := database.WatchRouteChangesArgsForCall(0)
					Expect(filterString).To(Equal(db.HTTP_ROUTE_BASE_KEY))
				})

				It("sets the content-type to text/event-stream", func() {
					Expect(response.Header.Get("Content-Type")).Should(Equal("text/event-stream; charset=utf-8"))
					Expect(response.Header.Get("Cache-Control")).Should(Equal("no-cache, no-store, must-revalidate"))
					Expect(response.Header.Get("Connection")).Should(Equal("keep-alive"))
				})

				Context("when the event is Invalid", func() {
					BeforeEach(func() {
						resultsChan := make(chan db.Event, 1)
						resultsChan <- db.Event{Type: db.InvalidEvent}
						database.WatchRouteChangesReturns(resultsChan, nil, emptyCancelFunc)
					})

					It("closes the event stream", func() {
						reader := sse.NewReadCloser(response.Body)
						_, err := reader.Next()
						Expect(err).Should(Equal(io.EOF))
					})
				})

				Context("when the event is of type Expire", func() {
					BeforeEach(func() {
						resultsChan := make(chan db.Event, 1)
						storeNode := client.Node{Value: "valuable-string"}
						resultsChan <- db.Event{Type: db.ExpireEvent, PrevNode: &storeNode}
						database.WatchRouteChangesReturns(resultsChan, nil, emptyCancelFunc)
					})

					It("emits a Delete Event", func() {
						reader := sse.NewReadCloser(response.Body)
						event, err := reader.Next()
						expectedEvent := sse.Event{ID: "0", Name: "Delete", Data: []byte("valuable-string")}

						Expect(err).NotTo(HaveOccurred())
						Expect(event).To(Equal(expectedEvent))
					})
				})

				Context("when the event is of type Delete", func() {
					BeforeEach(func() {
						resultsChan := make(chan db.Event, 1)
						storeNode := client.Node{Value: "valuable-string"}
						resultsChan <- db.Event{Type: db.DeleteEvent, PrevNode: &storeNode}
						database.WatchRouteChangesReturns(resultsChan, nil, emptyCancelFunc)
					})

					It("emits a Delete Event", func() {
						reader := sse.NewReadCloser(response.Body)
						event, err := reader.Next()
						expectedEvent := sse.Event{ID: "0", Name: "Delete", Data: []byte("valuable-string")}

						Expect(err).NotTo(HaveOccurred())
						Expect(event).To(Equal(expectedEvent))
					})
				})

				Context("when the event is of type Create", func() {
					BeforeEach(func() {
						resultsChan := make(chan db.Event, 1)
						storeNode := client.Node{Value: "valuable-string"}
						resultsChan <- db.Event{Type: db.CreateEvent, Node: &storeNode}
						database.WatchRouteChangesReturns(resultsChan, nil, emptyCancelFunc)
					})

					It("emits a Upsert Event", func() {
						reader := sse.NewReadCloser(response.Body)
						event, err := reader.Next()
						expectedEvent := sse.Event{ID: "0", Name: "Upsert", Data: []byte("valuable-string")}

						Expect(err).NotTo(HaveOccurred())
						Expect(event).To(Equal(expectedEvent))
					})
				})

				Context("when the event is of type Update", func() {
					BeforeEach(func() {
						resultsChan := make(chan db.Event, 1)
						storeNode := client.Node{Value: "valuable-string"}
						resultsChan <- db.Event{Type: db.UpdateEvent, Node: &storeNode}
						database.WatchRouteChangesReturns(resultsChan, nil, emptyCancelFunc)
					})

					It("emits a Upsert Event", func() {
						reader := sse.NewReadCloser(response.Body)
						event, err := reader.Next()
						expectedEvent := sse.Event{ID: "0", Name: "Upsert", Data: []byte("valuable-string")}

						Expect(err).NotTo(HaveOccurred())
						Expect(event).To(Equal(expectedEvent))
					})
				})

				Context("when the watch returns an error", func() {
					var errChan chan error

					BeforeEach(func() {
						resultsChan := make(chan db.Event, 1)
						storeNode := client.Node{Value: "valuable-string"}
						resultsChan <- db.Event{Type: db.UpdateEvent, Node: &storeNode}

						errChan = make(chan error)
						database.WatchRouteChangesReturns(resultsChan, errChan, emptyCancelFunc)
					})

					It("returns early", func() {
						errChan <- errors.New("Boom!")
						Eventually(eventStreamDone).Should(BeClosed())
					})
				})

				Context("when the client closes the response body", func() {
					var cancelTest chan struct{}
					BeforeEach(func() {
						resultsChan := make(chan db.Event, 1)
						cancelTest = make(chan struct{}, 1)

						cancelFunc := func() { cancelTest <- struct{}{} }
						database.WatchRouteChangesReturns(resultsChan, nil, cancelFunc)
					})
					It("returns early", func() {
						reader := sse.NewReadCloser(response.Body)

						err := reader.Close()
						Expect(err).NotTo(HaveOccurred())
						Eventually(cancelTest).Should(Receive())
						Eventually(eventStreamDone).Should(BeClosed())
					})
				})
			})
		})

		Describe("TcpEventStream", func() {
			BeforeEach(func() {
				eventStreamDone = make(chan struct{})
				server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					handler.TcpEventStream(w, r)
					close(eventStreamDone)
				}))
			})

			// No need to all combinations of test for tcp as it reuses same code path. Just confirm
			// that it puts watch on db with appropriate filter
			Context("when there are changes in db", func() {
				BeforeEach(func() {
					resultsChan := make(chan db.Event, 1)
					storeNode := client.Node{Value: "valuable-string"}
					resultsChan <- db.Event{Type: db.UpdateEvent, Node: &storeNode}
					database.WatchRouteChangesReturns(resultsChan, nil, emptyCancelFunc)
				})

				It("emits events from changes in the db", func() {
					reader := sse.NewReadCloser(response.Body)

					event, err := reader.Next()
					Expect(err).NotTo(HaveOccurred())

					expectedEvent := sse.Event{ID: "0", Name: "Upsert", Data: []byte("valuable-string")}

					Expect(event).To(Equal(expectedEvent))
					filterString := database.WatchRouteChangesArgsForCall(0)
					Expect(filterString).To(Equal(db.TCP_MAPPING_BASE_KEY))
				})
			})
		})
	})

})
