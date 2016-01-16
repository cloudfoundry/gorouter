package access_log

import (
	"fmt"
	"strconv"

	"github.com/cloudfoundry/gorouter/config"
	"github.com/pivotal-golang/lager"

	"os"
	"io"
)

func CreateRunningAccessLogger(logger lager.Logger, config *config.Config) (AccessLogger, error) {

	if config.AccessLog == "" && !config.Logging.LoggregatorEnabled {
		return &NullAccessLogger{}, nil
	}

	var err error
	var file *os.File
	var writers []io.Writer
	if config.AccessLog != "" {
		file, err = os.OpenFile(config.AccessLog, os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0666)
		if err != nil {
			logger.Error(fmt.Sprintf("Error creating accesslog file, %s", config.AccessLog), err)
			return nil, err
		}
		writers = append(writers, file)
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
