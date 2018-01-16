package test_util

import (
	"fmt"
	"net"
	"sync"

	"github.com/cloudfoundry/sonde-go/events"
	"github.com/gogo/protobuf/proto"
)

type Event struct {
	EventType string
	Name      string
	Origin    string
	Value     float64
}

type FakeMetron interface {
	AllEvents() []Event

	Address() string
	Close() error
	Port() int
}

type fakeMetron struct {
	lock           *sync.Mutex
	receivedEvents []Event
	listener       net.PacketConn
	port           int
}

func NewFakeMetron() *fakeMetron {
	port := NextAvailPort()
	addr := fmt.Sprintf("127.0.0.1:%d", port)
	listener, err := net.ListenPacket("udp4", addr)
	if err != nil {
		panic(err)
	}

	metron := &fakeMetron{
		lock:     &sync.Mutex{},
		listener: listener,
		port:     int(port),
	}
	go metron.listenForEvents()
	return metron
}

func (f *fakeMetron) Address() string {
	return f.listener.LocalAddr().String()
}

func (f *fakeMetron) Port() int {
	return f.port
}

func (f *fakeMetron) Close() error {
	return f.listener.Close()
}

func (f *fakeMetron) AllEvents() []Event {
	f.lock.Lock()
	defer f.lock.Unlock()

	ret := make([]Event, len(f.receivedEvents))
	copy(ret, f.receivedEvents)
	return ret
}

// modified from https://github.com/cloudfoundry/dropsonde/blob/9b2cd8f8f9e99dca1f764ca4511d6011b4f44d0c/integration_test/dropsonde_end_to_end_test.go
func (f *fakeMetron) listenForEvents() {
	for {
		buffer := make([]byte, 1024)
		n, _, err := f.listener.ReadFrom(buffer)
		if err != nil {
			return
		}

		if n == 0 {
			panic("Received empty packet")
		}
		envelope := new(events.Envelope)
		err = proto.Unmarshal(buffer[0:n], envelope)
		if err != nil {
			panic(err)
		}

		var eventId = envelope.GetEventType().String()

		newEvent := Event{EventType: eventId}

		switch envelope.GetEventType() {
		case events.Envelope_HttpStartStop:
			newEvent.Name = envelope.GetHttpStartStop().GetPeerType().String()
		case events.Envelope_ValueMetric:
			valMetric := envelope.GetValueMetric()
			newEvent.Name = valMetric.GetName()
			newEvent.Value = valMetric.GetValue()
		case events.Envelope_CounterEvent:
			countMetric := envelope.GetCounterEvent()
			newEvent.Name = countMetric.GetName()
			newEvent.Value = float64(countMetric.GetDelta())
		default:
			panic("Unexpected message type: " + envelope.GetEventType().String())

		}

		newEvent.Origin = envelope.GetOrigin()

		f.lock.Lock()
		f.receivedEvents = append(f.receivedEvents, newEvent)
		f.lock.Unlock()
	}
}
