package fake_routing_api

import (
	"errors"

	"github.com/cloudfoundry-incubator/routing-api"
)

type FakeEventSource struct {
	events chan routing_api.Event
	errors chan error
	Closed bool
}

func NewFakeEventSource() FakeEventSource {
	events := make(chan routing_api.Event, 10)
	errors := make(chan error, 10)
	return FakeEventSource{events: events, errors: errors, Closed: false}
}

func (fake *FakeEventSource) Next() (routing_api.Event, error) {
	if fake.Closed {
		return routing_api.Event{}, errors.New("closed stream")
	}

	select {
	case event := <-fake.events:
		return event, nil
	case err := <-fake.errors:
		return routing_api.Event{}, err
	}
}

func (fake *FakeEventSource) AddEvent(event routing_api.Event) {
	fake.events <- event
}

func (fake *FakeEventSource) AddError(err error) {
	fake.errors <- err
}

func (fake *FakeEventSource) Close() error {
	fake.Closed = true
	return nil
}
