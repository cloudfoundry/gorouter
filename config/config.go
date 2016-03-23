package config

import (
	"crypto/tls"
	"fmt"
	"net/url"

	"io/ioutil"
	"runtime"
	"strings"
	"time"

	"github.com/cloudfoundry-incubator/candiedyaml"
	"github.com/pivotal-golang/localip"
)

type StatusConfig struct {
	Port uint16 `yaml:"port"`
	User string `yaml:"user"`
	Pass string `yaml:"pass"`
}

var defaultStatusConfig = StatusConfig{
	Port: 8082,
	User: "",
	Pass: "",
}

type NatsConfig struct {
	Host string `yaml:"host"`
	Port uint16 `yaml:"port"`
	User string `yaml:"user"`
	Pass string `yaml:"pass"`
}

type RoutingApiConfig struct {
	Uri          string `yaml:"uri"`
	Port         int    `yaml:"port"`
	AuthDisabled bool   `yaml:"auth_disabled"`
}

var defaultNatsConfig = NatsConfig{
	Host: "localhost",
	Port: 4222,
	User: "",
	Pass: "",
}

type OAuthConfig struct {
	TokenEndpoint            string `yaml:"token_endpoint"`
	Port                     int    `yaml:"port"`
	SkipOAuthTLSVerification bool   `yaml:"skip_oauth_tls_verification"`
	ClientName               string `yaml:"client_name"`
	ClientSecret             string `yaml:"client_secret"`
}

type LoggingConfig struct {
	File                 string `yaml:"file"`
	Syslog               string `yaml:"syslog"`
	Level                string `yaml:"level"`
	LoggregatorEnabled   bool   `yaml:"loggregator_enabled"`
	AccessLoggingEnabled bool   `yaml:"access_logging_enabled"`
	MetronAddress        string `yaml:"metron_address"`

	// This field is populated by the `Process` function.
	JobName string `yaml:"-"`
}

var defaultLoggingConfig = LoggingConfig{
	Level:                "debug",
	MetronAddress:        "localhost:3457",
	AccessLoggingEnabled: true,
}

type Config struct {
	Status            StatusConfig  `yaml:"status"`
	Nats              []NatsConfig  `yaml:"nats"`
	Logging           LoggingConfig `yaml:"logging"`
	Port              uint16        `yaml:"port"`
	Index             uint          `yaml:"index"`
	Zone              string        `yaml:"zone"`
	GoMaxProcs        int           `yaml:"go_max_procs,omitempty"`
	TraceKey          string        `yaml:"trace_key"`
	AccessLog         string        `yaml:"access_log"`
	DebugAddr         string        `yaml:"debug_addr"`
	EnableSSL         bool          `yaml:"enable_ssl"`
	SSLPort           uint16        `yaml:"ssl_port"`
	SSLCertPath       string        `yaml:"ssl_cert_path"`
	SSLKeyPath        string        `yaml:"ssl_key_path"`
	SSLCertificate    tls.Certificate
	SSLSkipValidation bool `yaml:"ssl_skip_validation"`

	CipherString string `yaml:"cipher_suites"`
	CipherSuites []uint16

	PublishStartMessageIntervalInSeconds int `yaml:"publish_start_message_interval"`
	PruneStaleDropletsIntervalInSeconds  int `yaml:"prune_stale_droplets_interval"`
	DropletStaleThresholdInSeconds       int `yaml:"droplet_stale_threshold"`
	PublishActiveAppsIntervalInSeconds   int `yaml:"publish_active_apps_interval"`
	StartResponseDelayIntervalInSeconds  int `yaml:"start_response_delay_interval"`
	EndpointTimeoutInSeconds             int `yaml:"endpoint_timeout"`
	RouteServiceTimeoutInSeconds         int `yaml:"route_services_timeout"`

	DrainWaitInSeconds    int  `yaml:"drain_wait,omitempty"`
	DrainTimeoutInSeconds int  `yaml:"drain_timeout,omitempty"`
	SecureCookies         bool `yaml:"secure_cookies"`

	OAuth                      OAuthConfig      `yaml:"oauth"`
	RoutingApi                 RoutingApiConfig `yaml:"routing_api"`
	RouteServiceSecret         string           `yaml:"route_services_secret"`
	RouteServiceSecretPrev     string           `yaml:"route_services_secret_decrypt_only"`
	RouteServiceRecommendHttps bool             `yaml:"route_services_recommend_https"`
	// These fields are populated by the `Process` function.
	PruneStaleDropletsInterval time.Duration `yaml:"-"`
	DropletStaleThreshold      time.Duration `yaml:"-"`
	PublishActiveAppsInterval  time.Duration `yaml:"-"`
	StartResponseDelayInterval time.Duration `yaml:"-"`
	EndpointTimeout            time.Duration `yaml:"-"`
	RouteServiceTimeout        time.Duration `yaml:"-"`
	DrainWait                  time.Duration `yaml:"-"`
	DrainTimeout               time.Duration `yaml:"-"`
	Ip                         string        `yaml:"-"`
	RouteServiceEnabled        bool          `yaml:"-"`
	TokenFetcherRetryInterval  time.Duration `yaml:"-"`

	ExtraHeadersToLog []string `yaml:"extra_headers_to_log"`

	TokenFetcherMaxRetries                    uint32 `yaml:"token_fetcher_max_retries"`
	TokenFetcherRetryIntervalInSeconds        int    `yaml:"token_fetcher_retry_interval"`
	TokenFetcherExpirationBufferTimeInSeconds int64  `yaml:"token_fetcher_expiration_buffer_time"`

	PidFile string `yaml:"pid_file"`
}

