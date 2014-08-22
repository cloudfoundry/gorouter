// Package metrics provides a simple API for sending value and counter metrics
// through the dropsonde system.
//
// Use
//
// See the documentation for package autowire for details on configuring through
// environment variables.
//
// Import the package (note that you do not need to additionally import
// autowire). The package self-initializes; to send metrics use
//
//		metrics.SendValue(name, value, unit)
//
// for sending known quantities, and
//
//		metrics.IncrementCounter(name)
//
// to increment a counter. (Note that the value of the counter is maintained by
// the receiver of the counter events, not the application that includes this
// package.)
package metrics

import (
	"github.com/cloudfoundry/dropsonde/autowire"
	"github.com/cloudfoundry/dropsonde/metric_sender"
)

var metricSender metric_sender.MetricSender

func init() {
	Initialize(metric_sender.NewMetricSender(autowire.AutowiredEmitter()))
}

// Initialize prepares the metrics package for use with the automatic Emitter
// from dropsonde/autowire. This function is called by the package's init
// method, so should only be explicitly called to reset the default
// MetricSender, e.g. in tests.
func Initialize(ms metric_sender.MetricSender) {
	metricSender = ms
}

// SendValue sends a value event for the named metric. See
// http://metrics20.org/spec/#units for the specifications on allowed units.
func SendValue(name string, value float64, unit string) error {
	return metricSender.SendValue(name, value, unit)
}

// IncrementCounter sends an event to increment the named counter by one.
// Maintaining the value of the counter is the responsibility of the receiver of
// the event, not the process that includes this package.
func IncrementCounter(name string) error {
	return metricSender.IncrementCounter(name)
}

// AddToCounter sends an event to increment the named counter by the specified
// (positive) delta. Maintaining the value of the counter is the responsibility
// of the receiver, as with IncrementCounter.
func AddToCounter(name string, delta uint64) error {
	return metricSender.AddToCounter(name, delta)
}
