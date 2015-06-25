package config

import (
	"errors"
	"io/ioutil"
	"time"

	"github.com/cloudfoundry-incubator/candiedyaml"
)

type MetronConfig struct {
	Address string
	Port    string
}

type Config struct {
	UAAPublicKey                    string        `yaml:"uaa_verification_key"`
	DebugAddress                    string        `yaml:"debug_address"`
	LogGuid                         string        `yaml:"log_guid"`
	MetronConfig                    MetronConfig  `yaml:"metron_config"`
	MetricsReportingIntervalString  string        `yaml:"metrics_reporting_interval"`
	MetricsReportingInterval        time.Duration `yaml:"-"`
	StatsdEndpoint                  string        `yaml:"statsd_endpoint"`
	StatsdClientFlushIntervalString string        `yaml:"statsd_client_flush_interval"`
	StatsdClientFlushInterval       time.Duration `yaml:"-"`
	MaxConcurrentETCDRequests       uint          `yaml:"max_concurrent_etcd_requests"`
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

	err = cfg.process()

	if err != nil {
		return err
	}

	return nil
}

func (cfg *Config) process() error {
	metricsReportingInterval, err := time.ParseDuration(cfg.MetricsReportingIntervalString)
	if err != nil {
		return err
	}
	cfg.MetricsReportingInterval = metricsReportingInterval

	statsdClientFlushInterval, err := time.ParseDuration(cfg.StatsdClientFlushIntervalString)
	if err != nil {
		return err
	}
	cfg.StatsdClientFlushInterval = statsdClientFlushInterval

	return nil
}
