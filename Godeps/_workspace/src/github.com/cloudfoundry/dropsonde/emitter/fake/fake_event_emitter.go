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
	mutex       *sync.RWMutex
	Origin      string
	isClosed    bool
}

func NewFakeEventEmitter(origin string) *FakeEventEmitter {
	return &FakeEventEmitter{mutex: new(sync.RWMutex), Origin: origin}
}
func (f *FakeEventEmitter) Emit(e events.Event) error {

	f.mutex.Lock()
	defer f.mutex.Unlock()

	if f.ReturnError != nil {
		err := f.ReturnError
		f.ReturnError = nil
		return err
	}

	f.Messages = append(f.Messages, envelope{e, f.Origin})
	return nil
}

func (f *FakeEventEmitter) GetMessages() (messages []envelope) {
	f.mutex.Lock()
	defer f.mutex.Unlock()

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
	f.mutex.Lock()
	defer f.mutex.Unlock()
	f.isClosed = true
}

func (f *FakeEventEmitter) IsClosed() bool {
	f.mutex.RLock()
	defer f.mutex.RUnlock()
	return f.isClosed
}
