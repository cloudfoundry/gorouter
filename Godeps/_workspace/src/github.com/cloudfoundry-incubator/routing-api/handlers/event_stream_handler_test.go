package handlers_test

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"

	fake_token "github.com/cloudfoundry-incubator/routing-api/authentication/fakes"
	fake_db "github.com/cloudfoundry-incubator/routing-api/db/fakes"
	"github.com/cloudfoundry-incubator/routing-api/handlers"
	fake_statsd "github.com/cloudfoundry-incubator/routing-api/metrics/fakes"
	"github.com/cloudfoundry/storeadapter"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/pivotal-golang/lager/lagertest"
	"github.com/vito/go-sse/sse"
)

var _ = Describe("EventsHandler", func() {
	var (
		handler  handlers.EventStreamHandler
		database *fake_db.FakeDB
		logger   *lagertest.TestLogger
		token    *fake_token.FakeToken
		server   *httptest.Server
		stats    *fake_statsd.FakePartialStatsdClient
		stopChan chan struct{}
	)

	BeforeEach(func() {
		token = &fake_token.FakeToken{}
		database = &fake_db.FakeDB{}
		logger = lagertest.NewTestLogger("event-handler-test")
		stats = new(fake_statsd.FakePartialStatsdClient)
		stopChan = make(chan struct{})
		handler = *handlers.NewEventStreamHandler(token, database, logger, stats, stopChan)
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

	Describe(".EventStream", func() {
		var (
			response        *http.Response
			eventStreamDone chan struct{}
		)

		BeforeEach(func() {
			eventStreamDone = make(chan struct{})
			server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				handler.EventStream(w, r)
				close(eventStreamDone)
			}))
		})

		JustBeforeEach(func() {
			var err error

			response, err = http.Get(server.URL)
			Expect(err).NotTo(HaveOccurred())
		})

		It("checks for routing.routes.read scope", func() {
			_, permission := token.DecodeTokenArgsForCall(0)
			Expect(permission).To(ConsistOf(handlers.RoutingRoutesReadScope))
		})

		Context("when the user has incorrect scopes", func() {
			BeforeEach(func() {
				token.DecodeTokenReturns(errors.New("Not valid"))
			})

			It("returns an Unauthorized status code", func() {
				Expect(response.StatusCode).To(Equal(http.StatusUnauthorized))
			})
		})

		Context("when the user has routing.routes.read scope", func() {
			BeforeEach(func() {
				resultsChan := make(chan storeadapter.WatchEvent, 1)
				storeNode := storeadapter.StoreNode{Value: []byte("valuable-string")}
				resultsChan <- storeadapter.WatchEvent{Type: storeadapter.UpdateEvent, Node: &storeNode}
				database.WatchRouteChangesReturns(resultsChan, nil, nil)
			})

			It("emits events from changes in the db", func() {
				reader := sse.NewReadCloser(response.Body)

				event, err := reader.Next()
				Expect(err).NotTo(HaveOccurred())

				expectedEvent := sse.Event{ID: "0", Name: "Upsert", Data: []byte("valuable-string")}

				Expect(event).To(Equal(expectedEvent))
			})

			It("sets the content-type to text/event-stream", func() {
				Expect(response.Header.Get("Content-Type")).Should(Equal("text/event-stream; charset=utf-8"))
				Expect(response.Header.Get("Cache-Control")).Should(Equal("no-cache, no-store, must-revalidate"))
				Expect(response.Header.Get("Connection")).Should(Equal("keep-alive"))
			})

			Context("when the event is Invalid", func() {
				BeforeEach(func() {
					resultsChan := make(chan storeadapter.WatchEvent, 1)
					resultsChan <- storeadapter.WatchEvent{Type: storeadapter.InvalidEvent}
					database.WatchRouteChangesReturns(resultsChan, nil, nil)
				})

				It("closes the event stream", func() {
					reader := sse.NewReadCloser(response.Body)
					_, err := reader.Next()
					Expect(err).Should(Equal(io.EOF))
				})
			})

			Context("when the event is of type Expire", func() {
				BeforeEach(func() {
					resultsChan := make(chan storeadapter.WatchEvent, 1)
					storeNode := storeadapter.StoreNode{Value: []byte("valuable-string")}
					resultsChan <- storeadapter.WatchEvent{Type: storeadapter.ExpireEvent, PrevNode: &storeNode}
					database.WatchRouteChangesReturns(resultsChan, nil, nil)
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
					resultsChan := make(chan storeadapter.WatchEvent, 1)
					storeNode := storeadapter.StoreNode{Value: []byte("valuable-string")}
					resultsChan <- storeadapter.WatchEvent{Type: storeadapter.DeleteEvent, PrevNode: &storeNode}
					database.WatchRouteChangesReturns(resultsChan, nil, nil)
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
					resultsChan := make(chan storeadapter.WatchEvent, 1)
					storeNode := storeadapter.StoreNode{Value: []byte("valuable-string")}
					resultsChan <- storeadapter.WatchEvent{Type: storeadapter.CreateEvent, Node: &storeNode}
					database.WatchRouteChangesReturns(resultsChan, nil, nil)
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
					resultsChan := make(chan storeadapter.WatchEvent, 1)
					storeNode := storeadapter.StoreNode{Value: []byte("valuable-string")}
					resultsChan <- storeadapter.WatchEvent{Type: storeadapter.UpdateEvent, Node: &storeNode}
					database.WatchRouteChangesReturns(resultsChan, nil, nil)
				})

				It("emits a Upsert Event", func() {
					reader := sse.NewReadCloser(response.Body)
					event, err := reader.Next()
					expectedEvent := sse.Event{ID: "0", Name: "Upsert", Data: []byte("valuable-string")}

					Expect(err).NotTo(HaveOccurred())
					Expect(event).To(Equal(expectedEvent))
				})
			})

			Context("when the api server is stopped", func() {
				var cancelChan chan bool

				BeforeEach(func() {
					resultsChan := make(chan storeadapter.WatchEvent, 1)
					storeNode := storeadapter.StoreNode{Value: []byte("valuable-string")}
					resultsChan <- storeadapter.WatchEvent{Type: storeadapter.UpdateEvent, Node: &storeNode}

					cancelChan = make(chan bool)
					database.WatchRouteChangesReturns(resultsChan, cancelChan, nil)
				})

				It("returns early", func() {
					stopChan <- struct{}{}
					Eventually(cancelChan).Should(Receive())
					Eventually(eventStreamDone).Should(BeClosed())
				})
			})

			Context("when the watch returns an error", func() {
				var errChan chan error

				BeforeEach(func() {
					resultsChan := make(chan storeadapter.WatchEvent, 1)
					storeNode := storeadapter.StoreNode{Value: []byte("valuable-string")}
					resultsChan <- storeadapter.WatchEvent{Type: storeadapter.UpdateEvent, Node: &storeNode}

					errChan = make(chan error)
					database.WatchRouteChangesReturns(resultsChan, nil, errChan)
				})

				It("returns early", func() {
					errChan <- errors.New("Boom!")
					Eventually(eventStreamDone).Should(BeClosed())
				})
			})

			Context("when the client closes the response body", func() {
				It("returns early", func() {
					reader := sse.NewReadCloser(response.Body)

					err := reader.Close()
					Expect(err).NotTo(HaveOccurred())
					Eventually(eventStreamDone).Should(BeClosed())
				})
			})
		})
	})
})
