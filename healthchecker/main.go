package main

import (
	"context"
	"flag"
	"fmt"
	"net/url"
	"os"
	"os/signal"
	"syscall"
	"time"

	"code.cloudfoundry.org/gorouter/config"
	"code.cloudfoundry.org/gorouter/healthchecker/watchdog"
	goRouterLogger "code.cloudfoundry.org/gorouter/logger"
	"code.cloudfoundry.org/lager"
	"github.com/uber-go/zap"
)

const (
	SIGNAL_BUFFER_SIZE   = 1024
	STARTUP_DELAY_BUFFER = 5 * time.Second
)

func main() {
	var configFile string
	flag.StringVar(&configFile, "c", "", "Configuration File")
	flag.Parse()

	prefix := "healthchecker.stdout"

	tmpLogger := goRouterLogger.NewLogger(
		prefix,
		"unix-epoch",
		zap.Level(lager.INFO),
		zap.Output(os.Stdout),
	)

	c, err := config.DefaultConfig()
	if err != nil {
		tmpLogger.Fatal("Error loading config:", zap.Error(err))
	}

	if configFile != "" {
		c, err = config.InitConfigFromFile(configFile)
		if err != nil {
			tmpLogger.Fatal("Error loading config:", zap.Error(err))
		}
	}

	var logLevel zap.Level
	logLevel.UnmarshalText([]byte(c.Logging.Level))

	logger := goRouterLogger.NewLogger(
		prefix,
		c.Logging.Format.Timestamp,
		logLevel,
		zap.Output(os.Stdout),
	)

	startupDelay := c.StartResponseDelayInterval + STARTUP_DELAY_BUFFER
	logger.Debug("Sleeping before gorouter responds to /health endpoint on startup", zap.Float64("sleep_time_seconds", startupDelay.Seconds()))
	time.Sleep(startupDelay)

	logger.Info("Starting")

	u := &url.URL{
		Scheme: "http",
		Host:   fmt.Sprintf("%s:%d", c.Status.Host, c.Status.Port),
		User:   url.UserPassword(c.Status.User, c.Status.Pass),
	}
	host := u.String()

	w := watchdog.NewWatchdog(host, c.HealthCheckPollInterval, c.HealthCheckTimeout, logger)
	signals := make(chan os.Signal, SIGNAL_BUFFER_SIZE)
	signal.Notify(signals, syscall.SIGUSR1)

	err = w.WatchHealthcheckEndpoint(context.Background(), signals)
	if err != nil {
		logger.Fatal("Error running healthcheck:", zap.Error(err))
	}
}
