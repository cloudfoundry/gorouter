package access_log

import (
	"io"
	"regexp"

	"fmt"
	"github.com/cloudfoundry/dropsonde/logs"
	"github.com/pivotal-golang/lager"
	"strconv"

	"github.com/cloudfoundry/gorouter/access_log/schema"
	"github.com/cloudfoundry/gorouter/config"

	"os"
)

//go:generate counterfeiter -o fakes/fake_access_logger.go . AccessLogger
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
	dropsondeSourceInstance string
	channel                 chan schema.AccessLogRecord
	stopCh                  chan struct{}
	writer                  io.Writer
	writerCount             int
	logger                  lager.Logger
}

func CreateRunningAccessLogger(logger lager.Logger, config *config.Config) (AccessLogger, error) {

	if config.AccessLog.File == "" && !config.Logging.LoggregatorEnabled {
		return &NullAccessLogger{}, nil
	}

	var err error
	var file *os.File
	var writers []io.Writer
	if config.AccessLog.File != "" {
		file, err = os.OpenFile(config.AccessLog.File, os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0666)
		if err != nil {
			logger.Error(fmt.Sprintf("Error creating accesslog file, %s", config.AccessLog.File), err)
			return nil, err
		}
		writers = append(writers, file)
	}

	if config.AccessLog.EnableStreaming {
		writers = append(writers, os.Stdout)
	}

	var dropsondeSourceInstance string
	if config.Logging.LoggregatorEnabled {
		dropsondeSourceInstance = strconv.FormatUint(uint64(config.Index), 10)
	}

	accessLogger := NewFileAndLoggregatorAccessLogger(logger, dropsondeSourceInstance, writers...)
	go accessLogger.Run()
	return accessLogger, nil
}

func NewFileAndLoggregatorAccessLogger(logger lager.Logger, dropsondeSourceInstance string, ws ...io.Writer) *FileAndLoggregatorAccessLogger {
	a := &FileAndLoggregatorAccessLogger{
		dropsondeSourceInstance: dropsondeSourceInstance,
		channel:                 make(chan schema.AccessLogRecord, 128),
		stopCh:                  make(chan struct{}),
		logger:                  logger,
	}
	configureWriters(a, ws)
	return a
}

func (x *FileAndLoggregatorAccessLogger) Run() {
	for {
		select {
		case record := <-x.channel:
			if x.writer != nil {
				_, err := record.WriteTo(x.writer)
				if err != nil {
					x.logger.Error("Error when emiting access log to writers ", err)
				}
			}
			if x.dropsondeSourceInstance != "" && record.ApplicationId() != "" {
				logs.SendAppLog(record.ApplicationId(), record.LogMessage(), "RTR", x.dropsondeSourceInstance)
			}
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

func (x *FileAndLoggregatorAccessLogger) DropsondeSourceInstance() string {
	return x.dropsondeSourceInstance
}

func (x *FileAndLoggregatorAccessLogger) Stop() {
	close(x.stopCh)
}

func (x *FileAndLoggregatorAccessLogger) Log(r schema.AccessLogRecord) {
	x.channel <- r
}

var ipAddressRegex, _ = regexp.Compile(`^(([0-9]|[1-9][0-9]|1[0-9]{2}|2[0-4][0-9]|25[0-5])\.){3}([0-9]|[1-9][0-9]|1[0-9]{2}|2[0-4][0-9]|25[0-5])(:[0-9]{1,5}){1}$`)
var hostnameRegex, _ = regexp.Compile(`^(([a-zA-Z0-9]|[a-zA-Z0-9][a-zA-Z0-9\-]*[a-zA-Z0-9])\.)*([A-Za-z0-9]|[A-Za-z0-9][A-Za-z0-9\-]*[A-Za-z0-9])(:[0-9]{1,5}){1}$`)

func isValidUrl(url string) bool {
	return ipAddressRegex.MatchString(url) || hostnameRegex.MatchString(url)
}

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
