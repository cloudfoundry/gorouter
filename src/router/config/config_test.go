package config

import (
	. "launchpad.net/gocheck"
	"launchpad.net/goyaml"
	"time"
)

type ConfigSuite struct {
	*Config
}

var _ = Suite(&ConfigSuite{})

func (s *ConfigSuite) SetUpTest(c *C) {
	s.Config = DefaultConfig()
}

func (s *ConfigSuite) TestStatus(c *C) {
	var b = []byte(`
status:
  port: 1234
  user: user
  pass: pass
`)

	c.Check(s.Status.Port, Equals, uint16(8082))
	c.Check(s.Status.User, Equals, "")
	c.Check(s.Status.Pass, Equals, "")

	goyaml.Unmarshal(b, &s.Config)

	c.Check(s.Status.Port, Equals, uint16(1234))
	c.Check(s.Status.User, Equals, "user")
	c.Check(s.Status.Pass, Equals, "pass")
}

func (s *ConfigSuite) TestNatsWithoutUri(c *C) {
	var b = []byte(`
nats:
  host: remotehost:4223
  user: user
  pass: pass
`)

	c.Check(s.Nats.Host, Equals, "localhost:4222")
	c.Check(s.Nats.User, Equals, "")
	c.Check(s.Nats.Pass, Equals, "")

	goyaml.Unmarshal(b, &s.Config)

	c.Check(s.Nats.Host, Equals, "remotehost:4223")
	c.Check(s.Nats.User, Equals, "user")
	c.Check(s.Nats.Pass, Equals, "pass")
}

func (s *ConfigSuite) TestNatsWithUri(c *C) {
	var b = []byte(`
nats:
  uri: nats://user:pass@remotehost:4223/
`)

	c.Check(s.Nats.Host, Equals, "localhost:4222")
	c.Check(s.Nats.User, Equals, "")
	c.Check(s.Nats.Pass, Equals, "")

	goyaml.Unmarshal(b, &s.Config)

	s.Config.Process()

	c.Check(s.Nats.Host, Equals, "remotehost:4223")
	c.Check(s.Nats.User, Equals, "user")
	c.Check(s.Nats.Pass, Equals, "pass")
}

func (s *ConfigSuite) TestLogging(c *C) {
	var b = []byte(`
logging:
  file: /tmp/file
  syslog: syslog
  level: debug2
`)

	c.Check(s.Logging.File, Equals, "")
	c.Check(s.Logging.Syslog, Equals, "")
	c.Check(s.Logging.Level, Equals, "debug")

	goyaml.Unmarshal(b, &s.Config)

	c.Check(s.Logging.File, Equals, "/tmp/file")
	c.Check(s.Logging.Syslog, Equals, "syslog")
	c.Check(s.Logging.Level, Equals, "debug2")
}

func (s *ConfigSuite) TestConfig(c *C) {
	var b = []byte(`
port: 8082
index: 1
pidfile: /tmp/pidfile
go_max_procs: 2
trace_key: "foo"

info_host_port: "127.0.0.1:23457"

publish_start_message_interval: 1
prune_stale_droplets_interval: 2
droplet_stale_threshold: 3
publish_active_apps_interval: 4
`)

	c.Check(s.Port, Equals, uint16(8081))
	c.Check(s.Index, Equals, uint(0))
	c.Check(s.Pidfile, Equals, "")
	c.Check(s.GoMaxProcs, Equals, 8)
	c.Check(s.TraceKey, Equals, "")

	c.Check(s.InfoHostPort, Equals, ":23456")

	c.Check(s.PublishStartMessageInterval, Equals, 30*time.Second)
	c.Check(s.PruneStaleDropletsInterval, Equals, 30*time.Second)
	c.Check(s.DropletStaleThreshold, Equals, 120*time.Second)
	c.Check(s.PublishActiveAppsInterval, Equals, 0*time.Second)

	goyaml.Unmarshal(b, &s.Config)

	s.Config.Process()

	c.Check(s.Port, Equals, uint16(8082))
	c.Check(s.Index, Equals, uint(1))
	c.Check(s.Pidfile, Equals, "/tmp/pidfile")
	c.Check(s.GoMaxProcs, Equals, 2)
	c.Check(s.TraceKey, Equals, "foo")

	c.Check(s.InfoHostPort, Equals, "127.0.0.1:23457")

	c.Check(s.PublishStartMessageInterval, Equals, 1*time.Second)
	c.Check(s.PruneStaleDropletsInterval, Equals, 2*time.Second)
	c.Check(s.DropletStaleThreshold, Equals, 3*time.Second)
	c.Check(s.PublishActiveAppsInterval, Equals, 4*time.Second)
}
