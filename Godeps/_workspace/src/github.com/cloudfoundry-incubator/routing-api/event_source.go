package routing_api

import (
	"encoding/json"

	"github.com/cloudfoundry-incubator/routing-api/db"
	trace "github.com/cloudfoundry-incubator/trace-logger"
	"github.com/vito/go-sse/sse"
)

//go:generate counterfeiter -o fake_routing_api/fake_event_source.go . EventSource
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

//go:generate counterfeiter -o fake_routing_api/fake_tcp_event_source.go . TcpEventSource
type TcpEventSource interface {
	Next() (TcpEvent, error)
	Close() error
}

type TcpEvent struct {
	TcpRouteMapping db.TcpRouteMapping
	Action          string
}

type tcpEventSource struct {
	rawEventSource RawEventSource
}

func NewTcpEventSource(raw RawEventSource) TcpEventSource {
	return &tcpEventSource{
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
	return doClose(e.rawEventSource)
}

func (e *tcpEventSource) Next() (TcpEvent, error) {
	rawEvent, err := e.rawEventSource.Next()
	if err != nil {
		return TcpEvent{}, err
	}

	trace.DumpJSON("EVENT", rawEvent)

	event, err := convertRawToTcpEvent(rawEvent)
	if err != nil {
		return TcpEvent{}, err
	}

	return event, nil
}

func (e *tcpEventSource) Close() error {
	return doClose(e.rawEventSource)
}

func doClose(rawEventSource RawEventSource) error {
	err := rawEventSource.Close()
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

func convertRawToTcpEvent(event sse.Event) (TcpEvent, error) {
	var route db.TcpRouteMapping

	err := json.Unmarshal(event.Data, &route)
	if err != nil {
		return TcpEvent{}, err
	}

	return TcpEvent{Action: event.Name, TcpRouteMapping: route}, nil
}
