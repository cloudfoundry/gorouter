package router

import (
	"io/ioutil"
	"launchpad.net/goyaml"
	"net"
	"net/url"
)

type Config struct {
	Port       int
	SessionKey string
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
		config.Nats.User = u.User.Username()
		config.Nats.Pass, _ = u.User.Password()
	}

	if config.Nats.Host == "" {
		panic("nats server not configured.")
	}

	config.ip, _ = localIP()

	if config.SessionKey == "" {
		config.SessionKey = "14fbc303b76bacd1e0a3ab641c11d114"
	}

	return config
}

func localIP() (string, error) {
	addr, err := net.ResolveUDPAddr("udp", "1.2.3.4:1")
	if err != nil {
		return "", err
	}

	conn, err := net.DialUDP("udp", nil, addr)
	if err != nil {
		return "", err
	}

	host, _, err := net.SplitHostPort(conn.LocalAddr().String())
	if err != nil {
		return "", err
	}

	return host, nil
}
