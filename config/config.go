package config

import (
	"crypto/tls"
	"fmt"
	"net/url"

	"github.com/cloudfoundry-incubator/candiedyaml"
	steno "github.com/cloudfoundry/gosteno"
	"github.com/pivotal-golang/localip"

	"io/ioutil"
	"runtime"
	"strconv"
	"strings"
	"time"
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

type OAuthConfig struct {
	TokenEndpoint string `yaml:"token_endpoint"`
	ClientName    string `yaml:"client_name"`
	ClientSecret  string `yaml:"client_secret"`
	Port          int    `yaml:"port"`
}

type RoutingApiConfig struct {
	Uri  string `yaml:"uri"`
	Port int    `yaml:"port"`
}

var defaultNatsConfig = NatsConfig{
	Host: "localhost",
	Port: 4222,
	User: "",
	Pass: "",
}

type LoggingConfig struct {
	File               string `yaml:"file"`
	Syslog             string `yaml:"syslog"`
	Level              string `yaml:"level"`
	LoggregatorEnabled bool   `yaml:"loggregator_enabled"`
	MetronAddress      string `yaml:"metron_address"`

	// This field is populated by the `Process` function.
	JobName string `yaml:"-"`
}

var defaultLoggingConfig = LoggingConfig{
	Level:         "debug",
	MetronAddress: "localhost:3457",
}

type Config struct {
	Status  StatusConfig  `yaml:"status"`
	Nats    []NatsConfig  `yaml:"nats"`
	Logging LoggingConfig `yaml:"logging"`

	Port           uint16 `yaml:"port"`
	Index          uint   `yaml:"index"`
	Zone           string `yaml:"zone"`
	GoMaxProcs     int    `yaml:"go_max_procs,omitempty"`
	TraceKey       string `yaml:"trace_key"`
	AccessLog      string `yaml:"access_log"`
	DebugAddr      string `yaml:"debug_addr"`
	EnableSSL      bool   `yaml:"enable_ssl"`
	SSLPort        uint16 `yaml:"ssl_port"`
	SSLCertPath    string `yaml:"ssl_cert_path"`
	SSLKeyPath     string `yaml:"ssl_key_path"`
	SSLCertificate tls.Certificate

	CipherString string `yaml:"cipher_suites"`
	CipherSuites []uint16

	PublishStartMessageIntervalInSeconds int  `yaml:"publish_start_message_interval"`
	PruneStaleDropletsIntervalInSeconds  int  `yaml:"prune_stale_droplets_interval"`
	DropletStaleThresholdInSeconds       int  `yaml:"droplet_stale_threshold"`
	PublishActiveAppsIntervalInSeconds   int  `yaml:"publish_active_apps_interval"`
	StartResponseDelayIntervalInSeconds  int  `yaml:"start_response_delay_interval"`
	EndpointTimeoutInSeconds             int  `yaml:"endpoint_timeout"`
	DrainTimeoutInSeconds                int  `yaml:"drain_timeout,omitempty"`
	SecureCookies                        bool `yaml:"secure_cookies"`

	OAuth      OAuthConfig      `yaml:"oauth"`
	RoutingApi RoutingApiConfig `yaml:"routing_api"`

	// These fields are populated by the `Process` function.
	PruneStaleDropletsInterval time.Duration `yaml:"-"`
	DropletStaleThreshold      time.Duration `yaml:"-"`
	PublishActiveAppsInterval  time.Duration `yaml:"-"`
	StartResponseDelayInterval time.Duration `yaml:"-"`
	EndpointTimeout            time.Duration `yaml:"-"`
	DrainTimeout               time.Duration `yaml:"-"`
	Ip                         string        `yaml:"-"`
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

	EndpointTimeoutInSeconds: 60,

	PublishStartMessageIntervalInSeconds: 30,
	PruneStaleDropletsIntervalInSeconds:  30,
	DropletStaleThresholdInSeconds:       120,
	PublishActiveAppsIntervalInSeconds:   0,
	StartResponseDelayIntervalInSeconds:  5,
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
	c.Logging.JobName = "router_" + c.Zone + "_" + strconv.Itoa(int(c.Index))

	if c.StartResponseDelayInterval > c.DropletStaleThreshold {
		c.DropletStaleThreshold = c.StartResponseDelayInterval
		log := steno.NewLogger("config.logger")
		log.Warnf("DropletStaleThreshold (%s) cannot be less than StartResponseDelayInterval (%s); setting both equal to StartResponseDelayInterval and continuing", c.DropletStaleThreshold, c.StartResponseDelayInterval)
	}

	drain := c.DrainTimeoutInSeconds
	if drain == 0 {
		drain = c.EndpointTimeoutInSeconds
	}
	c.DrainTimeout = time.Duration(drain) * time.Second

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
}

func (c *Config) processCipherSuites() []uint16 {
	cipherMap := map[string]uint16{
		"TLS_RSA_WITH_RC4_128_SHA":                0x0005,
		"TLS_RSA_WITH_AES_128_CBC_SHA":            0x002f,
		"TLS_RSA_WITH_AES_256_CBC_SHA":            0x0035,
		"TLS_ECDHE_ECDSA_WITH_RC4_128_SHA":        0xc007,
		"TLS_ECDHE_ECDSA_WITH_AES_128_CBC_SHA":    0xc009,
		"TLS_ECDHE_ECDSA_WITH_AES_256_CBC_SHA":    0xc00a,
		"TLS_ECDHE_RSA_WITH_RC4_128_SHA":          0xc011,
		"TLS_ECDHE_RSA_WITH_AES_128_CBC_SHA":      0xc013,
		"TLS_ECDHE_RSA_WITH_AES_256_CBC_SHA":      0xc014,
		"TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256":   0xc02f,
		"TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256": 0xc02b,
	}

	ciphers := []uint16{}
	if len(strings.TrimSpace(c.CipherString)) == 0 {
		for _, cipherValue := range cipherMap {
			ciphers = append(ciphers, cipherValue)
		}
		return ciphers
	}

	for _, cipher := range strings.Split(c.CipherString, ":") {
		if val, ok := cipherMap[cipher]; ok {
			ciphers = append(ciphers, val)
		} else {
			panic("invalid cipher string configuration")
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
