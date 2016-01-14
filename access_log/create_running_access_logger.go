package access_log

import (
	"log"
	"strconv"

	syslog "github.com/cloudfoundry/gosteno/syslog"

	"github.com/cloudfoundry/gorouter/config"
	steno "github.com/cloudfoundry/gosteno"

	"os"
)

func CreateRunningAccessLogger(config *config.Config) (AccessLogger, error) {

	if config.AccessLog == "" && config.Logging.Syslog == "" && !config.Logging.LoggregatorEnabled {
		return &NullAccessLogger{}, nil
	}

	logger := steno.NewLogger("access_log")

	var err error
	var file *os.File
	if config.AccessLog != "" {
		file, err = os.OpenFile(config.AccessLog, os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0666)
		if err != nil {
			logger.Errorf("Error creating accesslog file, %s: (%s)", config.AccessLog, err.Error())
			return nil, err
		}
	}

	var syslogLogger *log.Logger
	if config.Logging.Syslog != "" {
		syslogLogger, err = newSyslogLogger(config.Logging.Syslog, 0)
		if err != nil {
			logger.Errorf("Error creating syslog logger: (%s)", err.Error())
			return nil, err
		}
	}

	var dropsondeSourceInstance string
	if config.Logging.LoggregatorEnabled {
		dropsondeSourceInstance = strconv.FormatUint(uint64(config.Index), 10)
	}

	accessLogger := NewFileAndLoggregatorAccessLogger(file, dropsondeSourceInstance, syslogLogger)
	go accessLogger.Run()
	return accessLogger, nil
}

func newSyslogLogger(namespace string, logFlag int) (*log.Logger, error) {
	s, err := syslog.New(syslog.LOG_DEBUG, namespace)
	if err != nil {
		return nil, err
	}
	return log.New(s, "", logFlag), nil
}
