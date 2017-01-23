package logger

import "github.com/uber-go/zap"

//go:generate counterfeiter -o fakes/fake_logger.go . Logger
type Logger interface {
	With(...zap.Field) Logger
	Check(zap.Level, string) *zap.CheckedMessage
	Log(zap.Level, string, ...zap.Field)
	Debug(string, ...zap.Field)
	Info(string, ...zap.Field)
	Warn(string, ...zap.Field)
	Error(string, ...zap.Field)
	DPanic(string, ...zap.Field)
	Panic(string, ...zap.Field)
	Fatal(string, ...zap.Field)
	Session(string) Logger
	SessionName() string
}

type logger struct {
	source     string
	origLogger zap.Logger
	zap.Logger
}

func NewLogger(component string, options ...zap.Option) Logger {
	enc := zap.NewJSONEncoder(
		zap.LevelString("log_level"),
		zap.MessageKey("message"),
		zap.EpochFormatter("timestamp"),
		numberLevelFormatter(),
	)
	origLogger := zap.New(enc, options...)

	return &logger{
		source:     component,
		origLogger: origLogger,
		Logger:     origLogger.With(zap.String("source", component)),
	}
}

func (log *logger) Session(component string) Logger {
	newSource := log.source + "." + component
	lggr := &logger{
		source:     newSource,
		origLogger: log.origLogger,
		Logger:     log.origLogger.With(zap.String("source", newSource)),
	}
	return lggr
}

func (log *logger) SessionName() string {
	return log.source
}

func (log *logger) With(fields ...zap.Field) Logger {
	return &logger{
		source:     log.source,
		origLogger: log.origLogger.With(fields...),
		Logger:     log.Logger.With(fields...),
	}
}

func numberLevelFormatter() zap.LevelFormatter {
	return zap.LevelFormatter(func(level zap.Level) zap.Field {
		return zap.Int("log_level", levelNumber(level))
	})
}

// We add 1 to zap's default values to match our level definitions
// https://github.com/uber-go/zap/blob/47f41350ff078ea1415b63c117bf1475b7bbe72c/level.go#L36
func levelNumber(level zap.Level) int {
	return int(level) + 1
}
