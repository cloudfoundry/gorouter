package access_log

import (
	"fmt"
	"strconv"

	"github.com/cloudfoundry/gorouter/config"
	"github.com/pivotal-golang/lager"

	"os"
)

func CreateRunningAccessLogger(logger lager.Logger, config *config.Config) (AccessLogger, error) {

	if config.AccessLog == "" && !config.Logging.LoggregatorEnabled {
		return &NullAccessLogger{}, nil
	}

	var err error
	var file *os.File
	if config.AccessLog != "" {
		file, err = os.OpenFile(config.AccessLog, os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0666)
		if err != nil {
			logger.Error(fmt.Sprintf("Error creating accesslog file, %s", config.AccessLog), err)
			return nil, err
		}
	}

	var dropsondeSourceInstance string
	if config.Logging.LoggregatorEnabled {
		dropsondeSourceInstance = strconv.FormatUint(uint64(config.Index), 10)
	}

	accessLogger := NewFileAndLoggregatorAccessLogger(file, dropsondeSourceInstance)
	go accessLogger.Run()
	return accessLogger, nil
}
