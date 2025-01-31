package logger

import (
	"io"
	"log/slog"
	"os"
	"reflect"
	"sync"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/exp/zapslog"
	"go.uber.org/zap/zapcore"
)

var (
	conf        dynamicLoggingConfig
	baseLogger  *slog.Logger
	writeSyncer = &dynamicWriter{w: os.Stdout}
	mutex       sync.Mutex
)

/*
dynamicLoggingConfig holds dynamic configuration for the time encoding and logging level.
*/
type dynamicLoggingConfig struct {
	encoding string
	level    zap.AtomicLevel
}

type dynamicWriter struct {
	w WriteSyncer
}

// SetDynamicWriteSyncer sets the log handler's sink.
func SetDynamicWriteSyncer(syncer WriteSyncer) {
	mutex.Lock()
	defer mutex.Unlock()
	writeSyncer.w = syncer
}

func (d *dynamicWriter) Write(b []byte) (n int, err error) {
	mutex.Lock()
	defer mutex.Unlock()
	return d.w.Write(b)
}

func (d *dynamicWriter) Sync() error {
	mutex.Lock()
	defer mutex.Unlock()
	return d.w.Sync()
}

type WriteSyncer interface {
	io.Writer
	Sync() error
}

/*
init creates one global, configured logger instance. This instance has no 'source'
and nested 'data' field yet. It allows creating copies later on, where 'source'
and 'data' is set.
This is a workaround to a limitation in slog: Once the 'data' field has been added
via 'WithGroup()', we cannot go back and set the 'source' field in the log message
root.
*/
func init() {
	baseLogger = initializeLogger()
}

/*
SetTimeEncoder dynamically sets the time encoder at runtime:
'rfc3339': The encoder is set to a custom RFC3339 encoder
All other values: The encoder is set to an Epoch encoder
*/
func SetTimeEncoder(enc string) {
	conf.encoding = enc
}

func (e *dynamicLoggingConfig) encodeTime(t time.Time, pae zapcore.PrimitiveArrayEncoder) {
	switch e.encoding {
	case "rfc3339":
		RFC3339Formatter()(t, pae)
	default:
		zapcore.EpochTimeEncoder(t, pae)
	}
}

/*
SetLoggingLevel dynamically sets the logging level at runtime. See https://github.com/uber-go/zap/blob/5786471c1d41c255c1d8b63ad30a82b68eda2c21/zapcore/level.go#L180
for possible logging levels.
*/
func SetLoggingLevel(level string) {
	zapLevel, err := zapcore.ParseLevel(level)
	if err != nil {
		panic(err)
	}
	conf.level.SetLevel(zapLevel)
}

type Logger interface {
}

/*
InitializeLogger is used in init() to create a pre-configured slog.Logger with a zapslog handler and provided logging level,
timestamp format and writeSyncer.
*/
func initializeLogger() *slog.Logger {
	zapLevel := zap.InfoLevel

	conf = dynamicLoggingConfig{encoding: "epoch", level: zap.NewAtomicLevelAt(zapLevel)}

	zapConfig := zapcore.EncoderConfig{
		MessageKey:    "message",
		LevelKey:      "log_level",
		EncodeLevel:   numberLevelFormatter,
		TimeKey:       "timestamp",
		EncodeTime:    conf.encodeTime,
		EncodeCaller:  zapcore.ShortCallerEncoder,
		StacktraceKey: "stack_trace",
	}

	zapCore := zapcore.NewCore(
		zapcore.NewJSONEncoder(zapConfig),
		writeSyncer,
		conf.level,
	)

	// Disable adding the stack trace to all error messages automatically.
	// The stack trace in the panic handler is added manually and remains in effect.
	disableErrorStacktrace := zapslog.AddStacktraceAt(slog.LevelError + 1)

	zapHandler := zapslog.NewHandler(zapCore, zapslog.WithCaller(true), disableErrorStacktrace)
	slogFrontend := slog.New(zapHandler)
	return slogFrontend
}

/*
ErrAttr is creating an slog.String attribute with 'error' key and the provided error message as value.
*/
func ErrAttr(err error) slog.Attr {
	return slog.String("error", err.Error())
}

/*
StructValue takes an arbitrary struct. It returns a StructWithLogValue. which implements LogValue(), which return an slog.Value
where struct fields are parsed as a list of slog.Attr, and returned as an grouped slog.Value.
*/
func StructValue(obj any) StructWithLogValue {
	return StructWithLogValue{Value: obj}
}

/*
StructWithLogValue implements LogValue(), which allows lazy execution.
*/
type StructWithLogValue struct {
	Value any
}

func (r StructWithLogValue) LogValue() slog.Value {
	if r.Value == nil || (reflect.ValueOf(r.Value).Kind() == reflect.Ptr && reflect.ValueOf(r.Value).IsNil()) {
		return slog.GroupValue()
	}
	v := reflect.ValueOf(r.Value)
	if v.Kind() == reflect.Interface || v.Kind() == reflect.Pointer {
		v = v.Elem()
	} else if v.Kind() != reflect.Struct {
		return slog.GroupValue()
	}
	var values []slog.Attr
	for i := 0; i < v.NumField(); i++ {
		field := v.Field(i)
		fieldValue, ok := v.Type().Field(i).Tag.Lookup("json")
		if !ok {
			fieldValue = v.Type().Field(i).Name
		}
		if field.CanInterface() {
			values = append(values, slog.Any(
				fieldValue,
				slog.AnyValue(field.Interface())))
		}
	}
	return slog.GroupValue(values...)
}

func numberLevelFormatter(level zapcore.Level, enc zapcore.PrimitiveArrayEncoder) {
	enc.AppendInt(levelNumber(level))
}

// We add 1 to zap's default values to match our setLoggingLevel definitions
// https://github.com/uber-go/zap/blob/5786471c1d41c255c1d8b63ad30a82b68eda2c21/zapcore/level.go#L37
func levelNumber(level zapcore.Level) int {
	return int(level) + 1
}

/*
CreateLoggerWithSource returns a copy of the logger, which comes with the 'source' attribute set to the provided
prefix and component. All subsequent log statements will be nested in the 'data' field.
*/
func CreateLoggerWithSource(prefix string, component string) *slog.Logger {
	if baseLogger == nil {
		panic("logger is not initialized")
	}
	var appendix string

	if len(component) == 0 {
		appendix = prefix
	} else {
		appendix = prefix + "." + component
	}
	return baseLogger.With(slog.String("source", appendix)).WithGroup("data")
}

/*
CreateLogger returns a copy of the logger. All subsequent log statements will be nested in the 'data' field.
*/
func CreateLogger() *slog.Logger {
	if baseLogger == nil {
		panic("logger is not initialized")
	}
	return baseLogger.WithGroup("data")
}

/*
Panic logs message and slogAttrs with Error level. For compatibility with zlog, the function is panicking after
writing the log message.
*/
func Panic(logger *slog.Logger, message string, slogAttrs ...any) {
	logger.Error(message, slogAttrs...)
	panic(message)
}

/*
Fatal logs message and slogAttrs with Error level. For compatibility with zlog, the process is terminated
via os.Exit(1) after writing the log message.
*/
func Fatal(logger *slog.Logger, message string, slogAttrs ...any) {
	logger.Error(message, slogAttrs...)
	os.Exit(1)
}

// RFC3339Formatter TimeEncoder for RFC3339 with trailing Z for UTC and nanoseconds
func RFC3339Formatter() zapcore.TimeEncoder {
	return zapcore.TimeEncoderOfLayout("2006-01-02T15:04:05.000000000Z")
}
