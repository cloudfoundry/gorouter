package watchdog

import (
	"errors"
	"fmt"
	"net/http"
)

const healthCheckEndpoint = "/healthz"

type Watchdog struct {
	channel chan<- error
	host    string
}

func NewWatchdog(channel chan<- error, host string) (*Watchdog, error) {
	if cap(channel) <= 0 { //if unbuffered channel
		return nil, errors.New("attempted construction of a watchdog with an unbuffered channel")
	}
	return &Watchdog{
		channel: channel,
		host:    host,
	}, nil
}

func (w *Watchdog) HitHealthcheckEndpoint() error {
	response, err := http.DefaultClient.Get(w.host + healthCheckEndpoint)
	if err != nil {
		return err
	}
	// fmt.Errorf("%v", response)
	if response.StatusCode != http.StatusOK {
		w.channel <- errors.New(fmt.Sprintf("%v received from healthcheck endpoint (200 expected)", response.StatusCode))
		close(w.channel)
	}
	return nil
}
