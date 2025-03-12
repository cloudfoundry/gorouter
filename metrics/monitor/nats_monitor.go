package monitor

import (
	"log/slog"
	"os"
	"time"

	log "code.cloudfoundry.org/gorouter/logger"
	"code.cloudfoundry.org/gorouter/metrics"
)

//go:generate counterfeiter -o ../fakes/fake_subscriber.go . Subscriber
type Subscriber interface {
	Pending() (int, error)
	Dropped() (int, error)
}

type NATSMonitor struct {
	Subscriber Subscriber
	Reporter   metrics.MonitorReporter
	TickChan   <-chan time.Time
	Logger     *slog.Logger
}

func (n *NATSMonitor) Run(signals <-chan os.Signal, ready chan<- struct{}) error {
	close(ready)
	for {
		select {
		case <-n.TickChan:
			queuedMsgs, err := n.Subscriber.Pending()
			if err != nil {
				n.Logger.Error("error-retrieving-nats-subscription-pending-messages", log.ErrAttr(err))
			}
			n.Reporter.CaptureNATSBufferedMessages(queuedMsgs)

			droppedMsgs, err := n.Subscriber.Dropped()
			if err != nil {
				n.Logger.Error("error-retrieving-nats-subscription-dropped-messages", log.ErrAttr(err))
			}
			n.Reporter.CaptureNATSDroppedMessages(droppedMsgs)
		case <-signals:
			n.Logger.Info("exited")
			return nil
		}
	}
}
