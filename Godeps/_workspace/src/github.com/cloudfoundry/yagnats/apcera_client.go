package yagnats

import (
	"sync"
	"time"

	"github.com/apcera/nats"
)

type NATSConn interface {
	Close() //Disconnect?
	Publish(subject string, data []byte) error
	PublishRequest(subj, reply string, data []byte) error
	Subscribe(subject string, handler nats.MsgHandler) (*nats.Subscription, error)
	QueueSubscribe(subject, queue string, handler nats.MsgHandler) (*nats.Subscription, error)
	Unsubscribe(sub *nats.Subscription) error
	Ping() bool // Not actually in nats.Conn, but quite useful
	AddReconnectedCB(func(*nats.Conn))
}

type conn struct {
	*nats.Conn
	reconnectcbs *[]func(*nats.Conn)
	*sync.Mutex
}

func Connect(urls []string) (NATSConn, error) {
	options := nats.DefaultOptions
	options.Servers = urls
	options.ReconnectWait = 500 * time.Millisecond
	options.MaxReconnect = -1

	callbacks := make([]func(*nats.Conn), 0)
	s := &conn{nil, &callbacks, &sync.Mutex{}}

	options.ReconnectedCB = func(conn *nats.Conn) {
		s.Lock()
		defer s.Unlock()
		for _, cb := range *s.reconnectcbs {
			cb(conn)
		}
	}

	conn, err := options.Connect()
	if err != nil {
		return nil, err
	}

	s.Conn = conn
	return s, nil
}

func (c *conn) AddReconnectedCB(handler func(*nats.Conn)) {
	c.Lock()
	defer c.Unlock()
	callbacks := *c.reconnectcbs
	callbacks = append(callbacks, handler)
	c.reconnectcbs = &callbacks
}

func (c *conn) Unsubscribe(sub *nats.Subscription) error {
	return sub.Unsubscribe()
}

func (c *conn) Ping() bool {
	err := c.FlushTimeout(500 * time.Millisecond)
	return err == nil
}
