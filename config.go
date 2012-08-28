package router

import (
	"encoding/json"
	"net"
	"net/url"
	"os"
)

type Config struct {
	Port       int
	StatusPort int
	Nats       NatsConfig

	ip string
}

type NatsConfig struct {
	URI  string
	Host string
	User string
	Pass string
}

var config Config

func GenerateConfig(configFile string) {
	file, err := os.OpenFile(configFile, os.O_RDONLY, 0)
	if err != nil {
		panic(err)
	}

	decoder := json.NewDecoder(file)
	err = decoder.Decode(&config)
	if err != nil {
		panic(err)
	}

	postProcess(&config)
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

	config.ip, _ = localIP()
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
