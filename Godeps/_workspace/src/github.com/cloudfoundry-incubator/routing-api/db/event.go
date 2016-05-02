package db

import (
	"fmt"

	"github.com/coreos/etcd/client"
)

type Event struct {
	Type     EventType
	Node     *client.Node
	PrevNode *client.Node
}

type EventType int

const (
	InvalidEvent = EventType(iota)
	CreateEvent
	DeleteEvent
	ExpireEvent
	UpdateEvent
)

func (e EventType) String() string {
	switch e {
	case CreateEvent:
		return "Upsert"
	case UpdateEvent:
		return "Upsert"
	case DeleteEvent, ExpireEvent:
		return "Delete"
	default:
		return "Invalid"
	}
}

func NewEvent(event *client.Response) (Event, error) {
	var eventType EventType

	node := event.Node
	switch event.Action {
	case "delete", "compareAndDelete":
		eventType = DeleteEvent
		node = nil
	case "create":
		eventType = CreateEvent
	case "set", "update", "compareAndSwap":
		eventType = UpdateEvent
	case "expire":
		eventType = ExpireEvent
		node = nil
	default:
		return Event{}, fmt.Errorf("unknown event: %s", event.Action)
	}

	return Event{
		Type:     eventType,
		Node:     node,
		PrevNode: event.PrevNode,
	}, nil
}
