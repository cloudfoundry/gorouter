package router

import (
	"encoding/json"
	"net/url"
	"os"
)

type Config struct {
	Port       int
	StatusPort int
	Nats       NatsConfig
}

type NatsConfig struct {
	URI  string
	Host string
	User string
	Pass string
}

func GenerateConfig(configFile string) *Config {
	file, err := os.OpenFile(configFile, os.O_RDONLY, 0)
	if err != nil {
		panic(err)
	}

	var config *Config = new(Config)

	decoder := json.NewDecoder(file)
	err = decoder.Decode(config)
	if err != nil {
		panic(err)
	}

	postProcess(config)

	return config
}

func postProcess(config *Config) {
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
}
