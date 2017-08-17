package config

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net/url"

	"io/ioutil"
	"runtime"
	"strings"
	"time"

	"code.cloudfoundry.org/localip"
	"gopkg.in/yaml.v2"
)

const (
	LOAD_BALANCE_RR           string = "round-robin"
	LOAD_BALANCE_LC           string = "least-connection"
	SHARD_ALL                 string = "all"
	SHARD_SEGMENTS            string = "segments"
	SHARD_SHARED_AND_SEGMENTS string = "shared-and-segments"
	ALWAYS_FORWARD            string = "always_forward"
	SANITIZE_SET              string = "sanitize_set"
	FORWARD                   string = "forward"
)

var LoadBalancingStrategies = []string{LOAD_BALANCE_RR, LOAD_BALANCE_LC}
var AllowedShardingModes = []string{SHARD_ALL, SHARD_SEGMENTS, SHARD_SHARED_AND_SEGMENTS}
var AllowedForwardedClientCertModes = []string{ALWAYS_FORWARD, FORWARD, SANITIZE_SET}

type StatusConfig struct {
	Host string `yaml:"host"`
	Port uint16 `yaml:"port"`
	User string `yaml:"user"`
	Pass string `yaml:"pass"`
}

var defaultStatusConfig = StatusConfig{
	Host: "0.0.0.0",
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
	TokenEndpoint     string `yaml:"token_endpoint"`
	Port              int    `yaml:"port"`
	SkipSSLValidation bool   `yaml:"skip_ssl_validation"`
	ClientName        string `yaml:"client_name"`
	ClientSecret      string `yaml:"client_secret"`
	CACerts           string `yaml:"ca_certs"`
}

type BackendConfig struct {
	MaxConns int64 `yaml:"max_conns"`
}

type LoggingConfig struct {
	Syslog             string `yaml:"syslog"`
	Level              string `yaml:"level"`
	LoggregatorEnabled bool   `yaml:"loggregator_enabled"`
	MetronAddress      string `yaml:"metron_address"`

	// This field is populated by the `Process` function.
	JobName string `yaml:"-"`
}

type AccessLog struct {
	File            string `yaml:"file"`
	EnableStreaming bool   `yaml:"enable_streaming"`
}

type Tracing struct {
	EnableZipkin bool `yaml:"enable_zipkin"`
}

type TLSPem struct {
	CertChain  string `yaml:"cert_chain"`
	PrivateKey string `yaml:"private_key"`
}

var defaultLoggingConfig = LoggingConfig{
	Level:         "debug",
	MetronAddress: "localhost:3457",
}

