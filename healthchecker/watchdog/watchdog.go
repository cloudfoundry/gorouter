package watchdog

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"syscall"
	"time"

	goRouterLogger "code.cloudfoundry.org/gorouter/logger"
)

const healthCheckEndpoint = "/healthz"

type Watchdog struct {
	host         string
	pollInterval time.Duration
	client       http.Client
	logger       goRouterLogger.Logger
}

func NewWatchdog(host string, pollInterval time.Duration, healthcheckTimeout time.Duration, logger goRouterLogger.Logger) *Watchdog {
	client := http.Client{
		Timeout: healthcheckTimeout,
	}
	return &Watchdog{
		host:         host,
		pollInterval: pollInterval,
		client:       client,
		logger:       logger,
	}
}

func (w *Watchdog) WatchHealthcheckEndpoint(ctx context.Context, signals <-chan os.Signal) error {
	pollTimer := time.NewTimer(w.pollInterval)
	defer pollTimer.Stop()
	for {
		select {
		case <-ctx.Done():
			w.logger.Info("Context done, exiting")
			return nil
		case sig := <-signals:
			if sig == syscall.SIGUSR1 {
				w.logger.Info("Received USR1 signal, exiting")
				return nil
			}
		case <-pollTimer.C:
			w.logger.Debug("Verifying gorouter endpoint")
			err := w.HitHealthcheckEndpoint()
			if err != nil {
				select {
				case sig := <-signals:
					if sig == syscall.SIGUSR1 {
						w.logger.Info("Received USR1 signal, exiting")
						return nil
					}
				default:
					return err
				}
			}
			pollTimer.Reset(w.pollInterval)
		}
	}
}

func (w *Watchdog) HitHealthcheckEndpoint() error {
	response, err := w.client.Get(w.host + healthCheckEndpoint)
	if err != nil {
		return err
	}
	// fmt.Printf("status: %d", response.StatusCode)
	if response.StatusCode != http.StatusOK {
		return errors.New(fmt.Sprintf(
			"%v received from healthcheck endpoint (200 expected)",
			response.StatusCode))
	}
	return nil
}
