package access_log

import (
	"errors"
	"fmt"
	"log"
	"strconv"

	syslog "github.com/cloudfoundry/gosteno/syslog"

	"github.com/cloudfoundry/gorouter/config"
	steno "github.com/cloudfoundry/gosteno"

	"os"
)

func CreateRunningAccessLogger(config *config.Config) (AccessLogger, error) {

	if config.AccessLog == "" && !config.Logging.LoggregatorEnabled {
		return &NullAccessLogger{}, nil
	}

	logger := steno.NewLogger("access_log")

	var syslogLogger *log.Logger
	var err error
	if config.AccessLogSyslog.Enabled {
		syslogLogger, err = newLogger(config.AccessLogSyslog.Level, config.AccessLogSyslog.Syslog, 0)
		if err != nil {
			logger.Errorf("Error creating syslog logger: (%s)", err.Error())
			return nil, err
		}
	}

	var file *os.File
	if config.AccessLog != "" {
		file, err = os.OpenFile(config.AccessLog, os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0666)
		if err != nil {
			logger.Errorf("Error creating accesslog file, %s: (%s)", config.AccessLog, err.Error())
			return nil, err
		}
	}

	var dropsondeSourceInstance string
	if config.Logging.LoggregatorEnabled {
		dropsondeSourceInstance = strconv.FormatUint(uint64(config.Index), 10)
	}

	accessLogger := NewFileAndLoggregatorAccessLogger(file, dropsondeSourceInstance, syslogLogger, AppIdFilter(config.AccessLogSyslog.AppIdFilter))
	go accessLogger.Run()
	return accessLogger, nil
}

func newLogger(level string, namespace string, logFlag int) (*log.Logger, error) {
	p, err := toSyslogPriority(level)
	if err != nil {
		return nil, err
	}
	s, err := syslog.New(p, namespace)
	if err != nil {
		return nil, err
	}
	return log.New(s, "", logFlag), nil
}

func toSyslogPriority(level string) (syslog.Priority, error) {
	l, err := steno.GetLogLevel(level)
	if err != nil {
		return -1, err
	}
	switch l {
	case steno.LOG_FATAL:
		return syslog.LOG_CRIT, nil
	case steno.LOG_ERROR:
		return syslog.LOG_ERR, nil
	case steno.LOG_WARN:
		return syslog.LOG_WARNING, nil
	case steno.LOG_INFO:
		return syslog.LOG_INFO, nil
	case steno.LOG_DEBUG, steno.LOG_DEBUG1, steno.LOG_DEBUG2:
		return syslog.LOG_DEBUG, nil
	default:
		return -1, errors.New(fmt.Sprintf("Unknown log level %s", l.Name))
	}
}
