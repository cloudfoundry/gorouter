package logger

import (
	"fmt"

	"code.cloudfoundry.org/lager"
	"github.com/uber-go/zap"
)

type LagerAdapter struct {
	originalLogger Logger
}

func NewLagerAdapter(zapLogger Logger) *LagerAdapter {
	return &LagerAdapter{
		originalLogger: zapLogger,
	}
}

func (_ *LagerAdapter) RegisterSink(_ lager.Sink) {}

func (l *LagerAdapter) Session(task string, data ...lager.Data) lager.Logger {
	tmpLogger := l.originalLogger.Session(task)

	if data != nil {
		tmpLogger = l.originalLogger.With(dataToFields(data)...)
	}

	return &LagerAdapter{
		originalLogger: tmpLogger,
	}
}

func (l *LagerAdapter) SessionName() string {
	return l.originalLogger.SessionName()
}

func (l *LagerAdapter) Debug(action string, data ...lager.Data) {
	l.originalLogger.Debug(action, dataToFields(data)...)
}

func (l *LagerAdapter) Info(action string, data ...lager.Data) {
	l.originalLogger.Info(action, dataToFields(data)...)
}

func (l *LagerAdapter) Error(action string, err error, data ...lager.Data) {
	l.originalLogger.Error(action, appendError(err, dataToFields(data))...)
}

func (l *LagerAdapter) Fatal(action string, err error, data ...lager.Data) {
	l.originalLogger.Fatal(action, appendError(err, dataToFields(data))...)
}

func (l *LagerAdapter) WithData(data lager.Data) lager.Logger {
	return &LagerAdapter{
		originalLogger: l.originalLogger.With(dataToFields([]lager.Data{data})...),
	}
}

func dataToFields(data []lager.Data) []zap.Field {
	fields := []zap.Field{}
	for _, datum := range data {
		for key, value := range datum {
			fields = append(fields, zap.String(key, fmt.Sprintf("%v", value)))
		}
	}
	return fields
}

func appendError(err error, fields []zap.Field) []zap.Field {
	return append(fields, zap.Error(err))
}
