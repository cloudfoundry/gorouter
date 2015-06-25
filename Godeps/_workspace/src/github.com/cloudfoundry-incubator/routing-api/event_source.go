package routing_api

import (
	"encoding/json"

	"github.com/cloudfoundry-incubator/routing-api/db"
	trace "github.com/cloudfoundry-incubator/trace-logger"
	"github.com/vito/go-sse/sse"
)

type EventSource interface {
	Next() (Event, error)
	Close() error
}

type RawEventSource interface {
	Next() (sse.Event, error)
	Close() error
}

type eventSource struct {
	rawEventSource RawEventSource
}

type Event struct {
	Route  db.Route
	Action string
}

func NewEventSource(raw RawEventSource) EventSource {
	return &eventSource{
		rawEventSource: raw,
	}
}

func (e *eventSource) Next() (Event, error) {
	rawEvent, err := e.rawEventSource.Next()
	if err != nil {
		return Event{}, err
	}

	trace.DumpJSON("EVENT", rawEvent)

	event, err := convertRawEvent(rawEvent)
	if err != nil {
		return Event{}, err
	}

	return event, nil
}

func (e *eventSource) Close() error {
	err := e.rawEventSource.Close()
	if err != nil {
		return err
	}

	return nil
}

func convertRawEvent(event sse.Event) (Event, error) {
	var route db.Route

	err := json.Unmarshal(event.Data, &route)
	if err != nil {
		return Event{}, err
	}

	return Event{Action: event.Name, Route: route}, nil
}
