package yagnats

import (
	"errors"
	"sync"
	"time"

	"github.com/apcera/nats"
)

type ApceraWrapperNATSClient interface {
	Ping() bool
	Connect() error
	Disconnect()
	Publish(subject string, payload []byte) error
	PublishWithReplyTo(subject, reply string, payload []byte) error
	Subscribe(subject string, handler nats.MsgHandler) (*nats.Subscription, error)
	SubscribeWithQueue(subject, queue string, handler nats.MsgHandler) (*nats.Subscription, error)
	Unsubscribe(subscription *nats.Subscription) error

	AddReconnectedCB(func(ApceraWrapperNATSClient))
}

type ApceraWrapper struct {
	options      nats.Options
	conn         *nats.Conn
	reconnectcbs []func(ApceraWrapperNATSClient)
	*sync.Mutex
}

func NewApceraClientWrapper(urls []string) *ApceraWrapper {
	options := nats.DefaultOptions
	options.Servers = urls
	options.ReconnectWait = 500 * time.Millisecond
	options.MaxReconnect = -1
	c := &ApceraWrapper{
		options:      options,
		reconnectcbs: []func(ApceraWrapperNATSClient){},
		Mutex:        &sync.Mutex{},
	}
	c.options.ReconnectedCB = c.reconnectcb
	return c
}

func (c *ApceraWrapper) reconnectcb(conn *nats.Conn) {
	c.Lock()
	callbacks := make([]func(ApceraWrapperNATSClient), len(c.reconnectcbs))
	copy(callbacks, c.reconnectcbs)
	c.Unlock()

	for _, cb := range callbacks {
		cb(c)
	}
}

func (c *ApceraWrapper) AddReconnectedCB(handler func(ApceraWrapperNATSClient)) {
	c.Lock()
	c.reconnectcbs = append(c.reconnectcbs, handler)
	c.Unlock()
}

func (c *ApceraWrapper) connection() *nats.Conn {
	c.Lock()
	conn := c.conn
	c.Unlock()
	return conn
}

func (c *ApceraWrapper) Connect() error {
	c.Lock()
	defer c.Unlock()

	if c.conn != nil && !c.conn.IsClosed() {
		return errors.New("already connected")
	}

	conn, err := c.options.Connect()
	if err != nil {
		return err
	}

	c.conn = conn

	return nil
}

func (c *ApceraWrapper) Disconnect() {
	conn := c.connection()

	if conn != nil {
		conn.Close()
	}
}

func (c *ApceraWrapper) Publish(subject string, payload []byte) error {
	conn := c.connection()

	if conn == nil {
		return errors.New("not connected")
	}

	return conn.Publish(subject, payload)
}

func (c *ApceraWrapper) Ping() bool {
	conn := c.connection()

	if conn == nil {
		return false
	}

	err := conn.FlushTimeout(500 * time.Millisecond)
	return err == nil
}

func (c *ApceraWrapper) PublishWithReplyTo(subject, reply string, payload []byte) error {
	conn := c.connection()

	if conn == nil {
		return errors.New("not connected")
	}

	return conn.PublishRequest(subject, reply, payload)
}

func (c *ApceraWrapper) Subscribe(subject string, handler nats.MsgHandler) (*nats.Subscription, error) {
	conn := c.connection()
	if conn == nil {
		return nil, errors.New("not connected")
	}

	return conn.Subscribe(subject, handler)
}

func (c *ApceraWrapper) SubscribeWithQueue(subject, queue string, handler nats.MsgHandler) (*nats.Subscription, error) {
	conn := c.connection()
	if conn == nil {
		return nil, errors.New("not connected")
	}

	return conn.QueueSubscribe(subject, queue, handler)
}

func (c *ApceraWrapper) Unsubscribe(subscription *nats.Subscription) error {
	return subscription.Unsubscribe()
}
