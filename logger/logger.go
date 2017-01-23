package logger

import "github.com/uber-go/zap"

// Logger is the zap.Logger interface with additional Session methods.
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
	context    []zap.Field
	zap.Logger
}

// NewLogger returns a new zap logger that implements the Logger interface.
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

func (l *logger) Session(component string) Logger {
	newSource := l.source + "." + component
	lggr := &logger{
		source:     newSource,
		origLogger: l.origLogger,
		Logger:     l.origLogger.With(zap.String("source", newSource)),
		context:    l.context,
	}
	return lggr
}

func (l *logger) SessionName() string {
	return l.source
}

func (l *logger) wrapDataFields(fields ...zap.Field) zap.Field {
	finalFields := append(l.context, fields...)
	return zap.Nest("data", finalFields...)
}

func (l *logger) With(fields ...zap.Field) Logger {
	return &logger{
		source:     l.source,
		origLogger: l.origLogger,
		Logger:     l.Logger,
		context:    append(l.context, fields...),
	}
}

func (l *logger) Log(level zap.Level, msg string, fields ...zap.Field) {
	l.Logger.Log(level, msg, l.wrapDataFields(fields...))
}
func (l *logger) Debug(msg string, fields ...zap.Field) {
	l.Log(zap.DebugLevel, msg, fields...)
}
func (l *logger) Info(msg string, fields ...zap.Field) {
	l.Log(zap.InfoLevel, msg, fields...)
}
func (l *logger) Warn(msg string, fields ...zap.Field) {
	l.Log(zap.WarnLevel, msg, fields...)
}
func (l *logger) Error(msg string, fields ...zap.Field) {
	l.Log(zap.ErrorLevel, msg, fields...)
}
func (l *logger) DPanic(msg string, fields ...zap.Field) {
	l.Logger.DPanic(msg, l.wrapDataFields(fields...))
}
func (l *logger) Panic(msg string, fields ...zap.Field) {
	l.Logger.Panic(msg, l.wrapDataFields(fields...))
}
func (l *logger) Fatal(msg string, fields ...zap.Field) {
	l.Logger.Fatal(msg, l.wrapDataFields(fields...))
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
