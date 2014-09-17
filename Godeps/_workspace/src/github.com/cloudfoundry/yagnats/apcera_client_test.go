package yagnats

import (
	"time"

	"github.com/apcera/nats"

	. "launchpad.net/gocheck"
)

func (s *YSuite) TestApceraDisconnectOnNewClient(c *C) {
	client := NewApceraClientWrapper([]string{"nats://nats:nats@127.0.0.1:4223"})
	client.Disconnect()
}

func (s *YSuite) TestApceraConnectWithInvalidAddress(c *C) {
	badClient := NewApceraClientWrapper([]string{""})

	err := badClient.Connect()

	c.Assert(err, Not(Equals), nil)
	c.Assert(err.Error(), Equals, "dial tcp: missing address")
}

func (s *YSuite) TestApceraClientConnectWithInvalidAuth(c *C) {
	badClient := NewApceraClientWrapper([]string{"nats://cats:bats@127.0.0.1:4223"})

	err := badClient.Connect()

	c.Assert(err, Not(Equals), nil)
}

func (s *YSuite) TestApceraClientPing(c *C) {
	c.Assert(s.ApceraWrapper.Ping(), Equals, true)
}

func (s *YSuite) TestApceraClientPingWhenNotConnected(c *C) {
	disconnectedClient := NewApceraClientWrapper([]string{"nats://nats:nats@127.0.0.1:4223"})
	c.Assert(disconnectedClient.Ping(), Equals, false)
}

func (s *YSuite) TestApceraClientPingWhenConnectionClosed(c *C) {
	s.ApceraWrapper.Disconnect()
	c.Assert(s.ApceraWrapper.Ping(), Equals, false)
}

func (s *YSuite) TestApceraClientSubscribe(c *C) {
	sub, _ := s.ApceraWrapper.Subscribe("some.subject", func(msg *nats.Msg) {})
	c.Assert(sub.Subject, Equals, "some.subject")
}

func (s *YSuite) TestApceraClientUnsubscribe(c *C) {
	payload1 := make(chan []byte)
	payload2 := make(chan []byte)

	sid1, _ := s.ApceraWrapper.Subscribe("some.subject", func(msg *nats.Msg) {
		payload1 <- msg.Data
	})

	s.ApceraWrapper.Subscribe("some.subject", func(msg *nats.Msg) {
		payload2 <- msg.Data
	})

	s.ApceraWrapper.Publish("some.subject", []byte("hello!"))

	waitReceive(c, "hello!", payload1, 500)
	waitReceive(c, "hello!", payload2, 500)

	s.ApceraWrapper.Unsubscribe(sid1)

	s.ApceraWrapper.Publish("some.subject", []byte("hello!"))

	select {
	case <-payload1:
		c.Error("Should not have received message.")
	case <-time.After(500 * time.Millisecond):
	}

	waitReceive(c, "hello!", payload2, 500)
}

func (s *YSuite) TestApceraClientSubscribeAndUnsubscribe(c *C) {
	payload := make(chan []byte)

	sid1, _ := s.ApceraWrapper.Subscribe("some.subject", func(msg *nats.Msg) {
		payload <- msg.Data
	})

	s.ApceraWrapper.Publish("some.subject", []byte("hello!"))

	waitReceive(c, "hello!", payload, 500)

	s.ApceraWrapper.Unsubscribe(sid1)

	s.ApceraWrapper.Subscribe("some.subject", func(msg *nats.Msg) {
		payload <- msg.Data
	})

	s.ApceraWrapper.Publish("some.subject", []byte("hello!"))

	waitReceive(c, "hello!", payload, 500)

	select {
	case <-payload:
		c.Error("Should not have received message.")
	case <-time.After(500 * time.Millisecond):
	}
}

func (s *YSuite) TestApceraClientPubSub(c *C) {
	payload := make(chan []byte)

	s.ApceraWrapper.Subscribe("some.subject", func(msg *nats.Msg) {
		payload <- msg.Data
	})

	s.ApceraWrapper.Publish("some.subject", []byte("hello!"))

	waitReceive(c, "hello!", payload, 500)
}

func (s *YSuite) TestApceraClientPubSubWithQueue(c *C) {
	payload := make(chan []byte)

	s.ApceraWrapper.SubscribeWithQueue("some.subject", "some-queue", func(msg *nats.Msg) {
		payload <- msg.Data
	})

	s.ApceraWrapper.SubscribeWithQueue("some.subject", "some-queue", func(msg *nats.Msg) {
		payload <- msg.Data
	})

	s.ApceraWrapper.Publish("some.subject", []byte("hello!"))

	waitReceive(c, "hello!", payload, 500)

	select {
	case <-payload:
		c.Error("Should not have received message.")
	case <-time.After(500 * time.Millisecond):
	}
}

func (s *YSuite) TestApceraClientPublishWithReply(c *C) {
	payload := make(chan []byte)

	s.ApceraWrapper.Subscribe("some.request", func(msg *nats.Msg) {
		s.ApceraWrapper.Publish(msg.Reply, []byte("response!"))
	})

	s.ApceraWrapper.Subscribe("some.reply", func(msg *nats.Msg) {
		payload <- msg.Data
	})

	s.ApceraWrapper.PublishWithReplyTo("some.request", "some.reply", []byte("hello!"))

	waitReceive(c, "response!", payload, 500)
}
