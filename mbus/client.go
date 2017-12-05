package mbus

import (
	"errors"
	"net/url"
	"sync/atomic"
	"time"

	"code.cloudfoundry.org/gorouter/config"
	"code.cloudfoundry.org/gorouter/logger"
	"github.com/nats-io/go-nats"
	"github.com/uber-go/zap"
)

type Signal struct{}

//go:generate counterfeiter -o fakes/fake_client.go . Client
type Client interface {
	Subscribe(subj string, cb nats.MsgHandler) (*nats.Subscription, error)
	Publish(subj string, data []byte) error
}

func Connect(c *config.Config, reconnected chan<- Signal, l logger.Logger) *nats.Conn {
	var natsClient *nats.Conn
	var natsHost atomic.Value
	var err error

	options := natsOptions(l, c, &natsHost, reconnected)
	attempts := 3
	for attempts > 0 {
		natsClient, err = options.Connect()
		if err == nil {
			break
		} else {
			attempts--
			time.Sleep(100 * time.Millisecond)
		}
	}

	if err != nil {
		l.Fatal("nats-connection-error", zap.Error(err))
	}

	var natsHostStr string
	natsURL, err := url.Parse(natsClient.ConnectedUrl())
	if err == nil {
		natsHostStr = natsURL.Host
	}

	l.Info("Successfully-connected-to-nats", zap.String("host", natsHostStr))

	natsHost.Store(natsHostStr)
	return natsClient
}

func natsOptions(l logger.Logger, c *config.Config, natsHost *atomic.Value, reconnected chan<- Signal) nats.Options {
	natsServers := c.NatsServers()

	options := nats.DefaultOptions
	options.Servers = natsServers
	options.PingInterval = c.NatsClientPingInterval
	options.MaxReconnect = -1
	notDisconnected := make(chan Signal)

	options.ClosedCB = func(conn *nats.Conn) {
		l.Fatal(
			"nats-connection-closed",
			zap.Error(errors.New("unexpected close")),
			zap.Object("last_error", conn.LastError()),
		)
	}

	options.DisconnectedCB = func(conn *nats.Conn) {
		hostStr := natsHost.Load().(string)
		l.Info("nats-connection-disconnected", zap.String("nats-host", hostStr))

		go func() {
			ticker := time.NewTicker(c.NatsClientPingInterval)

			for {
				select {
				case <-notDisconnected:
					return
				case <-ticker.C:
					l.Info("nats-connection-still-disconnected")
				}
			}
		}()
	}

	options.ReconnectedCB = func(conn *nats.Conn) {
		notDisconnected <- Signal{}

		natsURL, err := url.Parse(conn.ConnectedUrl())
		natsHostStr := ""
		if err != nil {
			l.Error("nats-url-parse-error", zap.Error(err))
		} else {
			natsHostStr = natsURL.Host
		}
		natsHost.Store(natsHostStr)

		l.Info("nats-connection-reconnected", zap.String("nats-host", natsHostStr))
		reconnected <- Signal{}
	}

	return options
}