var defaultConfig = Config{
	Status:  defaultStatusConfig,
	Nats:    []NatsConfig{defaultNatsConfig},
	Logging: defaultLoggingConfig,

	Port:       8081,
	Index:      0,
	GoMaxProcs: -1,
	EnableSSL:  false,
	SSLPort:    443,

	EndpointTimeoutInSeconds:     60,
	RouteServiceTimeoutInSeconds: 60,

	PublishStartMessageIntervalInSeconds:      30,
	PruneStaleDropletsIntervalInSeconds:       30,
	DropletStaleThresholdInSeconds:            120,
	PublishActiveAppsIntervalInSeconds:        0,
	StartResponseDelayIntervalInSeconds:       5,
	TokenFetcherMaxRetries:                    3,
	TokenFetcherRetryIntervalInSeconds:        5,
	TokenFetcherExpirationBufferTimeInSeconds: 30,
}

func DefaultConfig() *Config {
	c := defaultConfig
	c.Process()

	return &c
}

func (c *Config) Process() {
	var err error

	if c.GoMaxProcs == -1 {
		c.GoMaxProcs = runtime.NumCPU()
	}

	c.PruneStaleDropletsInterval = time.Duration(c.PruneStaleDropletsIntervalInSeconds) * time.Second
	c.DropletStaleThreshold = time.Duration(c.DropletStaleThresholdInSeconds) * time.Second
	c.PublishActiveAppsInterval = time.Duration(c.PublishActiveAppsIntervalInSeconds) * time.Second
	c.StartResponseDelayInterval = time.Duration(c.StartResponseDelayIntervalInSeconds) * time.Second
	c.EndpointTimeout = time.Duration(c.EndpointTimeoutInSeconds) * time.Second
	c.RouteServiceTimeout = time.Duration(c.RouteServiceTimeoutInSeconds) * time.Second
	c.TokenFetcherRetryInterval = time.Duration(c.TokenFetcherRetryIntervalInSeconds) * time.Second
	c.Logging.JobName = "gorouter"
	if c.StartResponseDelayInterval > c.DropletStaleThreshold {
		c.DropletStaleThreshold = c.StartResponseDelayInterval
	}

	c.DrainTimeout = c.EndpointTimeout
	if c.DrainTimeoutInSeconds > 0 {
		c.DrainTimeout = time.Duration(c.DrainTimeoutInSeconds) * time.Second
	}

	if c.DrainWaitInSeconds > 0 {
		c.DrainWait = time.Duration(c.DrainWaitInSeconds) * time.Second
	}

	c.Ip, err = localip.LocalIP()
	if err != nil {
		panic(err)
	}

	if c.EnableSSL {
		c.CipherSuites = c.processCipherSuites()
		cert, err := tls.LoadX509KeyPair(c.SSLCertPath, c.SSLKeyPath)
		if err != nil {
			panic(err)
		}
		c.SSLCertificate = cert
	}

	if c.RouteServiceSecret != "" {
		c.RouteServiceEnabled = true
	}
}

func (c *Config) processCipherSuites() []uint16 {
	cipherMap := map[string]uint16{
		"TLS_RSA_WITH_AES_128_CBC_SHA":            0x002f,
		"TLS_RSA_WITH_AES_256_CBC_SHA":            0x0035,
		"TLS_ECDHE_ECDSA_WITH_AES_128_CBC_SHA":    0xc009,
		"TLS_ECDHE_ECDSA_WITH_AES_256_CBC_SHA":    0xc00a,
		"TLS_ECDHE_RSA_WITH_AES_128_CBC_SHA":      0xc013,
		"TLS_ECDHE_RSA_WITH_AES_256_CBC_SHA":      0xc014,
		"TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256":   0xc02f,
		"TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256": 0xc02b,
	}

	var ciphers []string

	if len(strings.TrimSpace(c.CipherString)) == 0 {
		panic("must specify list of cipher suite when ssl is enabled")
	} else {
		ciphers = strings.Split(c.CipherString, ":")
	}

	return convertCipherStringToInt(ciphers, cipherMap)
}

func convertCipherStringToInt(cipherStrs []string, cipherMap map[string]uint16) []uint16 {
	ciphers := []uint16{}
	for _, cipher := range cipherStrs {
		if val, ok := cipherMap[cipher]; ok {
			ciphers = append(ciphers, val)
		} else {
			var supportedCipherSuites = []string{}
			for key, _ := range cipherMap {
				supportedCipherSuites = append(supportedCipherSuites, key)
			}
			errMsg := fmt.Sprintf("invalid cipher string configuration: %s, please choose from %v", cipher, supportedCipherSuites)
			panic(errMsg)
		}
	}

	return ciphers
}

func (c *Config) NatsServers() []string {
	var natsServers []string
	for _, info := range c.Nats {
		uri := url.URL{
			Scheme: "nats",
			User:   url.UserPassword(info.User, info.Pass),
			Host:   fmt.Sprintf("%s:%d", info.Host, info.Port),
		}
		natsServers = append(natsServers, uri.String())
	}

	return natsServers
}

func (c *Config) RoutingApiEnabled() bool {
	return (c.RoutingApi.Uri != "") && (c.RoutingApi.Port != 0)
}

func (c *Config) Initialize(configYAML []byte) error {
	c.Nats = []NatsConfig{}
	return candiedyaml.Unmarshal(configYAML, &c)
}

func InitConfigFromFile(path string) *Config {
	var c *Config = DefaultConfig()
	var e error

	b, e := ioutil.ReadFile(path)
	if e != nil {
		panic(e.Error())
	}

	e = c.Initialize(b)
	if e != nil {
		panic(e.Error())
	}

	c.Process()

	return c
}
