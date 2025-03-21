package accesslog

import (
	"fmt"
	"io"
	"log/slog"
	"os"

	"code.cloudfoundry.org/gorouter/accesslog/schema"
	"code.cloudfoundry.org/gorouter/accesslog/syslog"
	"code.cloudfoundry.org/gorouter/config"
	log "code.cloudfoundry.org/gorouter/logger"
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
	logger                 *slog.Logger
	logsender              schema.LogSender
}

type CustomWriter interface {
	Name() string
	io.Writer
}

// SyslogWriter sends logs to a [syslog.Writer].
type SyslogWriter struct {
	name     string
	truncate int
	*syslog.Writer
}

func (w *SyslogWriter) Name() string {
	return w.name
}

func (w *SyslogWriter) Write(b []byte) (int, error) {
	n := len(b)
	if w.truncate > 0 && n > w.truncate {
		n = w.truncate
	}
	return w.Writer.Write(b[:n])
}

// FileWriter sends logs to a [os.File] and appends a new line to each line written to seperate log
// lines.
type FileWriter struct {
	name string
	*os.File
}

func (w *FileWriter) Name() string {
	return w.name
}

func (w *FileWriter) Write(b []byte) (int, error) {
	n, err := w.File.Write(b)
	if err != nil {
		return n, err
	}

	// Do not count the extra bytes, we can not return more than len(b).
	_, err = w.File.Write([]byte{'\n'})
	return n, err
}

func CreateRunningAccessLogger(logger *slog.Logger, logsender schema.LogSender, config *config.Config) (AccessLogger, error) {
	if config.AccessLog.File == "" && !config.Logging.LoggregatorEnabled && !config.AccessLog.EnableStreaming {
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
			logger.Error("error-creating-accesslog-file", slog.String("filename", config.AccessLog.File), log.ErrAttr(err))
			return nil, err
		}

		accessLogger.addWriter(&FileWriter{
			name: "accesslog",
			File: file,
		})
	}

	if config.AccessLog.EnableStreaming {
		syslogWriter, err := syslog.Dial(config.Logging.SyslogNetwork, config.Logging.SyslogAddr, syslog.SeverityInfo, syslog.FacilityUser, config.Logging.Syslog)
		if err != nil {
			logger.Error("error-creating-syslog-writer", log.ErrAttr(err))
			return nil, err
		}

		accessLogger.addWriter(&SyslogWriter{
			name:     "syslog",
			truncate: config.Logging.SyslogTruncate,
			Writer:   syslogWriter,
		})
	}

	go accessLogger.Run()
	return accessLogger, nil
}

func (x *FileAndLoggregatorAccessLogger) Run() {
	for {
		select {
		case record := <-x.channel:
			for _, w := range x.writers {
				_, err := record.WriteTo(w)
				if err != nil {
					x.logger.Error(fmt.Sprintf("error-emitting-access-log-to-writer-%s", w.Name()), log.ErrAttr(err))
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
