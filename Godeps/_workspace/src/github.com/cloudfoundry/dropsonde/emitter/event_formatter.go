package emitter

import (
	"code.google.com/p/gogoprotobuf/proto"
	"errors"
	"github.com/cloudfoundry/dropsonde/events"
)

var ErrorMissingOrigin = errors.New("Event not emitted due to missing origin information")
var ErrorUnknownEventType = errors.New("Cannot create envelope for unknown event type")

func Wrap(e events.Event, origin string) (*events.Envelope, error) {
	if origin == "" {
		return nil, ErrorMissingOrigin
	}

	envelope := &events.Envelope{Origin: proto.String(origin)}

	switch e := e.(type) {
	case *events.Heartbeat:
		envelope.EventType = events.Envelope_Heartbeat.Enum()
		envelope.Heartbeat = e
	case *events.HttpStart:
		envelope.EventType = events.Envelope_HttpStart.Enum()
		envelope.HttpStart = e
	case *events.HttpStop:
		envelope.EventType = events.Envelope_HttpStop.Enum()
		envelope.HttpStop = e
	case *events.ValueMetric:
		envelope.EventType = events.Envelope_ValueMetric.Enum()
		envelope.ValueMetric = e
	case *events.CounterEvent:
		envelope.EventType = events.Envelope_CounterEvent.Enum()
		envelope.CounterEvent = e
	case *events.LogMessage:
		envelope.EventType = events.Envelope_LogMessage.Enum()
		envelope.LogMessage = e
	default:
		return nil, ErrorUnknownEventType
	}

	return envelope, nil
}
