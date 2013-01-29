package config

import (
	"io/ioutil"
	"launchpad.net/goyaml"
	"net/url"
	vcap "router/common"
)

type StatusConfig struct {
	Port uint16 "port"
	User string "user"
	Pass string "pass"
}

var defaultStatusConfig = StatusConfig{
	Port: 8082,
	User: "",
	Pass: "",
}

type NatsConfig struct {
	Uri  string "uri"
	Host string "host"
	User string "user"
	Pass string "pass"
}

var defaultNatsConfig = NatsConfig{
	Uri:  "",
	Host: "localhost:4222",
	User: "",
	Pass: "",
}

type LoggingConfig struct {
	File   string "file"
	Syslog string "syslog"
	Level  string "level"
}

var defaultLoggingConfig = LoggingConfig{
	Level: "debug",
}

type Config struct {
	Status  StatusConfig  "status"
	Nats    NatsConfig    "nats"
	Logging LoggingConfig "logging"

	Port  uint16 "port"
	Index uint   "index"

	FlushAppsInterval int "flush_apps_interval,omitempty"
	GoMaxProcs        int "go_max_procs,omitempty"
	ProxyWarmupTime   int "proxy_warmup_time,omitempty"

	Ip string
}

var defaultConfig = Config{
	Status:  defaultStatusConfig,
	Nats:    defaultNatsConfig,
	Logging: defaultLoggingConfig,

	Port:  8081,
	Index: 0,

	FlushAppsInterval: 0, // Disabled
	GoMaxProcs:        8,
	ProxyWarmupTime:   15,
}

func DefaultConfig() *Config {
	c := defaultConfig

	c.Process()

	return &c
}

func (c *Config) Process() {
	var err error

	if c.Nats.Uri != "" {
		u, err := url.Parse(c.Nats.Uri)
		if err != nil {
			panic(err)
		}

		c.Nats.Host = u.Host
		if u.User != nil {
			c.Nats.User = u.User.Username()
			c.Nats.Pass, _ = u.User.Password()
		}
	}

	c.Ip, err = vcap.LocalIP()
	if err != nil {
		panic(err)
	}
}

func InitConfigFromFile(path string) *Config {
	var c *Config = DefaultConfig()
	var e error

	b, e := ioutil.ReadFile(path)
	if e != nil {
		panic(e.Error())
	}

	e = goyaml.Unmarshal(b, c)
	if e != nil {
		panic(e.Error())
	}

	c.Process()

	return c
}
