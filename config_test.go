package router

import (
	. "launchpad.net/gocheck"
)

type ConfigSuite struct {
	config *Config
}

var _ = Suite(&ConfigSuite{})

func (s *ConfigSuite) SetUpTest(c *C) {
	s.config = &Config{
		Port:       8080,
		SessionKey: "14fbc303b76bacd1e0a3ab641c11d114",
		Status:     StatusConfig{8081, "user", "pass"},
		Nats:       NatsConfig{"nats://nats:nats@localhost:4222", "localhost:4222", "user", "pass"},
	}
}

func (s *ConfigSuite) TestInitConfig(c *C) {
	InitConfig(s.config)
}

func (s *ConfigSuite) TestInitNats(c *C) {
	// init with nats uri but without username:password
	s.config.Nats = NatsConfig{"nats://localhost:4222", "", "", ""}
	InitConfig(s.config)

	// init with nats host but without username:password
	s.config.Nats = NatsConfig{"", "localhost:4222", "", ""}
	InitConfig(s.config)
}
