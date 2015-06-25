package routing_api_test

import (
	"errors"

	"bytes"
	"encoding/json"
	"io/ioutil"

	"github.com/cloudfoundry-incubator/routing-api"
	"github.com/cloudfoundry-incubator/routing-api/db"
	"github.com/cloudfoundry-incubator/routing-api/fake_routing_api"
	trace "github.com/cloudfoundry-incubator/trace-logger"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/vito/go-sse/sse"
)

var _ = Describe("EventSource", func() {
	var fakeRawEventSource fake_routing_api.FakeRawEventSource
	var eventSource routing_api.EventSource

	BeforeEach(func() {
		fakeRawEventSource = fake_routing_api.FakeRawEventSource{}
		eventSource = routing_api.NewEventSource(&fakeRawEventSource)
	})

	Describe("Next", func() {
		Context("When the event source returns an error", func() {
			It("returns the error", func() {
				fakeRawEventSource.NextReturns(sse.Event{}, errors.New("boom"))
				_, err := eventSource.Next()
				Expect(err.Error()).To(Equal("boom"))
			})
		})

		Context("When the event source successfully returns an event", func() {
			It("logs the event", func() {
				stdout := bytes.NewBuffer([]byte{})
				trace.SetStdout(stdout)
				trace.Logger = trace.NewLogger("true")
				rawEvent := sse.Event{
					ID:    "1",
					Name:  "Test",
					Data:  []byte(`{"route":"jim.com","port":8080,"ip":"1.1.1.1","ttl":60,"log_guid":"logs"}`),
					Retry: 1,
				}
				expectedJSON, _ := json.Marshal(rawEvent)

				fakeRawEventSource.NextReturns(rawEvent, nil)
				eventSource.Next()

				log, err := ioutil.ReadAll(stdout)
				Expect(err).NotTo(HaveOccurred())
				Expect(log).To(ContainSubstring("EVENT: "))
				Expect(log).To(ContainSubstring(string(expectedJSON)))
			})

			Context("When the event is unmarshalled successfully", func() {
				It("returns the raw event", func() {
					rawEvent := sse.Event{
						ID:    "1",
						Name:  "Test",
						Data:  []byte(`{"route":"jim.com","port":8080,"ip":"1.1.1.1","ttl":60,"log_guid":"logs"}`),
						Retry: 1,
					}

					expectedEvent := routing_api.Event{
						Route:  db.Route{Route: "jim.com", Port: 8080, IP: "1.1.1.1", TTL: 60, LogGuid: "logs"},
						Action: "Test",
					}

					fakeRawEventSource.NextReturns(rawEvent, nil)
					event, err := eventSource.Next()
					Expect(err).ToNot(HaveOccurred())
					Expect(event).To(Equal(expectedEvent))
				})
			})

			Context("When the event is unmarshalled successfully", func() {
				It("returns the error", func() {
					rawEvent := sse.Event{
						ID:    "1",
						Name:  "Invalid",
						Data:  []byte("This isn't valid json"),
						Retry: 1,
					}

					fakeRawEventSource.NextReturns(rawEvent, nil)
					_, err := eventSource.Next()
					Expect(err).To(HaveOccurred())
				})
			})
		})
	})

	Describe("Close", func() {
		Context("when closing the raw event source succeeds", func() {
			It("closes the event source", func() {
				eventSource.Close()
				Expect(fakeRawEventSource.CloseCallCount()).To(Equal(1))
			})
		})

		Context("when closing the raw event source fails", func() {
			It("returns the error", func() {
				expectedError := errors.New("close failed")
				fakeRawEventSource.CloseReturns(expectedError)
				err := eventSource.Close()
				Expect(fakeRawEventSource.CloseCallCount()).To(Equal(1))
				Expect(err).To(Equal(expectedError))
			})
		})
	})
})
