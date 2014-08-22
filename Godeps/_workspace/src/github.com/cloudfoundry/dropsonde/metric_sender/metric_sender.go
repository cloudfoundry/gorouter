package metric_sender

import (
	"github.com/cloudfoundry/dropsonde/emitter"
	"github.com/cloudfoundry/dropsonde/events"
)

// A MetricSender emits metric events.
type MetricSender interface {
	SendValue(name string, value float64, unit string) error
	IncrementCounter(name string) error
	AddToCounter(name string, delta uint64) error
}

type metricSender struct {
	eventEmitter emitter.EventEmitter
}

// NewMetricSender instantiates a metricSender with the given EventEmitter.
func NewMetricSender(eventEmitter emitter.EventEmitter) MetricSender {
	return &metricSender{eventEmitter: eventEmitter}
}

// SendValue sends a metric with the given name, value and unit. See
// http://metrics20.org/spec/#units for a specification of acceptable units.
// Returns an error if one occurs while sending the event.
func (ms *metricSender) SendValue(name string, value float64, unit string) error {
	return ms.eventEmitter.Emit(&events.ValueMetric{Name: &name, Value: &value, Unit: &unit})
}

// IncrementCounter sends an event to increment the named counter by one.
// Maintaining the value of the counter is the responsibility of the receiver of
// the event, not the process that includes this package.
func (ms *metricSender) IncrementCounter(name string) error {
	return ms.AddToCounter(name, 1)
}

// AddToCounter sends an event to increment the named counter by the specified
// (positive) delta. Maintaining the value of the counter is the responsibility
// of the receiver, as with IncrementCounter.
func (ms *metricSender) AddToCounter(name string, delta uint64) error {
	return ms.eventEmitter.Emit(&events.CounterEvent{Name: &name, Delta: &delta})
}
