package generic_logger

import (
	"log"
)

type GenericLogger interface {
	Fatalf(string, ...interface{})
	Errorf(string, ...interface{})
	Debugf(string, ...interface{})
}

type defaultGenericLogger struct {
	debug bool
}

func NewDefaultGenericLogger(debug bool) GenericLogger {
	return defaultGenericLogger{
		debug: debug,
	}
}

func (defaultGenericLogger) Fatalf(format string, args ...interface{}) {
	log.Fatalf(format, args...)
}

func (defaultGenericLogger) Errorf(format string, args ...interface{}) {
	log.Printf("ERROR: "+format, args...)
}

func (l defaultGenericLogger) Debugf(format string, args ...interface{}) {
	if l.debug {
		log.Printf("DEBUG: "+format, args...)
	}
}
