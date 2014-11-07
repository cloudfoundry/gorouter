package access_log

import (
	"io"
	"regexp"

	"github.com/cloudfoundry/dropsonde/logs"
)

type FileAndLoggregatorAccessLogger struct {
	dropsondeSourceInstance string
	channel                 chan AccessLogRecord
	stopCh                  chan struct{}
	writer                  io.Writer
}

func NewFileAndLoggregatorAccessLogger(f io.Writer, dropsondeSourceInstance string) *FileAndLoggregatorAccessLogger {
	a := &FileAndLoggregatorAccessLogger{
		dropsondeSourceInstance: dropsondeSourceInstance,
		writer:                  f,
		channel:                 make(chan AccessLogRecord, 128),
		stopCh:                  make(chan struct{}),
	}

	return a
}

func (x *FileAndLoggregatorAccessLogger) Run() {
	for {
		select {
		case record := <-x.channel:
			if x.writer != nil {
				record.WriteTo(x.writer)
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
