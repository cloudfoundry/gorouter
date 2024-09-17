package monitor

import (
	"time"

	"code.cloudfoundry.org/gorouter/logger"
	"github.com/cloudfoundry/dropsonde/metrics"
	"github.com/uber-go/zap"
)

type Uptime struct {
	logger   logger.Logger
	interval time.Duration
	started  int64
	doneChan chan chan struct{}
}

func NewUptime(interval time.Duration, logger logger.Logger) *Uptime {
	return &Uptime{
		interval: interval,
		started:  time.Now().Unix(),
		doneChan: make(chan chan struct{}),
		logger:   logger,
	}
}

func (u *Uptime) Start() {
	ticker := time.NewTicker(u.interval)

	for {
		select {
		case <-ticker.C:
			err := metrics.SendValue("uptime", float64(time.Now().Unix()-u.started), "seconds")
			if err != nil {
				u.logger.Debug("failed-to-send-metric", zap.Error(err), zap.String("metric", "uptime"))
			}
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
