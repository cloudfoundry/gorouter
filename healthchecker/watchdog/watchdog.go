package watchdog

import (
	"context"
	"errors"
	"fmt"
	"net/htp"
	"time"
)

const healthCheckEndpoint = "/healthz"

type Watchdog struct {
	host         string
	pollInterval time.Duration
	client       http.Client
}

func NewWatchdog(host string, pollInterval time.Duration, healthcheckTimeout time.Duration) (*Watchdog, error) {
	client := http.Client{
		Timeout: healthcheckTimeout,
	}
	return &Watchdog{
		host:         host,
		pollInterval: pollInterval,
		client:       client,
	}, nil
}

func (w *Watchdog) WatchHealthcheckEndpoint(ctx context.Context) error {
	pollTimer := time.NewTimer(w.pollInterval)
	defer pollTimer.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-pollTimer.C:
			err := w.HitHealthcheckEndpoint()
			if err != nil {
				return err
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
