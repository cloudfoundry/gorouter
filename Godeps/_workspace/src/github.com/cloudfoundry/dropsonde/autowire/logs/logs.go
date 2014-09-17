// Package logs provides a simple API for sending app logs from STDOUT and STDERR
// through the dropsonde system.
//
// Use
//
// See the documentation for package autowire for details on configuring through
// environment variables.
//
// Import the package (note that you do not need to additionally import
// autowire). The package self-initializes; to send logs use
//
//		logs.SendAppLog(appId, message, sourceType, sourceInstance)
//
// for sending errors,
//
//		logs.SendAppErrorLog(appId, message, sourceType, sourceInstance)
package logs

import (
	"github.com/cloudfoundry/dropsonde/autowire"
	"github.com/cloudfoundry/dropsonde/log_sender"
	"github.com/cloudfoundry/gosteno"
)

var logSender log_sender.LogSender

func init() {
	Initialize(log_sender.NewLogSender(autowire.AutowiredEmitter(), gosteno.NewLogger("autowire/logs")))
}

// Initialize prepares the logs package for use with the automatic Emitter
// from dropsonde/autowire. This function is called by the package's init
// method, so should only be explicitly called to reset the default
// LogSender, e.g. in tests.
func Initialize(ls log_sender.LogSender) {
	logSender = ls
}

// SendAppLog sends a log message with the given appid, log message, source type
// and source instance, with a message type of std out.
// Returns an error if one occurs while sending the event.
func SendAppLog(appId, message, sourceType, sourceInstance string) error {
	return logSender.SendAppLog(appId, message, sourceType, sourceInstance)
}

// SendAppErrorLog sends a log error message with the given appid, log message, source type
// and source instance, with a message type of std err.
// Returns an error if one occurs while sending the event.
func SendAppErrorLog(appId, message, sourceType, sourceInstance string) error {
	return logSender.SendAppErrorLog(appId, message, sourceType, sourceInstance)
}
