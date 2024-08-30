package mbus

import (
	"log/slog"
	"net/url"
	"sync/atomic"
	"time"

	"code.cloudfoundry.org/tlsconfig"
	"github.com/nats-io/nats.go"

	"code.cloudfoundry.org/gorouter/config"
	log "code.cloudfoundry.org/gorouter/logger"
)

type Signal struct{}

//go:generate counterfeiter -o fakes/fake_client.go . Client
type Client interface {
	Subscribe(subj string, cb nats.MsgHandler) (*nats.Subscription, error)
	Publish(subj string, data []byte) error
}

func Connect(c *config.Config, reconnected chan<- Signal, l *slog.Logger) *nats.Conn {
	var natsClient *nats.Conn
	var natsHost atomic.Value
	var natsAddr atomic.Value
	var err error

	options := natsOptions(l, c, &natsHost, &natsAddr, reconnected)
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
		log.Fatal(l, "nats-connection-error", log.ErrAttr(err))
	}

	var natsHostStr string
	natsURL, err := url.Parse(natsClient.ConnectedUrl())
	if err == nil {
		natsHostStr = natsURL.Host
	}
	natsAddrStr := natsClient.ConnectedAddr()

	l.Info("Successfully-connected-to-nats", slog.String("host", natsHostStr), slog.String("addr", natsAddrStr))

	natsHost.Store(natsHostStr)
	natsAddr.Store(natsAddrStr)
	return natsClient
}

func natsOptions(l *slog.Logger, c *config.Config, natsHost *atomic.Value, natsAddr *atomic.Value, reconnected chan<- Signal) nats.Options {
	options := nats.GetDefaultOptions()
	options.Servers = c.NatsServers()
	if c.Nats.TLSEnabled {
		var err error
		options.TLSConfig, err = tlsconfig.Build(
			tlsconfig.WithInternalServiceDefaults(),
			tlsconfig.WithIdentity(c.Nats.ClientAuthCertificate),
		).Client(
			tlsconfig.WithAuthority(c.Nats.CAPool),
		)
		if err != nil {
			log.Fatal(l, "nats-tls-config-invalid", log.ErrAttr(err))
		}
	}
	options.PingInterval = c.NatsClientPingInterval
	options.MaxReconnect = -1
	notDisconnected := make(chan Signal)

	options.ClosedCB = func(conn *nats.Conn) {
		log.Fatal(
			l, "nats-connection-closed",
			slog.String("error", "unexpected close"),
			slog.String("last_error", conn.LastError().Error()),
		)
	}

	options.DisconnectedCB = func(conn *nats.Conn) {
		hostStr := natsHost.Load().(string)
		addrStr := natsAddr.Load().(string)
		l.Info("nats-connection-disconnected", slog.String("host", hostStr), slog.String("addr", addrStr))

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
			l.Error("nats-url-parse-error", log.ErrAttr(err))
		} else {
			natsHostStr = natsURL.Host
		}
		natsAddrStr := conn.ConnectedAddr()
		natsHost.Store(natsHostStr)
		natsAddr.Store(natsAddrStr)

		l.Info("nats-connection-reconnected", slog.String("host", natsHostStr), slog.String("addr", natsAddrStr))
		reconnected <- Signal{}
	}

	return options
}
