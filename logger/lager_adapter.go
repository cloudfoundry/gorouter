package logger

import (
	"log/slog"
	"net/http"
	"strings"

	"code.cloudfoundry.org/lager/v3"

	"github.com/openzipkin/zipkin-go/idgenerator"
	"github.com/openzipkin/zipkin-go/model"
)

const (
	RequestIdHeader = "X-Vcap-Request-Id"
)

// LagerAdapter tbd
type LagerAdapter struct {
	logger *slog.Logger
	source string
}

// NewLagerAdapter returns a new lager.Logger that uses slog with a zap handler underneath.
func NewLagerAdapter(source string) *LagerAdapter {
	lagerAdapter := &LagerAdapter{
		source: source,
		logger: CreateLoggerWithSource(source, ""),
	}
	return lagerAdapter
}

// RegisterSink is never used after initialization, so it does nothing.
func (l *LagerAdapter) RegisterSink(_ lager.Sink) {
	panic("RegisterSink is not implemented")
}

// Session returns a new logger with a nested source and optional data.
func (l *LagerAdapter) Session(task string, data ...lager.Data) lager.Logger {
	logger := CreateLoggerWithSource(l.source, task)

	if data != nil {
		logger = logger.With(dataToFields(data)...)
	}

	return &LagerAdapter{
		logger: logger,
		source: l.source + "." + task,
	}
}

// SessionName returns the name of the logger source
func (l *LagerAdapter) SessionName() string {
	return l.source
}

// Debug logs a message at the debug log setLoggingLevel.
func (l *LagerAdapter) Debug(action string, data ...lager.Data) {
	l.logger.Debug(action, dataToFields(data)...)
}

// Info logs a message at the info log setLoggingLevel.
func (l *LagerAdapter) Info(action string, data ...lager.Data) {
	l.logger.Info(action, dataToFields(data)...)
}

// Error logs a message at the error log setLoggingLevel.
func (l *LagerAdapter) Error(action string, err error, data ...lager.Data) {
	l.logger.Error(action, append(dataToFields(data), ErrAttr(err))...)
}

// Fatal logs a message and exits with status 1.
func (l *LagerAdapter) Fatal(action string, err error, data ...lager.Data) {
	Fatal(l.logger, action, append(dataToFields(data), ErrAttr(err))...)
}

// WithData returns a logger with newly added data.
func (l *LagerAdapter) WithData(data lager.Data) lager.Logger {
	return &LagerAdapter{
		logger: l.logger.With(dataToFields([]lager.Data{data})...),
	}
}

func (l *LagerAdapter) WithTraceInfo(req *http.Request) lager.Logger {
	traceIDHeader := req.Header.Get(RequestIdHeader)
	if traceIDHeader == "" {
		return l.WithData(nil)
	}
	traceHex := strings.Replace(traceIDHeader, "-", "", -1)
	traceID, err := model.TraceIDFromHex(traceHex)
	if err != nil {
		return l.WithData(nil)
	}

	spanID := idgenerator.NewRandom128().SpanID(traceID)
	return l.WithData(lager.Data{"trace-id": traceID.String(), "span-id": spanID.String()})
}

func dataToFields(data []lager.Data) []any {
	var fields []any
	for _, datum := range data {
		for key, value := range datum {
			fields = append(fields, slog.Any(key, value))
		}
	}
	return fields
}
