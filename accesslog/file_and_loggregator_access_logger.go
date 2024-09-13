package accesslog

import (
	"fmt"
	"io"
	"log/syslog"

	"github.com/uber-go/zap"

	"code.cloudfoundry.org/gorouter/accesslog/schema"
	"code.cloudfoundry.org/gorouter/config"
	"code.cloudfoundry.org/gorouter/logger"

	"os"
)

//go:generate counterfeiter -o fakes/accesslogger.go . AccessLogger
type AccessLogger interface {
	Run()
	Stop()
	Log(record schema.AccessLogRecord)
}

type NullAccessLogger struct {
}

func (x *NullAccessLogger) Run()                       {}
func (x *NullAccessLogger) Stop()                      {}
func (x *NullAccessLogger) Log(schema.AccessLogRecord) {}

type FileAndLoggregatorAccessLogger struct {
	channel                chan schema.AccessLogRecord
	stopCh                 chan struct{}
	writers                []CustomWriter
	writerCount            int
	disableXFFLogging      bool
	disableSourceIPLogging bool
	redactQueryParams      string
	logger                 logger.Logger
	logsender              schema.LogSender
}

type CustomWriter struct {
	Name            string
	Writer          io.Writer
	PerformTruncate bool
}

func CreateRunningAccessLogger(logger logger.Logger, logsender schema.LogSender, config *config.Config) (AccessLogger, error) {
	if config.AccessLog.File == "" && !config.Logging.LoggregatorEnabled {
		return &NullAccessLogger{}, nil
	}

	accessLogger := &FileAndLoggregatorAccessLogger{
		channel:                make(chan schema.AccessLogRecord, 1024),
		stopCh:                 make(chan struct{}),
		disableXFFLogging:      config.Logging.DisableLogForwardedFor,
		disableSourceIPLogging: config.Logging.DisableLogSourceIP,
		redactQueryParams:      config.Logging.RedactQueryParams,
		logger:                 logger,
		logsender:              logsender,
	}

	if config.AccessLog.File != "" {
		file, err := os.OpenFile(config.AccessLog.File, os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0644)
		if err != nil {
			logger.Error("error-creating-accesslog-file", zap.String("filename", config.AccessLog.File), zap.Error(err))
			return nil, err
		}

		accessLogger.addWriter(CustomWriter{Name: "accesslog", Writer: file, PerformTruncate: false})
	}

	if config.AccessLog.EnableStreaming {
		syslogWriter, err := syslog.Dial(config.Logging.SyslogNetwork, config.Logging.SyslogAddr, syslog.LOG_INFO, config.Logging.Syslog)
		if err != nil {
			logger.Error("error-creating-syslog-writer", zap.Error(err))
			return nil, err
		}

		accessLogger.addWriter(CustomWriter{Name: "syslog", Writer: syslogWriter, PerformTruncate: true})
	}

	go accessLogger.Run()
	return accessLogger, nil
}

func (x *FileAndLoggregatorAccessLogger) Run() {
	for {
		select {
		case record := <-x.channel:
			for _, w := range x.writers {
				_, err := record.WriteTo(w.Writer)
				if err != nil {
					x.logger.Error(fmt.Sprintf("error-emitting-access-log-to-writer-%s", w.Name), zap.Error(err))
				}
			}
			record.SendLog(x.logsender)
		case <-x.stopCh:
			return
		}
	}
}

func (x *FileAndLoggregatorAccessLogger) addWriter(writer CustomWriter) {
	x.writers = append(x.writers, writer)
	x.writerCount++
}

func (x *FileAndLoggregatorAccessLogger) FileWriters() []CustomWriter {
	return x.writers
}
func (x *FileAndLoggregatorAccessLogger) WriterCount() int {
	return x.writerCount
}

func (x *FileAndLoggregatorAccessLogger) Stop() {
	close(x.stopCh)
}

func (x *FileAndLoggregatorAccessLogger) Log(r schema.AccessLogRecord) {
	r.DisableXFFLogging = x.disableXFFLogging
	r.DisableSourceIPLogging = x.disableSourceIPLogging
	r.RedactQueryParams = x.redactQueryParams
	x.channel <- r
}
