package logger

import (
	"io"
	"log/slog"
	"os"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/exp/zapslog"
	"go.uber.org/zap/zapcore"
)

var (
	dynamicLoggingConfig dynamicTimeEncoder
	baseLogger           *slog.Logger
	writeSyncer          = &dynamicWriter{w: zapcore.Lock(os.Stdout)}
)

type dynamicTimeEncoder struct {
	encoding string
	level    zap.AtomicLevel
}

type dynamicWriter struct {
	w WriteSyncer
}

func SetDynamicWriteSyncer(syncer WriteSyncer) {
	writeSyncer.w = syncer
}

func (d *dynamicWriter) Write(b []byte) (n int, err error) {
	return d.w.Write(b)
}

func (d *dynamicWriter) Sync() error {
	return d.w.Sync()
}

type WriteSyncer interface {
	io.Writer
	Sync() error
}

func init() {
	baseLogger = initializeLogger()
}

/*
SetTimeEncoder dynamically sets the time encoder at runtime:
'rfc3339': The encoder is set to a custom RFC3339 encoder
All other values: The encoder is set to an Epoch encoder
*/
func SetTimeEncoder(enc string) {
	dynamicLoggingConfig.encoding = enc
}

func (e *dynamicTimeEncoder) encodeTime(t time.Time, pae zapcore.PrimitiveArrayEncoder) {
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
	dynamicLoggingConfig.level.SetLevel(zapLevel)
}

type Logger interface {
}

/*
InitializeLogger is used to create a pre-configured slog.Logger with a zapslog handler and provided logging level,
timestamp format and writeSyncer.
*/
func initializeLogger() *slog.Logger {
	zapLevel := zap.InfoLevel

	dynamicLoggingConfig = dynamicTimeEncoder{encoding: "epoch", level: zap.NewAtomicLevelAt(zapLevel)}

	zapConfig := zapcore.EncoderConfig{
		MessageKey:    "message",
		LevelKey:      "log_level",
		EncodeLevel:   numberLevelFormatter,
		TimeKey:       "timestamp",
		EncodeTime:    dynamicLoggingConfig.encodeTime,
		EncodeCaller:  zapcore.ShortCallerEncoder,
		StacktraceKey: "stack_trace",
	}

	zapCore := zapcore.NewCore(
		zapcore.NewJSONEncoder(zapConfig),
		writeSyncer,
		dynamicLoggingConfig.level,
	)

	zapHandler := zapslog.NewHandler(zapCore, &zapslog.HandlerOptions{AddSource: true})
	slogFrontend := slog.New(zapHandler)
	return slogFrontend
}

/*
ErrAttr is creating an slog.String attribute with 'error' key and the provided error message as value.
*/
func ErrAttr(err error) slog.Attr {
	return slog.String("error", err.Error())
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
CreateLoggerWithSource returns a copy of the provided logger, which comes with the 'source' attribute set to the provided
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
CreateLoggerWithSource returns a copy of the provided logger. All subsequent log statements will be nested in the 'data' field.
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
