package monitor

import (
	"time"

	"github.com/cloudfoundry/dropsonde/metrics"
)

type Uptime struct {
	interval time.Duration
	started  int64
	doneChan chan chan struct{}
}

func NewUptime(interval time.Duration) *Uptime {
	return &Uptime{
		interval: interval,
		started:  time.Now().Unix(),
		doneChan: make(chan chan struct{}),
	}
}

func (u *Uptime) Start() {
	ticker := time.NewTicker(u.interval)

	for {
		select {
		case <-ticker.C:
			metrics.SendValue("uptime", float64(time.Now().Unix()-u.started), "seconds")
		case stopped := <-u.doneChan:
			ticker.Stop()
			close(stopped)
			return
		}
	}
}

func (u *Uptime) Stop() {
	stopped := make(chan struct{})
	u.doneChan <- stopped
	<-stopped
}
