package accesslog

import (
	"io"
	"log/syslog"
	"regexp"

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
	writer                 io.Writer
	writerCount            int
	disableXFFLogging      bool
	disableSourceIPLogging bool
	logger                 logger.Logger
	logsender              schema.LogSender
}

func CreateRunningAccessLogger(logger logger.Logger, logsender schema.LogSender, config *config.Config) (AccessLogger, error) {
	if config.AccessLog.File == "" && !config.Logging.LoggregatorEnabled {
		return &NullAccessLogger{}, nil
	}

	var err error
	var file *os.File
	var writers []io.Writer
	if config.AccessLog.File != "" {
		file, err = os.OpenFile(config.AccessLog.File, os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0666)
		if err != nil {
			logger.Error("error-creating-accesslog-file", zap.String("filename", config.AccessLog.File), zap.Error(err))
			return nil, err
		}
		writers = append(writers, file)
	}

	if config.AccessLog.EnableStreaming {
		syslogWriter, err := syslog.Dial(config.Logging.SyslogNetwork, config.Logging.SyslogAddr, syslog.LOG_INFO, config.Logging.Syslog)
		if err != nil {
			logger.Error("error-creating-syslog-writer", zap.Error(err))
			return nil, err
		}
		writers = append(writers, syslogWriter)
	}

	accessLogger := &FileAndLoggregatorAccessLogger{
		channel:                make(chan schema.AccessLogRecord, 1024),
		stopCh:                 make(chan struct{}),
		disableXFFLogging:      config.Logging.DisableLogForwardedFor,
		disableSourceIPLogging: config.Logging.DisableLogSourceIP,
		logger:                 logger,
		logsender:              logsender,
	}
	configureWriters(accessLogger, writers)

	go accessLogger.Run()
	return accessLogger, nil
}

func (x *FileAndLoggregatorAccessLogger) Run() {
	for {
		select {
		case record := <-x.channel:
			if x.writer != nil {
				_, err := record.WriteTo(x.writer)
				if err != nil {
					x.logger.Error("error-emitting-access-log-to-writers", zap.Error(err))
				}
			}
			record.SendLog(x.logsender)
		case <-x.stopCh:
			return
		}
	}
}

func (x *FileAndLoggregatorAccessLogger) FileWriter() io.Writer {
	return x.writer
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
	x.channel <- r
}

var ipAddressRegex, _ = regexp.Compile(`^(([0-9]|[1-9][0-9]|1[0-9]{2}|2[0-4][0-9]|25[0-5])\.){3}([0-9]|[1-9][0-9]|1[0-9]{2}|2[0-4][0-9]|25[0-5])(:[0-9]{1,5}){1}$`)
var hostnameRegex, _ = regexp.Compile(`^(([a-zA-Z0-9]|[a-zA-Z0-9][a-zA-Z0-9\-]*[a-zA-Z0-9])\.)*([A-Za-z0-9]|[A-Za-z0-9][A-Za-z0-9\-]*[A-Za-z0-9])(:[0-9]{1,5}){1}$`)

func configureWriters(a *FileAndLoggregatorAccessLogger, ws []io.Writer) {
	var multiws []io.Writer
	for _, w := range ws {
		if w != nil {
			multiws = append(multiws, w)
			a.writerCount++
		}
	}
	if len(multiws) > 0 {
		a.writer = io.MultiWriter(multiws...)
	}
}
