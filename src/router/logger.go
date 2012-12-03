package router

import (
	steno "github.com/cloudfoundry/gosteno"
	"os"
)

var log steno.Logger

func init() {
	stenoConfig := &steno.Config{
		Sinks: []steno.Sink{steno.NewIOSink(os.Stderr)},
		Codec: steno.NewJsonCodec(),
		Level: steno.LOG_ALL,
	}

	steno.Init(stenoConfig)
	log = steno.NewLogger("init")
}

func SetupLogger() {
	level, err := steno.GetLogLevel(config.Log.Level)
	if err != nil {
		panic(err)
	}

	sinks := make([]steno.Sink, 0)
	if config.Log.File != "" {
		sinks = append(sinks, steno.NewFileSink(config.Log.File))
	} else {
		sinks = append(sinks, steno.NewIOSink(os.Stdout))
	}
	if config.Log.Syslog != "" {
		sinks = append(sinks, steno.NewSyslogSink(config.Log.Syslog))
	}

	stenoConfig := &steno.Config{
		Sinks: sinks,
		Codec: steno.NewJsonCodec(),
		Level: level,
	}

	steno.Init(stenoConfig)
	log = steno.NewLogger("router")
}
