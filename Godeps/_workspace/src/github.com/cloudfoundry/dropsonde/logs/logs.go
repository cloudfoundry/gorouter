// Package logs provides a simple API for sending app logs from STDOUT and STDERR
// through the dropsonde system.
//
// Use
//
// See the documentation for package dropsonde for configuration details.
//
// Importing package dropsonde and initializing will initial this package.
// To send logs use
//
//		logs.SendAppLog(appId, message, sourceType, sourceInstance)
//
// for sending errors,
//
//		logs.SendAppErrorLog(appId, message, sourceType, sourceInstance)
package logs

import (
	"github.com/cloudfoundry/dropsonde/log_sender"
	"io"
)

var logSender log_sender.LogSender

// Initialize prepares the logs package for use with the automatic Emitter
// from dropsonde.
func Initialize(ls log_sender.LogSender) {
	logSender = ls
}

// SendAppLog sends a log message with the given appid, log message, source type
// and source instance, with a message type of std out.
// Returns an error if one occurs while sending the event.
func SendAppLog(appId, message, sourceType, sourceInstance string) error {
	if logSender == nil {
		return nil
	}
	return logSender.SendAppLog(appId, message, sourceType, sourceInstance)
}

// SendAppErrorLog sends a log error message with the given appid, log message, source type
// and source instance, with a message type of std err.
// Returns an error if one occurs while sending the event.
func SendAppErrorLog(appId, message, sourceType, sourceInstance string) error {
	if logSender == nil {
		return nil
	}
	return logSender.SendAppErrorLog(appId, message, sourceType, sourceInstance)
}

// ScanLogStream sends a log message with the given meta-data for each line from reader.
// Restarts on read errors and continues until EOF (or stopChan is closed).
func ScanLogStream(appId, sourceType, sourceInstance string, reader io.Reader, stopChan chan struct{}) {
	if logSender == nil {
		return
	}
	logSender.ScanLogStream(appId, sourceType, sourceInstance, reader, stopChan)
}

// ScanErrorLogStream sends a log error message with the given meta-data for each line from reader.
// Restarts on read errors and continues until EOF (or stopChan is closed).
func ScanErrorLogStream(appId, sourceType, sourceInstance string, reader io.Reader, stopChan chan struct{}) {
	if logSender == nil {
		return
	}
	logSender.ScanErrorLogStream(appId, sourceType, sourceInstance, reader, stopChan)
}
