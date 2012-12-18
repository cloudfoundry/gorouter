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
		Port:   10000,
		Status: StatusConfig{10001, "user", "pass"},
		Nats:   NatsConfig{"nats://natsuser:natspass@localhost:4222", "", "", ""},
	}

	c.Assert(config, Equals, Config{})
}

func (s *ConfigSuite) TearDownTest(c *C) {
	s.config = nil
	// since global variable 'config' will be modified by InitConfig,it should be
	// reset after every case finished in order to make sure all cases run independently
	config = Config{}
}

func (s *ConfigSuite) TestInitFromFile(c *C) {
	f := "config/router.yml"
	InitConfigFromFile(f)

	c.Assert(config.Port, Equals, uint16(8083))
	c.Assert(config.FlushAppsInterval, Equals, 30)
	c.Assert(config.GoMaxProcs, Equals, 8)
	c.Assert(config.ProxyWarmupTime, Equals, 5)
}

func (s *ConfigSuite) TestSanitizeConfig(c *C) {
	c.Assert(s.config.ip, Equals, "")
	c.Assert(s.config.Nats.Host, Equals, "")
	c.Assert(s.config.Nats.User, Equals, "")
	c.Assert(s.config.Nats.Pass, Equals, "")

	sanitizeConfig(s.config)

	ip, err := vcap.LocalIP()
	c.Assert(err, IsNil)
	c.Assert(s.config.ip, Equals, ip)
	c.Assert(s.config.Nats.Host, Equals, "localhost:4222")
	c.Assert(s.config.Nats.User, Equals, "natsuser")
	c.Assert(s.config.Nats.Pass, Equals, "natspass")
}

func (s *ConfigSuite) TestInitConfig(c *C) {
	InitConfig(s.config)

	c.Assert(config.Port, Equals, uint16(10000))
}

func (s *ConfigSuite) TestInitStatusServiceConfig(c *C) {
	InitConfig(s.config)

	c.Assert(config.Status.Port, Equals, uint16(10001))
	c.Assert(config.Status.User, Equals, s.config.Status.User)
	c.Assert(config.Status.Password, Equals, s.config.Status.Password)
}

func (s *ConfigSuite) TestInitNatsWithAuth(c *C) {
	InitConfig(s.config)

	c.Assert(config.Nats.Host, Equals, "localhost:4222")
	c.Assert(config.Nats.User, Equals, "natsuser")
	c.Assert(config.Nats.Pass, Equals, "natspass")
}

func (s *ConfigSuite) TestInitNatsWithNatsUri(c *C) {
	// init with nats uri but without username:password
	s.config.Nats = NatsConfig{"nats://localhost:4222", "", "", ""}
	InitConfig(s.config)

	c.Assert(config.Nats.Host, Equals, "localhost:4222")
	c.Assert(config.Nats.User, Equals, "")
	c.Assert(config.Nats.Pass, Equals, "")
}

func (s *ConfigSuite) TestInitNatsWithNatsHost(c *C) {
	// init with nats host but without username:password
	s.config.Nats = NatsConfig{"", "localhost:4222", "natsuser", "natspass"}
	InitConfig(s.config)

	c.Assert(config.Nats.URI, Equals, "")
	c.Assert(config.Nats.Host, Equals, "localhost:4222")
	c.Assert(config.Nats.User, Equals, "natsuser")
	c.Assert(config.Nats.Pass, Equals, "natspass")
}
