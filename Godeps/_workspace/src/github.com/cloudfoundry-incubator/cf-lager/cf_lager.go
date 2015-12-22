package cf_lager

import (
	"flag"
	"fmt"
	"os"

	"github.com/pivotal-golang/lager"
)

const (
	DEBUG = "debug"
	INFO  = "info"
	ERROR = "error"
	FATAL = "fatal"
)

var enableSyslog bool
var syslogPrefix string
var minLogLevel string

func AddFlags(flagSet *flag.FlagSet) {
	flagSet.StringVar(
		&minLogLevel,
		"logLevel",
		string(INFO),
		"log level: debug, info, error or fatal",
	)
}

func New(component string) (lager.Logger, *lager.ReconfigurableSink) {
	var minLagerLogLevel lager.LogLevel
	switch minLogLevel {
	case DEBUG:
		minLagerLogLevel = lager.DEBUG
	case INFO:
		minLagerLogLevel = lager.INFO
	case ERROR:
		minLagerLogLevel = lager.ERROR
	case FATAL:
		minLagerLogLevel = lager.FATAL
	default:
		panic(fmt.Errorf("unknown log level: %s", minLogLevel))
	}

	logger := lager.NewLogger(component)

	sink := lager.NewReconfigurableSink(lager.NewWriterSink(os.Stdout, lager.DEBUG), minLagerLogLevel)
	logger.RegisterSink(sink)

	return logger, sink
}