type Config struct {
	Status                   StatusConfig  `yaml:"status"`
	Nats                     []NatsConfig  `yaml:"nats"`
	Logging                  LoggingConfig `yaml:"logging"`
	Port                     uint16        `yaml:"port"`
	Index                    uint          `yaml:"index"`
	Zone                     string        `yaml:"zone"`
	GoMaxProcs               int           `yaml:"go_max_procs,omitempty"`
	Tracing                  Tracing       `yaml:"tracing"`
	TraceKey                 string        `yaml:"trace_key"`
	AccessLog                AccessLog     `yaml:"access_log"`
	EnableAccessLogStreaming bool          `yaml:"enable_access_log_streaming"`
	DebugAddr                string        `yaml:"debug_addr"`
	EnablePROXY              bool          `yaml:"enable_proxy"`
	EnableSSL                bool          `yaml:"enable_ssl"`
	SSLPort                  uint16        `yaml:"ssl_port"`
	DisableHTTP              bool          `yaml:"disable_http"`
	SSLCertificates          []tls.Certificate
	TLSPEM                   []TLSPem `yaml:"tls_pem"`
	CACerts                  []string `yaml:"ca_certs"`
	CAPool                   *x509.CertPool
	SkipSSLValidation        bool     `yaml:"skip_ssl_validation"`
	ForwardedClientCert      string   `yaml:"forwarded_client_cert"`
	ForceForwardedProtoHttps bool     `yaml:"force_forwarded_proto_https"`
	IsolationSegments        []string `yaml:"isolation_segments"`
	RoutingTableShardingMode string   `yaml:"routing_table_sharding_mode"`

	CipherString        string `yaml:"cipher_suites"`
	CipherSuites        []uint16
	MinTLSVersionString string `yaml:"min_tls_version"`
	MinTLSVersion       uint16

	LoadBalancerHealthyThreshold    time.Duration `yaml:"load_balancer_healthy_threshold"`
	PublishStartMessageInterval     time.Duration `yaml:"publish_start_message_interval"`
	SuspendPruningIfNatsUnavailable bool          `yaml:"suspend_pruning_if_nats_unavailable"`
	PruneStaleDropletsInterval      time.Duration `yaml:"prune_stale_droplets_interval"`
	DropletStaleThreshold           time.Duration `yaml:"droplet_stale_threshold"`
	PublishActiveAppsInterval       time.Duration `yaml:"publish_active_apps_interval"`
	StartResponseDelayInterval      time.Duration `yaml:"start_response_delay_interval"`
	EndpointTimeout                 time.Duration `yaml:"endpoint_timeout"`
	RouteServiceTimeout             time.Duration `yaml:"route_services_timeout"`

	DrainWait            time.Duration `yaml:"drain_wait,omitempty"`
	DrainTimeout         time.Duration `yaml:"drain_timeout,omitempty"`
	SecureCookies        bool          `yaml:"secure_cookies"`
	HealthCheckUserAgent string        `yaml:"healthcheck_user_agent,omitempty"`

	OAuth                      OAuthConfig      `yaml:"oauth"`
	RoutingApi                 RoutingApiConfig `yaml:"routing_api"`
	RouteServiceSecret         string           `yaml:"route_services_secret"`
	RouteServiceSecretPrev     string           `yaml:"route_services_secret_decrypt_only"`
	RouteServiceRecommendHttps bool             `yaml:"route_services_recommend_https"`
	// These fields are populated by the `Process` function.
	Ip                     string        `yaml:"-"`
	RouteServiceEnabled    bool          `yaml:"-"`
	NatsClientPingInterval time.Duration `yaml:"-"`
	Backends               BackendConfig `yaml:"backends"`
	ExtraHeadersToLog      []string      `yaml:"extra_headers_to_log"`

	TokenFetcherMaxRetries                    uint32        `yaml:"token_fetcher_max_retries"`
	TokenFetcherRetryInterval                 time.Duration `yaml:"token_fetcher_retry_interval"`
	TokenFetcherExpirationBufferTimeInSeconds int64         `yaml:"token_fetcher_expiration_buffer_time"`

	PidFile     string `yaml:"pid_file"`
	LoadBalance string `yaml:"balancing_algorithm"`

	DisableKeepAlives   bool `yaml:"disable_keep_alives"`
	MaxIdleConns        int  `yaml:"max_idle_conns"`
	MaxIdleConnsPerHost int  `yaml:"max_idle_conns_per_host"`
}

