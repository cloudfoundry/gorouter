package yagnats

import (
	"sync"
	"time"

	"github.com/apcera/nats"
)

type NATSConn interface {
	Close()
	Publish(subject string, data []byte) error
	PublishRequest(subj, reply string, data []byte) error
	Subscribe(subject string, handler nats.MsgHandler) (*nats.Subscription, error)
	QueueSubscribe(subject, queue string, handler nats.MsgHandler) (*nats.Subscription, error)
	Unsubscribe(sub *nats.Subscription) error
	Ping() bool
	AddReconnectedCB(func(*nats.Conn))
	AddClosedCB(func(*nats.Conn))
	AddDisconnectedCB(func(*nats.Conn))
}

type apceraNATSWrapper struct {
	*nats.Conn
	reconnectCbs    *[]func(*nats.Conn)
	closedCbs       *[]func(*nats.Conn)
	disconnectedCbs *[]func(*nats.Conn)
	*sync.Mutex
}

func Connect(urls []string) (NATSConn, error) {
	options := nats.DefaultOptions
	options.Servers = urls
	options.ReconnectWait = 500 * time.Millisecond
	options.MaxReconnect = -1

	reconnectCallbacks := make([]func(*nats.Conn), 0)
	closedCallbacks := make([]func(*nats.Conn), 0)
	disconnectedCallbacks := make([]func(*nats.Conn), 0)

	s := &apceraNATSWrapper{
		nil,
		&reconnectCallbacks,
		&closedCallbacks,
		&disconnectedCallbacks,
		&sync.Mutex{},
	}

	options.ReconnectedCB = s.apceraReconnectCB
	options.ClosedCB = s.apceraClosedCB
	options.DisconnectedCB = s.apceraDisconnectedCB

	conn, err := options.Connect()
	if err != nil {
		return nil, err
	}

	s.Conn = conn
	return s, nil
}

func (c *apceraNATSWrapper) AddReconnectedCB(handler func(*nats.Conn)) {
	c.Lock()
	defer c.Unlock()
	callbacks := *c.reconnectCbs
	callbacks = append(callbacks, handler)
	c.reconnectCbs = &callbacks
}

func (c *apceraNATSWrapper) AddClosedCB(handler func(*nats.Conn)) {
	c.Lock()
	defer c.Unlock()
	callbacks := *c.closedCbs
	callbacks = append(callbacks, handler)
	c.closedCbs = &callbacks
}

func (c *apceraNATSWrapper) AddDisconnectedCB(handler func(*nats.Conn)) {
	c.Lock()
	defer c.Unlock()
	callbacks := *c.disconnectedCbs
	callbacks = append(callbacks, handler)
	c.disconnectedCbs = &callbacks
}

func (c *apceraNATSWrapper) Unsubscribe(sub *nats.Subscription) error {
	return sub.Unsubscribe()
}

func (c *apceraNATSWrapper) Ping() bool {
	err := c.FlushTimeout(500 * time.Millisecond)
	return err == nil
}

func (c *apceraNATSWrapper) apceraReconnectCB(conn *nats.Conn) {
	c.Lock()
	defer c.Unlock()
	for _, cb := range *c.reconnectCbs {
		cb(conn)
	}
}

func (c *apceraNATSWrapper) apceraClosedCB(conn *nats.Conn) {
	c.Lock()
	defer c.Unlock()
	for _, cb := range *c.closedCbs {
		cb(conn)
	}
}

func (c *apceraNATSWrapper) apceraDisconnectedCB(conn *nats.Conn) {
	c.Lock()
	defer c.Unlock()
	for _, cb := range *c.disconnectedCbs {
		cb(conn)
	}
}
