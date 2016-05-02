package config

import (
	"errors"
	"io/ioutil"
	"time"

	"github.com/cloudfoundry-incubator/candiedyaml"
	"github.com/cloudfoundry-incubator/routing-api/models"
)

type MetronConfig struct {
	Address string
	Port    string
}

type OAuthConfig struct {
	TokenEndpoint            string `yaml:"token_endpoint"`
	Port                     int    `yaml:"port"`
	SkipOAuthTLSVerification bool   `yaml:"skip_oauth_tls_verification"`
	ClientName               string `yaml:"client_name"`
	ClientSecret             string `yaml:"client_secret"`
}

type Config struct {
	DebugAddress                    string              `yaml:"debug_address"`
	LogGuid                         string              `yaml:"log_guid"`
	MetronConfig                    MetronConfig        `yaml:"metron_config"`
	MetricsReportingIntervalString  string              `yaml:"metrics_reporting_interval"`
	MetricsReportingInterval        time.Duration       `yaml:"-"`
	StatsdEndpoint                  string              `yaml:"statsd_endpoint"`
	StatsdClientFlushIntervalString string              `yaml:"statsd_client_flush_interval"`
	StatsdClientFlushInterval       time.Duration       `yaml:"-"`
	OAuth                           OAuthConfig         `yaml:"oauth"`
	RouterGroups                    models.RouterGroups `yaml:"router_groups"`
}

func NewConfigFromFile(configFile string, authDisabled bool) (Config, error) {
	c, err := ioutil.ReadFile(configFile)
	if err != nil {
		return Config{}, err
	}

	// Init things
	config := Config{}
	if err = config.Initialize(c, authDisabled); err != nil {
		return config, err
	}

	return config, nil
}

func (cfg *Config) Initialize(file []byte, authDisabled bool) error {
	err := candiedyaml.Unmarshal(file, &cfg)
	if err != nil {
		return err
	}

	if cfg.LogGuid == "" {
		return errors.New("No log_guid specified")
	}

	if !authDisabled && cfg.OAuth.TokenEndpoint == "" {
		return errors.New("No token endpoint specified")
	}

	if !authDisabled && cfg.OAuth.TokenEndpoint != "" && cfg.OAuth.Port == -1 {
		return errors.New("Routing API requires TLS enabled to get OAuth token")
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

	if err := cfg.RouterGroups.Validate(); err != nil {
		return err
	}

	return nil
}
