// Package metrics provides a simple API for sending value and counter metrics
// through the dropsonde system.
//
// Use
//
// See the documentation for package dropsonde for configuration details.
//
// Importing package dropsonde and initializing will initial this package.
// To send metrics use
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
	"github.com/cloudfoundry/dropsonde/metric_sender"
)

var metricSender metric_sender.MetricSender

// Initialize prepares the metrics package for use with the automatic Emitter.
func Initialize(ms metric_sender.MetricSender) {
	metricSender = ms
}

// SendValue sends a value event for the named metric. See
// http://metrics20.org/spec/#units for the specifications on allowed units.
func SendValue(name string, value float64, unit string) error {
	if metricSender == nil {
		return nil
	}
	return metricSender.SendValue(name, value, unit)
}

// IncrementCounter sends an event to increment the named counter by one.
// Maintaining the value of the counter is the responsibility of the receiver of
// the event, not the process that includes this package.
func IncrementCounter(name string) error {
	if metricSender == nil {
		return nil
	}
	return metricSender.IncrementCounter(name)
}

// AddToCounter sends an event to increment the named counter by the specified
// (positive) delta. Maintaining the value of the counter is the responsibility
// of the receiver, as with IncrementCounter.
func AddToCounter(name string, delta uint64) error {
	if metricSender == nil {
		return nil
	}
	return metricSender.AddToCounter(name, delta)
}
