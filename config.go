package router

import (
	"io/ioutil"
	"launchpad.net/goyaml"
	"net/url"
	vcap "router/common"
)

type Config struct {
	Port       int
	SessionKey string
	Index      uint
	Status     StatusConfig
	Nats       NatsConfig

	ip string
}

type StatusConfig struct {
	Port     int
	User     string
	Password string
}

type NatsConfig struct {
	URI  string
	Host string
	User string
	Pass string
}

var config Config

func InitConfigFromFile(configFile string) {
	configBytes, err := ioutil.ReadFile(configFile)
	if err != nil {
		panic(err)
	}

	err = goyaml.Unmarshal(configBytes, &config)
	if err != nil {
		panic(err)
	}

	SanitizeConfig(&config)
}

func InitConfig(c *Config) {
	SanitizeConfig(c)

	config = *c
}

func SanitizeConfig(config *Config) *Config {
	if config.Nats.URI != "" {
		u, err := url.Parse(config.Nats.URI)
		if err != nil {
			panic(err)
		}

		config.Nats.Host = u.Host
		if u.User != nil {
			config.Nats.User = u.User.Username()
			config.Nats.Pass, _ = u.User.Password()
		}
	}

	if config.Nats.Host == "" {
		panic("nats server not configured.")
	}

	config.ip, _ = vcap.LocalIP()

	if config.SessionKey == "" {
		config.SessionKey = "14fbc303b76bacd1e0a3ab641c11d114"
	}

	return config
}
