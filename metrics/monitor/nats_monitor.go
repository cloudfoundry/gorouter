package monitor

import (
	"os"
	"time"

	"code.cloudfoundry.org/gorouter/logger"
	"github.com/cloudfoundry/dropsonde/metrics"
	"github.com/uber-go/zap"
)

//go:generate counterfeiter -o ../fakes/fake_subscriber.go . Subscriber
type Subscriber interface {
	Pending() (int, error)
}

type NATSMonitor struct {
	Subscriber Subscriber
	Sender     metrics.MetricSender
	TickChan   <-chan time.Time
	Logger     logger.Logger
}

func (n *NATSMonitor) Run(signals <-chan os.Signal, ready chan<- struct{}) error {
	close(ready)
	for {
		select {
		case <-n.TickChan:
			queuedMsgs, err := n.Subscriber.Pending()
			if err != nil {
				n.Logger.Error("error-retrieving-nats-subscription-pending-messages", zap.Error(err))
			}
			chainer := n.Sender.Value("buffered_messages", float64(queuedMsgs), "")
			err = chainer.Send()
			if err != nil {
				n.Logger.Error("error-sending-nats-monitor-metric", zap.Error(err))
			}
		case <-signals:
			n.Logger.Info("exited")
			return nil
		}
	}
}
