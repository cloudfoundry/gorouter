package fake

import (
	"github.com/cloudfoundry/dropsonde/events"
	"sync"
)

type envelope struct {
	Event  events.Event
	Origin string
}

type FakeEventEmitter struct {
	ReturnError error
	Messages    []envelope
	Origin      string
	isClosed    bool
	sync.RWMutex
}

func NewFakeEventEmitter(origin string) *FakeEventEmitter {
	return &FakeEventEmitter{Origin: origin}
}
func (f *FakeEventEmitter) Emit(e events.Event) error {

	f.Lock()
	defer f.Unlock()

	if f.ReturnError != nil {
		err := f.ReturnError
		f.ReturnError = nil
		return err
	}

	f.Messages = append(f.Messages, envelope{e, f.Origin})
	return nil
}

func (f *FakeEventEmitter) GetMessages() (messages []envelope) {
	f.Lock()
	defer f.Unlock()

	messages = make([]envelope, len(f.Messages))
	copy(messages, f.Messages)
	return
}

func (f *FakeEventEmitter) GetEvents() []events.Event {
	messages := f.GetMessages()
	events := []events.Event{}
	for _, msg := range messages {
		events = append(events, msg.Event)
	}
	return events
}

func (f *FakeEventEmitter) Close() {
	f.Lock()
	defer f.Unlock()
	f.isClosed = true
}

func (f *FakeEventEmitter) IsClosed() bool {
	f.RLock()
	defer f.RUnlock()
	return f.isClosed
}
