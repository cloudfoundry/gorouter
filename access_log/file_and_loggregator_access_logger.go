package access_log

import (
	"io"
	"regexp"

	"github.com/cloudfoundry/dropsonde/logs"
	steno "github.com/cloudfoundry/gosteno"
)

type FileAndLoggregatorAccessLogger struct {
	dropsondeSourceInstance string
	channel                 chan AccessLogRecord
	stopCh                  chan struct{}
	writer                  io.Writer
	logger                  *steno.Logger
}

func NewFileAndLoggregatorAccessLogger(logger *steno.Logger, dropsondeSourceInstance string, ws ...io.Writer) *FileAndLoggregatorAccessLogger {
	a := &FileAndLoggregatorAccessLogger{
		dropsondeSourceInstance: dropsondeSourceInstance,
		channel:                 make(chan AccessLogRecord, 128),
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
					x.logger.Infof("Error when emiting access log to writers %s", err.Error())
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

func (x *FileAndLoggregatorAccessLogger) DropsondeSourceInstance() string {
	return x.dropsondeSourceInstance
}

func (x *FileAndLoggregatorAccessLogger) Stop() {
	close(x.stopCh)
}

func (x *FileAndLoggregatorAccessLogger) Log(r AccessLogRecord) {
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
		}
	}
	if len(multiws) > 0 {
		a.writer = io.MultiWriter(multiws...)
	}
}
