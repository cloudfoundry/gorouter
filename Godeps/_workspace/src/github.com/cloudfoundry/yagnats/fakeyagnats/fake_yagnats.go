package fakeyagnats

import (
	"sync"

	"github.com/cloudfoundry/yagnats"
)

type FakeYagnats struct {
	subscriptions        map[string][]yagnats.Subscription
	publishedMessages    map[string][]yagnats.Message
	unsubscriptions      []int
	unsubscribedSubjects []string

	connectedConnectionProvider yagnats.ConnectionProvider

	connectError     error
	unsubscribeError error

	whenSubscribing map[string]func() error
	whenPublishing  map[string]func() error

	onPing       func() bool
	pingResponse bool

	nextSubscriptionID int

	sync.RWMutex
}

func New() *FakeYagnats {
	fake := &FakeYagnats{}
	fake.Reset()
	return fake
}

func (f *FakeYagnats) Reset() {
	f.Lock()
	defer f.Unlock()

	f.publishedMessages = map[string][]yagnats.Message{}
	f.subscriptions = map[string][]yagnats.Subscription{}
	f.unsubscriptions = []int{}
	f.unsubscribedSubjects = []string{}

	f.connectedConnectionProvider = nil

	f.connectError = nil
	f.unsubscribeError = nil

	f.whenSubscribing = map[string]func() error{}
	f.whenPublishing = map[string]func() error{}

	f.pingResponse = true

	f.nextSubscriptionID = 0
}

func (f *FakeYagnats) Ping() bool {
	f.RLock()
	onPing := f.onPing
	response := f.pingResponse
	f.RUnlock()

	if onPing != nil {
		return onPing()
	}

	return response
}

func (f *FakeYagnats) Connect(connectionProvider yagnats.ConnectionProvider) error {
	f.Lock()
	defer f.Unlock()

	if f.connectError != nil {
		return f.connectError
	}

	f.connectedConnectionProvider = connectionProvider

	return f.connectError
}

func (f *FakeYagnats) Disconnect() {
	f.Lock()
	defer f.Unlock()

	f.connectedConnectionProvider = nil
	return
}

func (f *FakeYagnats) Publish(subject string, payload []byte) error {
	return f.PublishWithReplyTo(subject, "", payload)
}

func (f *FakeYagnats) PublishWithReplyTo(subject, reply string, payload []byte) error {
	f.RLock()

	injectedCallback, injected := f.whenPublishing[subject]

	message := &yagnats.Message{
		Subject: subject,
		ReplyTo: reply,
		Payload: payload,
	}

	var callback yagnats.Callback

	if len(f.subscriptions[subject]) > 0 {
		callback = f.subscriptions[subject][0].Callback
	}

	f.RUnlock()

	if injected {
		err := injectedCallback()
		if err != nil {
			return err
		}
	}

	f.Lock()
	f.publishedMessages[subject] = append(f.publishedMessages[subject], *message)
	f.Unlock()

	if callback != nil {
		callback(message)
	}

	return nil
}

func (f *FakeYagnats) Subscribe(subject string, callback yagnats.Callback) (int, error) {
	return f.SubscribeWithQueue(subject, "", callback)
}

func (f *FakeYagnats) SubscribeWithQueue(subject, queue string, callback yagnats.Callback) (int, error) {
	f.RLock()

	injectedCallback, injected := f.whenSubscribing[subject]

	f.RUnlock()

	if injected {
		err := injectedCallback()
		if err != nil {
			return 0, err
		}
	}

	f.Lock()
	defer f.Unlock()

	f.nextSubscriptionID++

	subscription := yagnats.Subscription{
		Subject:  subject,
		Queue:    queue,
		ID:       f.nextSubscriptionID,
		Callback: callback,
	}

	f.subscriptions[subject] = append(f.subscriptions[subject], subscription)

	return subscription.ID, nil
}

func (f *FakeYagnats) Unsubscribe(subscription int) error {
	f.Lock()
	defer f.Unlock()

	if f.unsubscribeError != nil {
		return f.unsubscribeError
	}

	f.unsubscriptions = append(f.unsubscriptions, subscription)

	return nil
}

func (f *FakeYagnats) UnsubscribeAll(subject string) {
	f.Lock()
	defer f.Unlock()

	f.unsubscribedSubjects = append(f.unsubscribedSubjects, subject)
}

func (f *FakeYagnats) WhenSubscribing(subject string, callback func() error) {
	f.Lock()
	defer f.Unlock()

	f.whenSubscribing[subject] = callback
}

func (f *FakeYagnats) Subscriptions(subject string) []yagnats.Subscription {
	f.RLock()
	defer f.RUnlock()

	return f.subscriptions[subject]
}

func (f *FakeYagnats) WhenPublishing(subject string, callback func() error) {
	f.Lock()
	defer f.Unlock()

	f.whenPublishing[subject] = callback
}

func (f *FakeYagnats) PublishedMessages(subject string) []yagnats.Message {
	f.RLock()
	defer f.RUnlock()

	return f.publishedMessages[subject]
}