var defaultConfig = Config{
	Status:        defaultStatusConfig,
	Nats:          []NatsConfig{defaultNatsConfig},
	Logging:       defaultLoggingConfig,
	Port:          8081,
	Index:         0,
	GoMaxProcs:    -1,
	EnablePROXY:   false,
	EnableSSL:     false,
	SSLPort:       443,
	DisableHTTP:   false,
	MinTLSVersion: tls.VersionTLS12,

	EndpointTimeout:     60 * time.Second,
	RouteServiceTimeout: 60 * time.Second,

	PublishStartMessageInterval:               30 * time.Second,
	PruneStaleDropletsInterval:                30 * time.Second,
	DropletStaleThreshold:                     120 * time.Second,
	PublishActiveAppsInterval:                 0 * time.Second,
	StartResponseDelayInterval:                5 * time.Second,
	TokenFetcherMaxRetries:                    3,
	TokenFetcherRetryInterval:                 5 * time.Second,
	TokenFetcherExpirationBufferTimeInSeconds: 30,

	HealthCheckUserAgent: "HTTP-Monitor/1.1",
	LoadBalance:          LOAD_BALANCE_RR,

	ForwardedClientCert:      "always_forward",
	RoutingTableShardingMode: "all",

	DisableKeepAlives:   true,
	MaxIdleConns:        100,
	MaxIdleConnsPerHost: 2,
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

	c.Logging.JobName = "gorouter"
	if c.StartResponseDelayInterval > c.DropletStaleThreshold {
		c.DropletStaleThreshold = c.StartResponseDelayInterval
	}

	// To avoid routes getting purged because of unresponsive NATS server
	// we need to set the ping interval of nats client such that it fails over
	// to next NATS server before dropletstalethreshold is hit. We are hardcoding the ping interval
	// to 20 sec because the operators cannot set the value of DropletStaleThreshold and StartResponseDelayInterval
	// ping_interval = ((DropletStaleThreshold- StartResponseDelayInterval)-minimumRegistrationInterval+(2 * number_of_nats_servers))/3
	c.NatsClientPingInterval = 20 * time.Second

	if c.DrainTimeout == 0 || c.DrainTimeout == defaultConfig.EndpointTimeout {
		c.DrainTimeout = c.EndpointTimeout
	}

	c.Ip, err = localip.LocalIP()
	if err != nil {
		panic(err)
	}

	if c.EnableSSL {
		switch c.MinTLSVersionString {
		case "TLSv1.0":
			c.MinTLSVersion = tls.VersionTLS10
		case "TLSv1.1":
			c.MinTLSVersion = tls.VersionTLS11
		case "TLSv1.2", "":
			c.MinTLSVersion = tls.VersionTLS12
		default:
			panic(`router.min_tls_version should be one of "", "TLSv1.2", "TLSv1.1", "TLSv1.0"`)
		}

		if len(c.TLSPEM) == 0 {
			panic("router.tls_pem must be provided if router.enable_ssl is set to true")
		}

		for _, v := range c.TLSPEM {
			if len(v.PrivateKey) == 0 || len(v.CertChain) == 0 {
				panic("Error parsing PEM blocks of router.tls_pem, missing cert or key.")
			}

			certificate, err := tls.X509KeyPair([]byte(v.CertChain), []byte(v.PrivateKey))
			if err != nil {
				errMsg := fmt.Sprintf("Error loading key pair: %s", err.Error())
				panic(errMsg)
			}
			c.SSLCertificates = append(c.SSLCertificates, certificate)

		}
		c.CipherSuites = c.processCipherSuites()
	} else {
		if c.DisableHTTP {
			errMsg := fmt.Sprintf("neither http nor https listener is enabled: router.enable_ssl: %t, router.disable_http: %t", c.EnableSSL, c.DisableHTTP)
			panic(errMsg)
		}
	}

	if c.RouteServiceSecret != "" {
		c.RouteServiceEnabled = true
	}

	// check if valid load balancing strategy
	validLb := false
	for _, lb := range LoadBalancingStrategies {
		if c.LoadBalance == lb {
			validLb = true
			break
		}
	}
	if !validLb {
		errMsg := fmt.Sprintf("Invalid load balancing algorithm %s. Allowed values are %s", c.LoadBalance, LoadBalancingStrategies)
		panic(errMsg)
	}
	if c.LoadBalancerHealthyThreshold < 0 {
		errMsg := fmt.Sprintf("Invalid load balancer healthy threshold: %s", c.LoadBalancerHealthyThreshold)
		panic(errMsg)
	}

	validForwardedClientCertMode := false
	for _, fm := range AllowedForwardedClientCertModes {
		if c.ForwardedClientCert == fm {
			validForwardedClientCertMode = true
			break
		}
	}
	if !validForwardedClientCertMode {
		errMsg := fmt.Sprintf("Invalid forwarded client cert mode: %s. Allowed values are %s", c.ForwardedClientCert, AllowedForwardedClientCertModes)
		panic(errMsg)
	}

	validShardMode := false
	for _, sm := range AllowedShardingModes {
		if c.RoutingTableShardingMode == sm {
			validShardMode = true
			break
		}
	}
	if !validShardMode {
		errMsg := fmt.Sprintf("Invalid sharding mode: %s. Allowed values are %s", c.RoutingTableShardingMode, AllowedShardingModes)
		panic(errMsg)
	}

	if c.RoutingTableShardingMode == SHARD_SEGMENTS && len(c.IsolationSegments) == 0 {
		panic("Expected isolation segments; routing table sharding mode set to segments and none provided.")
	}

	if err := c.buildCertPool(); err != nil {
		panic(err)
	}
}

