package config

import (
	"errors"
	"io/ioutil"

	"github.com/cloudfoundry-incubator/candiedyaml"
)

type MetronConfig struct {
	Address string
	Port    string
}

type Config struct {
	UAAPublicKey string       `yaml:"uaa_verification_key"`
	LogGuid      string       `yaml:"log_guid"`
	MetronConfig MetronConfig `yaml:"metron_config"`
}

func NewConfigFromFile(configFile string) (Config, error) {
	c, err := ioutil.ReadFile(configFile)
	if err != nil {
		return Config{}, err
	}

	// Init things
	config := Config{}
	config.Initialize(c)

	return config, nil
}

func (cfg *Config) Initialize(file []byte) error {
	err := candiedyaml.Unmarshal(file, &cfg)
	if err != nil {
		return err
	}

	if cfg.LogGuid == "" {
		return errors.New("No log_guid specified")
	}

	if cfg.UAAPublicKey == "" {
		return errors.New("No uaa_verification_key specified")
	}

	return nil
}
