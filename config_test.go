package router

import (
	. "launchpad.net/gocheck"
	vcap "router/common"
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
		Nats:       NatsConfig{"nats://natsuser:natspass@localhost:4222", "", "", ""},
	}

	c.Assert(config, Equals, Config{})
}

func (s *ConfigSuite) TearDownTest(c *C) {
	emptyConfig := Config{}
	s.config = &emptyConfig
	// since global variable 'config' will be modified by InitConfig,it should be
	// reset after every case finished in order to make sure all cases run independent
	config = emptyConfig
}

func (s *ConfigSuite) TestInitConfig(c *C) {
	InitConfig(s.config)

	c.Assert(config.Port, Equals, 8080)
	c.Assert(config.SessionKey, Equals, "14fbc303b76bacd1e0a3ab641c11d114")

	ip, err := vcap.LocalIP()
	c.Assert(err, IsNil)
	c.Assert(config.ip, Equals, ip)
}

func (s *ConfigSuite) TestInitStatus(c *C) {
	InitConfig(s.config)

	c.Assert(config.Status, Equals, StatusConfig{8081, "user", "pass"})
}

func (s *ConfigSuite) TestInitNatsWithNatsUri(c *C) {
	// init with nats uri but without username:password
	s.config.Nats = NatsConfig{"nats://localhost:4222", "", "", ""}
	InitConfig(s.config)

	c.Assert(config.Nats, Equals, NatsConfig{"nats://localhost:4222", "localhost:4222", "", ""})
}

func (s *ConfigSuite) TestInitNatsWithNatsHost(c *C) {
	// init with nats host but without username:password
	s.config.Nats = NatsConfig{"", "localhost:4222", "natsuser", "natspass"}
	InitConfig(s.config)

	c.Assert(config.Nats, Equals, NatsConfig{"", "localhost:4222", "natsuser", "natspass"})
}

func (s *ConfigSuite) TestInitNatsWithAuth(c *C) {
	InitConfig(s.config)

	c.Assert(config.Nats, Equals, NatsConfig{"nats://natsuser:natspass@localhost:4222", "localhost:4222", "natsuser", "natspass"})
}