func (c *Config) processCipherSuites() []uint16 {
	cipherMap := map[string]uint16{
		"RC4-SHA":                                 0x0005, // openssl formatted values
		"DES-CBC3-SHA":                            0x000a,
		"AES128-SHA":                              0x002f,
		"AES256-SHA":                              0x0035,
		"AES128-SHA256":                           0x003c,
		"AES128-GCM-SHA256":                       0x009c,
		"AES256-GCM-SHA384":                       0x009d,
		"ECDHE-ECDSA-RC4-SHA":                     0xc007,
		"ECDHE-ECDSA-AES128-SHA":                  0xc009,
		"ECDHE-ECDSA-AES256-SHA":                  0xc00a,
		"ECDHE-RSA-RC4-SHA":                       0xc011,
		"ECDHE-RSA-DES-CBC3-SHA":                  0xc012,
		"ECDHE-RSA-AES128-SHA":                    0xc013,
		"ECDHE-RSA-AES256-SHA":                    0xc014,
		"ECDHE-ECDSA-AES128-SHA256":               0xc023,
		"ECDHE-RSA-AES128-SHA256":                 0xc027,
		"ECDHE-RSA-AES128-GCM-SHA256":             0xc02f,
		"ECDHE-ECDSA-AES128-GCM-SHA256":           0xc02b,
		"ECDHE-RSA-AES256-GCM-SHA384":             0xc030,
		"ECDHE-ECDSA-AES256-GCM-SHA384":           0xc02c,
		"ECDHE-RSA-CHACHA20-POLY1305":             0xcca8,
		"ECDHE-ECDSA-CHACHA20-POLY1305":           0xcca9,
		"TLS_RSA_WITH_RC4_128_SHA":                0x0005, // RFC formatted values
		"TLS_RSA_WITH_3DES_EDE_CBC_SHA":           0x000a,
		"TLS_RSA_WITH_AES_128_CBC_SHA":            0x002f,
		"TLS_RSA_WITH_AES_256_CBC_SHA":            0x0035,
		"TLS_RSA_WITH_AES_128_CBC_SHA256":         0x003c,
		"TLS_RSA_WITH_AES_128_GCM_SHA256":         0x009c,
		"TLS_RSA_WITH_AES_256_GCM_SHA384":         0x009d,
		"TLS_ECDHE_ECDSA_WITH_RC4_128_SHA":        0xc007,
		"TLS_ECDHE_ECDSA_WITH_AES_128_CBC_SHA":    0xc009,
		"TLS_ECDHE_ECDSA_WITH_AES_256_CBC_SHA":    0xc00a,
		"TLS_ECDHE_RSA_WITH_RC4_128_SHA":          0xc011,
		"TLS_ECDHE_RSA_WITH_3DES_EDE_CBC_SHA":     0xc012,
		"TLS_ECDHE_RSA_WITH_AES_128_CBC_SHA":      0xc013,
		"TLS_ECDHE_RSA_WITH_AES_256_CBC_SHA":      0xc014,
		"TLS_ECDHE_ECDSA_WITH_AES_128_CBC_SHA256": 0xc023,
		"TLS_ECDHE_RSA_WITH_AES_128_CBC_SHA256":   0xc027,
		"TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256":   0xc02f,
		"TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256": 0xc02b,
		"TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384":   0xc030,
		"TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384": 0xc02c,
		"TLS_ECDHE_RSA_WITH_CHACHA20_POLY1305":    0xcca8,
		"TLS_ECDHE_ECDSA_WITH_CHACHA20_POLY1305":  0xcca9,
	}

	var ciphers []string

	if len(strings.TrimSpace(c.CipherString)) == 0 {
		panic("must specify list of cipher suite when ssl is enabled")
	} else {
		ciphers = strings.Split(c.CipherString, ":")
	}

	return convertCipherStringToInt(ciphers, cipherMap)
}

func (c *Config) buildCertPool() error {
	certPool, err := x509.SystemCertPool()
	if err != nil {
		return err
	}

	for i, cert := range c.CACerts {
		if ok := certPool.AppendCertsFromPEM([]byte(cert)); !ok {
			return fmt.Errorf("Error while adding %d cert in CACerts to gorouter's cert pool", i)
		}
	}
	c.CAPool = certPool
	return nil
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
			errMsg := fmt.Sprintf("Invalid cipher string configuration: %s, please choose from %v", cipher, supportedCipherSuites)
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
	return yaml.Unmarshal(configYAML, &c)
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
