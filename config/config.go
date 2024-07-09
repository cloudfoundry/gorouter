package config

import (
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"fmt"
	"net/url"
	"os"
	"runtime"
	"strings"
	"time"

	"go.step.sm/crypto/pemutil"

	"code.cloudfoundry.org/gorouter/logger"
	"github.com/uber-go/zap"
	"gopkg.in/yaml.v2"

	"code.cloudfoundry.org/localip"
	"slices"
)

const (
	LOAD_BALANCE_RR           string = "round-robin"
	LOAD_BALANCE_LC           string = "least-connection"
	AZ_PREF_NONE              string = "none"
	AZ_PREF_LOCAL             string = "locally-optimistic"
	SHARD_ALL                 string = "all"
	SHARD_SEGMENTS            string = "segments"
	SHARD_SHARED_AND_SEGMENTS string = "shared-and-segments"
	ALWAYS_FORWARD            string = "always_forward"
	SANITIZE_SET              string = "sanitize_set"
	FORWARD                   string = "forward"
	REDACT_QUERY_PARMS_NONE   string = "none"
	REDACT_QUERY_PARMS_ALL    string = "all"
	REDACT_QUERY_PARMS_HASH   string = "hash"
)

var (
	LoadBalancingStrategies         = []string{LOAD_BALANCE_RR, LOAD_BALANCE_LC}
	AZPreferences                   = []string{AZ_PREF_NONE, AZ_PREF_LOCAL}
	AllowedShardingModes            = []string{SHARD_ALL, SHARD_SEGMENTS, SHARD_SHARED_AND_SEGMENTS}
	AllowedForwardedClientCertModes = []string{ALWAYS_FORWARD, FORWARD, SANITIZE_SET}
	AllowedQueryParmRedactionModes  = []string{REDACT_QUERY_PARMS_NONE, REDACT_QUERY_PARMS_ALL, REDACT_QUERY_PARMS_HASH}
)

type StringSet map[string]struct{}

func (ss *StringSet) UnmarshalYAML(unmarshal func(interface{}) error) error {
	var arr []string

	err := unmarshal(&arr)
	if err != nil {
		return err
	}

	*ss = make(map[string]struct{})

	for _, elem := range arr {
		(*ss)[elem] = struct{}{}
	}

	return nil
}

func (ss StringSet) MarshalYAML() (interface{}, error) {
	arr := make([]string, 0, len(ss))

	for k := range ss {
		arr = append(arr, k)
	}

	return arr, nil
}

type StatusConfig struct {
	Host                                 string             `yaml:"host"`
	Port                                 uint16             `yaml:"port"`
	EnableNonTLSHealthChecks             bool               `yaml:"enable_nontls_health_checks"`
	EnableDeprecatedVarzHealthzEndpoints bool               `yaml:"enable_deprecated_varz_healthz_endpoints"`
	TLSCert                              tls.Certificate    `yaml:"-"`
	TLS                                  StatusTLSConfig    `yaml:"tls"`
	User                                 string             `yaml:"user"`
	Pass                                 string             `yaml:"pass"`
	Routes                               StatusRoutesConfig `yaml:"routes"`
}

type StatusTLSConfig struct {
	Port        uint16 `yaml:"port"`
	Certificate string `yaml:"certificate"`
	Key         string `yaml:"key"`
}

type StatusRoutesConfig struct {
	Port uint16 `yaml:"port"`
}

var defaultStatusTLSConfig = StatusTLSConfig{
	Port: 8443,
}

var defaultStatusConfig = StatusConfig{
	Host:                     "0.0.0.0",
	Port:                     8080,
	User:                     "",
	Pass:                     "",
	EnableNonTLSHealthChecks: true,
	TLS:                      defaultStatusTLSConfig,
	Routes: StatusRoutesConfig{
		Port: 8082,
	},
}

type PrometheusConfig struct {
	Port     uint16 `yaml:"port"`
	CertPath string `yaml:"cert_path"`
	KeyPath  string `yaml:"key_path"`
	CAPath   string `yaml:"ca_path"`
}

type NatsConfig struct {
	Hosts                 []NatsHost       `yaml:"hosts"`
	User                  string           `yaml:"user"`
	Pass                  string           `yaml:"pass"`
	TLSEnabled            bool             `yaml:"tls_enabled"`
	CACerts               string           `yaml:"ca_certs"`
	CAPool                *x509.CertPool   `yaml:"-"`
	ClientAuthCertificate tls.Certificate  `yaml:"-"`
	TLSPem                `yaml:",inline"` // embed to get cert_chain and private_key for client authentication
}

type NatsHost struct {
	Hostname string
	Port     uint16
}

var defaultNatsConfig = NatsConfig{
	Hosts: []NatsHost{{Hostname: "localhost", Port: 4222}},
	User:  "",
	Pass:  "",
}

type RoutingApiConfig struct {
	Uri                   string         `yaml:"uri"`
	Port                  int            `yaml:"port"`
	AuthDisabled          bool           `yaml:"auth_disabled"`
	CACerts               string         `yaml:"ca_certs"`
	CAPool                *x509.CertPool `yaml:"-"`
	ClientAuthCertificate tls.Certificate
	TLSPem                `yaml:",inline"` // embed to get cert_chain and private_key for client authentication
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
	ClientAuthCertificate tls.Certificate
	MaxConns              int64            `yaml:"max_conns"`
	MaxAttempts           int              `yaml:"max_attempts"`
	TLSPem                `yaml:",inline"` // embed to get cert_chain and private_key for client authentication
}

type RouteServiceConfig struct {
	ClientAuthCertificate     tls.Certificate
	MaxAttempts               int              `yaml:"max_attempts"`
	StrictSignatureValidation bool             `yaml:"strict_signature_validation"`
	TLSPem                    `yaml:",inline"` // embed to get cert_chain and private_key for client authentication
}

type LoggingConfig struct {
	Syslog                 string       `yaml:"syslog"`
	SyslogAddr             string       `yaml:"syslog_addr"`
	SyslogNetwork          string       `yaml:"syslog_network"`
	Level                  string       `yaml:"level"`
	LoggregatorEnabled     bool         `yaml:"loggregator_enabled"`
	MetronAddress          string       `yaml:"metron_address"`
	DisableLogForwardedFor bool         `yaml:"disable_log_forwarded_for"`
	DisableLogSourceIP     bool         `yaml:"disable_log_source_ip"`
	RedactQueryParams      string       `yaml:"redact_query_params"`
	EnableAttemptsDetails  bool         `yaml:"enable_attempts_details"`
	Format                 FormatConfig `yaml:"format"`

	// This field is populated by the `Process` function.
	JobName string `yaml:"-"`
}

type FormatConfig struct {
	Timestamp string `yaml:"timestamp"`
}

type AccessLog struct {
	File            string `yaml:"file"`
	EnableStreaming bool   `yaml:"enable_streaming"`
}

type Tracing struct {
	EnableZipkin bool   `yaml:"enable_zipkin"`
	EnableW3C    bool   `yaml:"enable_w3c"`
	W3CTenantID  string `yaml:"w3c_tenant_id"`
}

type TLSPem struct {
	CertChain  string `yaml:"cert_chain"`
	PrivateKey string `yaml:"private_key"`
}

var defaultLoggingConfig = LoggingConfig{
	Level:                 "debug",
	MetronAddress:         "localhost:3457",
	Format:                FormatConfig{"unix-epoch"},
	JobName:               "gorouter",
	RedactQueryParams:     REDACT_QUERY_PARMS_NONE,
	EnableAttemptsDetails: false,
}

type HeaderNameValue struct {
	Name  string `yaml:"name"`
	Value string `yaml:"value,omitempty"`
}

type HTTPRewrite struct {
	Responses HTTPRewriteResponses `yaml:"responses,omitempty"`
}

type HTTPRewriteResponses struct {
	AddHeadersIfNotPresent []HeaderNameValue `yaml:"add_headers_if_not_present,omitempty"`
	RemoveHeaders          []HeaderNameValue `yaml:"remove_headers,omitempty"`
}

// VerifyClientCertificateMetadataRules defines verification rules for client certificates, which allow additional checks
// for the certificates' subject.
//
// A rule is applied based on the CA certificate's subject. The CA certificate is defined as part of `client_ca_certs`
// and identified via its subject. See VerifyClientCertMetadata() for the implementation of checks.
//
// For client certificates issued by a CA that matches CASubject, the valid client certificate subjects are defined in
// ValidSubjects.
type VerifyClientCertificateMetadataRule struct {
	// The issuer DN , for which the subject validation should apply
	CASubject CertSubject `yaml:"issuer_in_chain"`
	// The subject DNs	 that are allowed to be used for mTLS connections to Gorouter
	ValidSubjects []CertSubject `yaml:"valid_cert_subjects"`
}

// CertSubject defines the same fields as pkix.Name and allows YAML declaration of said fields. This is used to
// express distinguished names for certificate subjects in a comparable manner.
type CertSubject struct {
	Country            []string `yaml:"country"`
	Organization       []string `yaml:"organization"`
	OrganizationalUnit []string `yaml:"organizational_unit"`
	CommonName         string   `yaml:"common_name"`
	SerialNumber       string   `yaml:"serial_number"`
	Locality           []string `yaml:"locality"`
	Province           []string `yaml:"province"`
	StreetAddress      []string `yaml:"street_address"`
	PostalCode         []string `yaml:"postal_code"`
}

// ToName converts this CertSubject  to a pkix.Name.
func (c CertSubject) ToName() pkix.Name {
	return pkix.Name{
		Country:            c.Country,
		Organization:       c.Organization,
		OrganizationalUnit: c.OrganizationalUnit,
		CommonName:         c.CommonName,
		SerialNumber:       c.SerialNumber,
		Locality:           c.Locality,
		Province:           c.Province,
		StreetAddress:      c.StreetAddress,
		PostalCode:         c.PostalCode,
	}
}

// VerifyClientCertMetadata checks for the certificate chain received from the tls.Config.VerifyPeerCertificate
// function callback, whether any configured VerifyClientCertificateMetadataRule applies.
//
// If a rule does apply, it is evaluated.
//
// Returns an error if there is an applicable rule which does not find a valid client certificate subject.
func VerifyClientCertMetadata(rules []VerifyClientCertificateMetadataRule, chains [][]*x509.Certificate, logger logger.Logger) error {
	for _, rule := range rules {
		for _, chain := range chains {
			requiredSubject := rule.CASubject.ToName()
			if !checkIfRuleAppliesToChain(chain, logger, requiredSubject) {
				continue
			}
			err := checkClientCertificateMetadataRule(chain, logger, rule)
			if err != nil {
				return err
			}
		}
	}
	return nil
}

// checkIfRuleAppliesToChain checks, whether the provided certificate chain contains a CA certificate
// whose subject matches the requiredCASubject name.
//
// Returns true, if a CA certificate in the chain (cert.IsCA == true) matches the requiredCASubject name.
func checkIfRuleAppliesToChain(chain []*x509.Certificate, logger logger.Logger, requiredCASubject pkix.Name) bool {
	for i, cert := range chain {
		logger.Debug("cert", zap.Int("index", i), zap.Bool("ca", cert.IsCA), zap.String("subject", cert.Subject.String()), zap.String("issuer", cert.Issuer.String()))
		if cert.IsCA && requiredCASubject.ToRDNSequence().String() == cert.Subject.ToRDNSequence().String() {
			return true
		}
	}
	return false
}

// checkClientCertificateMetadataRule is called by checkIfRuleAppliesToChain. When the CA subject matches, the subject
// of the client certificate is compared agains the subjects defined in rule.
//
// Returns an error when:
// * the certificate does not match any of the ValidSubjects in rule.
// * the chain does not contain any client certificates (i.e. IsCA == false).
func checkClientCertificateMetadataRule(chain []*x509.Certificate, logger logger.Logger, rule VerifyClientCertificateMetadataRule) error {
	for _, cert := range chain {
		if cert.IsCA {
			continue
		}
		subject := cert.Subject
		for _, validSubject := range rule.ValidSubjects {
			validCertSubject := validSubject.ToName()
			if validCertSubject.ToRDNSequence().String() == subject.ToRDNSequence().String() {
				logger.Debug("chain", zap.String("issuer", cert.Issuer.String()), zap.Bool("CA", cert.IsCA), zap.String("subject", cert.Subject.String()))
				return nil
			}
		}
		logger.Warn("invalid-subject", zap.String("issuer", cert.Issuer.String()), zap.String("subject", cert.Subject.String()), zap.Object("allowed", rule.ValidSubjects))
		return fmt.Errorf("subject not in the list of allowed subjects for CA Subject %q: %q", rule.CASubject, subject)
	}
	// this should never happen as the function is only called on successful client certificate verification as callback
	// to tls.Config.VerifyPeerCertificate.
	return fmt.Errorf("cert chain provided to client certificate metadata verification did not contain a leaf certificate; this should never happen")
}

// InitClientCertMetadataRules compares the defined rules against client CAs set in `client_ca_certs`. When a rule
// is found that does not have a corresponding client CA (based on the CA's subject) that matches the rule, startup will fail.
//
// This is to avoid defining a rule with a minor typo that would then not apply at all and would make the whole
// additional metadata check moot.
func InitClientCertMetadataRules(rules []VerifyClientCertificateMetadataRule, certs []*x509.Certificate) error {
	for _, rule := range rules {
		found := false
		for _, cert := range certs {
			if cert.Subject.ToRDNSequence().String() == rule.CASubject.ToName().ToRDNSequence().String() {
				found = true
			}
		}
		if !found {
			return fmt.Errorf("no CA certificate found for rule with ca subject %s", rule.CASubject.ToName().String())
		}
	}
	return nil
}

type Config struct {
	Status                         StatusConfig      `yaml:"status,omitempty"`
	Nats                           NatsConfig        `yaml:"nats,omitempty"`
	Logging                        LoggingConfig     `yaml:"logging,omitempty"`
	Port                           uint16            `yaml:"port,omitempty"`
	Prometheus                     PrometheusConfig  `yaml:"prometheus,omitempty"`
	Index                          uint              `yaml:"index,omitempty"`
	Zone                           string            `yaml:"zone,omitempty"`
	GoMaxProcs                     int               `yaml:"go_max_procs,omitempty"`
	Tracing                        Tracing           `yaml:"tracing,omitempty"`
	TraceKey                       string            `yaml:"trace_key,omitempty"`
	AccessLog                      AccessLog         `yaml:"access_log,omitempty"`
	DebugAddr                      string            `yaml:"debug_addr,omitempty"`
	EnablePROXY                    bool              `yaml:"enable_proxy,omitempty"`
	EnableSSL                      bool              `yaml:"enable_ssl,omitempty"`
	SSLPort                        uint16            `yaml:"ssl_port,omitempty"`
	DisableHTTP                    bool              `yaml:"disable_http,omitempty"`
	EnableHTTP2                    bool              `yaml:"enable_http2"`
	EnableHTTP1ConcurrentReadWrite bool              `yaml:"enable_http1_concurrent_read_write"`
	SSLCertificates                []tls.Certificate `yaml:"-"`
	TLSPEM                         []TLSPem          `yaml:"tls_pem,omitempty"`
	CACerts                        []string          `yaml:"ca_certs,omitempty"`
	CAPool                         *x509.CertPool    `yaml:"-"`
	ClientCACerts                  string            `yaml:"client_ca_certs,omitempty"`
	ClientCAPool                   *x509.CertPool    `yaml:"-"`

	SkipSSLValidation        bool     `yaml:"skip_ssl_validation,omitempty"`
	ForwardedClientCert      string   `yaml:"forwarded_client_cert,omitempty"`
	ForceForwardedProtoHttps bool     `yaml:"force_forwarded_proto_https,omitempty"`
	SanitizeForwardedProto   bool     `yaml:"sanitize_forwarded_proto,omitempty"`
	HopByHopHeadersToFilter  []string `yaml:"hop_by_hop_headers_to_filter"`
	IsolationSegments        []string `yaml:"isolation_segments,omitempty"`
	RoutingTableShardingMode string   `yaml:"routing_table_sharding_mode,omitempty"`

	CipherString                                    string                                `yaml:"cipher_suites,omitempty"`
	CipherSuites                                    []uint16                              `yaml:"-"`
	MinTLSVersionString                             string                                `yaml:"min_tls_version,omitempty"`
	MaxTLSVersionString                             string                                `yaml:"max_tls_version,omitempty"`
	MinTLSVersion                                   uint16                                `yaml:"-"`
	MaxTLSVersion                                   uint16                                `yaml:"-"`
	ClientCertificateValidationString               string                                `yaml:"client_cert_validation,omitempty"`
	ClientCertificateValidation                     tls.ClientAuthType                    `yaml:"-"`
	OnlyTrustClientCACerts                          bool                                  `yaml:"only_trust_client_ca_certs"`
	TLSHandshakeTimeout                             time.Duration                         `yaml:"tls_handshake_timeout"`
	VerifyClientCertificatesBasedOnProvidedMetadata bool                                  `yaml:"enable_verify_client_certificate_metadata,omitempty"`
	VerifyClientCertificateMetadataRules            []VerifyClientCertificateMetadataRule `yaml:"verify_client_certificate_metadata,omitempty"`

	LoadBalancerHealthyThreshold    time.Duration `yaml:"load_balancer_healthy_threshold,omitempty"`
	PublishStartMessageInterval     time.Duration `yaml:"publish_start_message_interval,omitempty"`
	SuspendPruningIfNatsUnavailable bool          `yaml:"suspend_pruning_if_nats_unavailable,omitempty"`
	PruneStaleDropletsInterval      time.Duration `yaml:"prune_stale_droplets_interval,omitempty"`
	DropletStaleThreshold           time.Duration `yaml:"droplet_stale_threshold,omitempty"`
	PublishActiveAppsInterval       time.Duration `yaml:"publish_active_apps_interval,omitempty"`
	StartResponseDelayInterval      time.Duration `yaml:"start_response_delay_interval,omitempty"`
	EndpointTimeout                 time.Duration `yaml:"endpoint_timeout,omitempty"`
	EndpointDialTimeout             time.Duration `yaml:"endpoint_dial_timeout,omitempty"`
	WebsocketDialTimeout            time.Duration `yaml:"websocket_dial_timeout,omitempty"`
	EndpointKeepAliveProbeInterval  time.Duration `yaml:"endpoint_keep_alive_probe_interval,omitempty"`
	RouteServiceTimeout             time.Duration `yaml:"route_services_timeout,omitempty"`
	FrontendIdleTimeout             time.Duration `yaml:"frontend_idle_timeout,omitempty"`

	RouteLatencyMetricMuzzleDuration time.Duration `yaml:"route_latency_metric_muzzle_duration,omitempty"`

	DrainWait                      time.Duration `yaml:"drain_wait,omitempty"`
	DrainTimeout                   time.Duration `yaml:"drain_timeout,omitempty"`
	SecureCookies                  bool          `yaml:"secure_cookies,omitempty"`
	StickySessionCookieNames       StringSet     `yaml:"sticky_session_cookie_names"`
	StickySessionsForAuthNegotiate bool          `yaml:"sticky_sessions_for_auth_negotiate"`
	HealthCheckUserAgent           string        `yaml:"healthcheck_user_agent,omitempty"`

	OAuth                             OAuthConfig      `yaml:"oauth,omitempty"`
	RoutingApi                        RoutingApiConfig `yaml:"routing_api,omitempty"`
	RouteServiceSecret                string           `yaml:"route_services_secret,omitempty"`
	RouteServiceSecretPrev            string           `yaml:"route_services_secret_decrypt_only,omitempty"`
	RouteServiceRecommendHttps        bool             `yaml:"route_services_recommend_https,omitempty"`
	RouteServicesHairpinning          bool             `yaml:"route_services_hairpinning"`
	RouteServicesHairpinningAllowlist []string         `yaml:"route_services_hairpinning_allowlist,omitempty"`
	RouteServicesServerPort           uint16           `yaml:"route_services_internal_server_port"`
	// These fields are populated by the `Process` function.
	Ip                          string        `yaml:"-"`
	RouteServiceEnabled         bool          `yaml:"-"`
	NatsClientPingInterval      time.Duration `yaml:"nats_client_ping_interval,omitempty"`
	NatsClientMessageBufferSize int           `yaml:"-"`
	Backends                    BackendConfig `yaml:"backends,omitempty"`
	ExtraHeadersToLog           []string      `yaml:"extra_headers_to_log,omitempty"`

	RouteServiceConfig RouteServiceConfig `yaml:"route_services,omitempty"`

	TokenFetcherMaxRetries                    uint32        `yaml:"token_fetcher_max_retries,omitempty"`
	TokenFetcherRetryInterval                 time.Duration `yaml:"token_fetcher_retry_interval,omitempty"`
	TokenFetcherExpirationBufferTimeInSeconds int64         `yaml:"token_fetcher_expiration_buffer_time,omitempty"`

	PidFile                 string `yaml:"pid_file,omitempty"`
	LoadBalance             string `yaml:"balancing_algorithm,omitempty"`
	LoadBalanceAZPreference string `yaml:"balancing_algorithm_az_preference,omitempty"`

	DisableKeepAlives            bool `yaml:"disable_keep_alives"`
	MaxIdleConns                 int  `yaml:"max_idle_conns,omitempty"`
	MaxIdleConnsPerHost          int  `yaml:"max_idle_conns_per_host,omitempty"`
	MaxHeaderBytes               int  `yaml:"max_header_bytes"`
	KeepAlive100ContinueRequests bool `yaml:"keep_alive_100_continue_requests"`

	HTTPRewrite HTTPRewrite `yaml:"http_rewrite,omitempty"`

	EmptyPoolResponseCode503 bool          `yaml:"empty_pool_response_code_503,omitempty"`
	EmptyPoolTimeout         time.Duration `yaml:"empty_pool_timeout,omitempty"`

	HTMLErrorTemplateFile string `yaml:"html_error_template_file,omitempty"`

	// Old metric, to eventually be replaced by prometheus reporting
	// reports latency under gorouter sourceid, and with and without component name
	PerRequestMetricsReporting bool `yaml:"per_request_metrics_reporting,omitempty"`

	// Old metric, to eventually be replaced by prometheus reporting
	SendHttpStartStopServerEvent bool `yaml:"send_http_start_stop_server_event,omitempty"`

	// Old metric, to eventually be replaced by prometheus reporting
	SendHttpStartStopClientEvent bool `yaml:"send_http_start_stop_client_event,omitempty"`

	PerAppPrometheusHttpMetricsReporting bool `yaml:"per_app_prometheus_http_metrics_reporting,omitempty"`

	HealthCheckPollInterval time.Duration `yaml:"healthcheck_poll_interval"`
	HealthCheckTimeout      time.Duration `yaml:"healthcheck_timeout"`
}

var defaultConfig = Config{
	Status:                         defaultStatusConfig,
	Nats:                           defaultNatsConfig,
	Logging:                        defaultLoggingConfig,
	Port:                           8081,
	Index:                          0,
	GoMaxProcs:                     -1,
	EnablePROXY:                    false,
	EnableSSL:                      false,
	SSLPort:                        443,
	DisableHTTP:                    false,
	EnableHTTP2:                    true,
	EnableHTTP1ConcurrentReadWrite: false,
	MinTLSVersion:                  tls.VersionTLS12,
	MaxTLSVersion:                  tls.VersionTLS12,
	RouteServicesServerPort:        7070,

	EndpointTimeout:                60 * time.Second,
	EndpointDialTimeout:            5 * time.Second,
	EndpointKeepAliveProbeInterval: 1 * time.Second,
	RouteServiceTimeout:            60 * time.Second,
	TLSHandshakeTimeout:            10 * time.Second,

	PublishStartMessageInterval:               30 * time.Second,
	PruneStaleDropletsInterval:                30 * time.Second,
	DropletStaleThreshold:                     120 * time.Second,
	PublishActiveAppsInterval:                 0 * time.Second,
	StartResponseDelayInterval:                5 * time.Second,
	TokenFetcherMaxRetries:                    3,
	TokenFetcherRetryInterval:                 5 * time.Second,
	TokenFetcherExpirationBufferTimeInSeconds: 30,
	FrontendIdleTimeout:                       900 * time.Second,
	RouteLatencyMetricMuzzleDuration:          20 * time.Second,

	// To avoid routes getting purged because of unresponsive NATS server
	// we need to set the ping interval of nats client such that it fails over
	// to next NATS server before dropletstalethreshold is hit. We are hardcoding the ping interval
	// to 20 sec because the operators cannot set the value of DropletStaleThreshold and StartResponseDelayInterval
	// ping_interval = ((DropletStaleThreshold- StartResponseDelayInterval)-minimumRegistrationInterval+(2 * number_of_nats_servers))/3
	NatsClientPingInterval: 20 * time.Second,
	// This is set to twice the defaults from the NATS library
	NatsClientMessageBufferSize: 131072,

	HealthCheckUserAgent:    "HTTP-Monitor/1.1",
	LoadBalance:             LOAD_BALANCE_RR,
	LoadBalanceAZPreference: AZ_PREF_NONE,

	ForwardedClientCert:      "always_forward",
	RoutingTableShardingMode: "all",

	DisableKeepAlives:   true,
	MaxIdleConns:        100,
	MaxIdleConnsPerHost: 2,

	StickySessionCookieNames:       StringSet{"JSESSIONID": struct{}{}},
	StickySessionsForAuthNegotiate: false,

	PerRequestMetricsReporting: true,

	SendHttpStartStopServerEvent: true,

	SendHttpStartStopClientEvent: true,

	// Default load balancer values
	HealthCheckPollInterval: 10 * time.Second,
	HealthCheckTimeout:      5 * time.Second,
}

func DefaultConfig() (*Config, error) {
	c := defaultConfig
	return &c, nil
}

func IsLoadBalancingAlgorithmValid(lbAlgo string) bool {
	return slices.Contains(LoadBalancingStrategies, lbAlgo)
}

func (c *Config) Process() error {
	if c.GoMaxProcs == -1 {
		c.GoMaxProcs = runtime.NumCPU()
	}

	c.Logging.JobName = "gorouter"
	if c.StartResponseDelayInterval > c.DropletStaleThreshold {
		c.DropletStaleThreshold = c.StartResponseDelayInterval
	}

	if c.DrainTimeout == 0 {
		c.DrainTimeout = c.EndpointTimeout
	}

	if c.WebsocketDialTimeout == 0 {
		c.WebsocketDialTimeout = c.EndpointDialTimeout
	}

	var localIPErr error
	c.Ip, localIPErr = localip.LocalIP()
	if localIPErr != nil {
		return localIPErr
	}

	if c.Backends.CertChain != "" && c.Backends.PrivateKey != "" {
		certificate, err := tls.X509KeyPair([]byte(c.Backends.CertChain), []byte(c.Backends.PrivateKey))
		if err != nil {
			errMsg := fmt.Sprintf("Error loading key pair: %s", err.Error())
			//lint:ignore SA1006 - for consistency sake
			return fmt.Errorf(errMsg)
		}
		c.Backends.ClientAuthCertificate = certificate
	}

	if c.RouteServiceConfig.CertChain != "" && c.RouteServiceConfig.PrivateKey != "" {
		certificate, err := tls.X509KeyPair([]byte(c.RouteServiceConfig.CertChain), []byte(c.RouteServiceConfig.PrivateKey))
		if err != nil {
			errMsg := fmt.Sprintf("Error loading key pair: %s", err.Error())
			//lint:ignore SA1006 - for consistency sake
			return fmt.Errorf(errMsg)
		}
		c.RouteServiceConfig.ClientAuthCertificate = certificate
	}

	if c.RoutingApiEnabled() {
		certificate, err := tls.X509KeyPair([]byte(c.RoutingApi.CertChain), []byte(c.RoutingApi.PrivateKey))
		if err != nil {
			errMsg := fmt.Sprintf("Error loading key pair: %s", err.Error())
			//lint:ignore SA1006 - for consistency sake
			return fmt.Errorf(errMsg)
		}
		c.RoutingApi.ClientAuthCertificate = certificate

		certPool := x509.NewCertPool()

		if ok := certPool.AppendCertsFromPEM([]byte(c.RoutingApi.CACerts)); !ok {
			return fmt.Errorf("Error while adding CACerts to gorouter's routing-api cert pool: \n%s\n", c.RoutingApi.CACerts)
		}
		c.RoutingApi.CAPool = certPool
	}

	if c.Nats.TLSEnabled {
		certificate, err := tls.X509KeyPair([]byte(c.Nats.CertChain), []byte(c.Nats.PrivateKey))
		if err != nil {
			errMsg := fmt.Sprintf("Error loading NATS key pair: %s", err.Error())
			//lint:ignore SA1006 - for consistency sake
			return fmt.Errorf(errMsg)
		}
		c.Nats.ClientAuthCertificate = certificate

		certPool := x509.NewCertPool()
		if ok := certPool.AppendCertsFromPEM([]byte(c.Nats.CACerts)); !ok {
			return fmt.Errorf("Error while adding CACerts to gorouter's routing-api cert pool: \n%s\n", c.Nats.CACerts)
		}
		c.Nats.CAPool = certPool
	}

	healthTLS := c.Status.TLS
	if healthTLS == defaultStatusTLSConfig && !c.Status.EnableNonTLSHealthChecks {
		return fmt.Errorf("Neither TLS nor non-TLS health endpoints are enabled. Refusing to start gorouter.")
	}

	if healthTLS != defaultStatusTLSConfig {
		if healthTLS.Key == "" {
			return fmt.Errorf("router.status.tls.key must be provided")
		}
		if healthTLS.Certificate == "" {
			return fmt.Errorf("router.status.tls.certificate must be provided")
		}
		if healthTLS.Port == 0 {
			return fmt.Errorf("router.status.tls.port must not be 0")
		}
		certificate, err := tls.X509KeyPair([]byte(healthTLS.Certificate), []byte(healthTLS.Key))
		if err != nil {
			return fmt.Errorf("Error loading router.status.tls certificate/key pair: %s", err.Error())
		}
		c.Status.TLSCert = certificate
	}

	if c.EnableSSL {
		switch c.ClientCertificateValidationString {
		case "none":
			c.ClientCertificateValidation = tls.NoClientCert
		case "request":
			c.ClientCertificateValidation = tls.VerifyClientCertIfGiven
		case "require":
			c.ClientCertificateValidation = tls.RequireAndVerifyClientCert
		default:
			return fmt.Errorf(`router.client_cert_validation must be one of 'none', 'request' or 'require'.`)
		}

		switch c.MinTLSVersionString {
		case "TLSv1.0":
			c.MinTLSVersion = tls.VersionTLS10
		case "TLSv1.1":
			c.MinTLSVersion = tls.VersionTLS11
		case "TLSv1.2", "":
			c.MinTLSVersion = tls.VersionTLS12
		case "TLSv1.3":
			c.MinTLSVersion = tls.VersionTLS13
		default:
			return fmt.Errorf(`router.min_tls_version should be one of "", "TLSv1.3", "TLSv1.2", "TLSv1.1", "TLSv1.0"`)
		}

		switch c.MaxTLSVersionString {
		case "TLSv1.2", "":
			c.MaxTLSVersion = tls.VersionTLS12
		case "TLSv1.3":
			c.MaxTLSVersion = tls.VersionTLS13
		default:
			return fmt.Errorf(`router.max_tls_version should be one of "TLSv1.2" or "TLSv1.3"`)
		}

		if len(c.TLSPEM) == 0 {
			return fmt.Errorf("router.tls_pem must be provided if router.enable_ssl is set to true")
		}

		for _, v := range c.TLSPEM {
			if len(v.PrivateKey) == 0 || len(v.CertChain) == 0 {
				return fmt.Errorf("Error parsing PEM blocks of router.tls_pem, missing cert or key.")
			}

			certificate, err := tls.X509KeyPair([]byte(v.CertChain), []byte(v.PrivateKey))
			if err != nil {
				errMsg := fmt.Sprintf("Error loading key pair: %s", err.Error())
				//lint:ignore SA1006 - for consistency sake
				return fmt.Errorf(errMsg)
			}
			c.SSLCertificates = append(c.SSLCertificates, certificate)

		}
		var err error
		c.CipherSuites, err = c.processCipherSuites()
		if err != nil {
			return err
		}
	} else {
		if c.DisableHTTP {
			errMsg := fmt.Sprintf("neither http nor https listener is enabled: router.enable_ssl: %t, router.disable_http: %t", c.EnableSSL, c.DisableHTTP)
			//lint:ignore SA1006 - for consistency sake
			return fmt.Errorf(errMsg)
		}
	}

	if c.RouteServiceSecret != "" {
		c.RouteServiceEnabled = true
	}

	// check if valid load balancing strategy
	if !IsLoadBalancingAlgorithmValid(c.LoadBalance) {
		errMsg := fmt.Sprintf("Invalid load balancing algorithm %s. Allowed values are %s", c.LoadBalance, LoadBalancingStrategies)
		//lint:ignore SA1006 - for consistency sake
		return fmt.Errorf(errMsg)
	}

	validAZPref := false
	for _, p := range AZPreferences {
		if c.LoadBalanceAZPreference == p {
			validAZPref = true
			break
		}
	}
	if !validAZPref {
		errMsg := fmt.Sprintf("Invalid load balancing AZ preference %s. Allowed values are %s", c.LoadBalanceAZPreference, AZPreferences)
		//lint:ignore SA1006 - for consistency sake
		return fmt.Errorf(errMsg)
	}

	if c.LoadBalancerHealthyThreshold < 0 {
		errMsg := fmt.Sprintf("Invalid load balancer healthy threshold: %s", c.LoadBalancerHealthyThreshold)
		//lint:ignore SA1006 - for consistency sake
		return fmt.Errorf(errMsg)
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
		//lint:ignore SA1006 - for consistency sake
		return fmt.Errorf(errMsg)
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
		//lint:ignore SA1006 - for consistency sake
		return fmt.Errorf(errMsg)
	}

	if c.RoutingTableShardingMode == SHARD_SEGMENTS && len(c.IsolationSegments) == 0 {
		return fmt.Errorf("Expected isolation segments; routing table sharding mode set to segments and none provided.")
	}

	validQueryParamRedaction := false
	for _, sm := range AllowedQueryParmRedactionModes {
		if c.Logging.RedactQueryParams == sm {
			validQueryParamRedaction = true
			break
		}
	}
	if !validQueryParamRedaction {
		errMsg := fmt.Sprintf("Invalid query param redaction mode: %s. Allowed values are %s", c.Logging.RedactQueryParams, AllowedQueryParmRedactionModes)
		//lint:ignore SA1006 - for consistency sake
		return fmt.Errorf(errMsg)
	}

	if err := c.buildCertPool(); err != nil {
		return err
	}
	if err := c.buildClientCertPool(); err != nil {
		return err
	}
	return nil
}

func (c *Config) processCipherSuites() ([]uint16, error) {
	// legacy/openssl formatted values that we've supported in the past
	cipherMap := map[string]uint16{
		"RC4-SHA":                                0x0005, // openssl formatted values
		"DES-CBC3-SHA":                           0x000a,
		"AES128-SHA":                             0x002f,
		"AES256-SHA":                             0x0035,
		"AES128-SHA256":                          0x003c,
		"AES128-GCM-SHA256":                      0x009c,
		"AES256-GCM-SHA384":                      0x009d,
		"ECDHE-ECDSA-RC4-SHA":                    0xc007,
		"ECDHE-ECDSA-AES128-SHA":                 0xc009,
		"ECDHE-ECDSA-AES256-SHA":                 0xc00a,
		"ECDHE-RSA-RC4-SHA":                      0xc011,
		"ECDHE-RSA-DES-CBC3-SHA":                 0xc012,
		"ECDHE-RSA-AES128-SHA":                   0xc013,
		"ECDHE-RSA-AES256-SHA":                   0xc014,
		"ECDHE-ECDSA-AES128-SHA256":              0xc023,
		"ECDHE-RSA-AES128-SHA256":                0xc027,
		"ECDHE-RSA-AES128-GCM-SHA256":            0xc02f,
		"ECDHE-ECDSA-AES128-GCM-SHA256":          0xc02b,
		"ECDHE-RSA-AES256-GCM-SHA384":            0xc030,
		"ECDHE-ECDSA-AES256-GCM-SHA384":          0xc02c,
		"ECDHE-RSA-CHACHA20-POLY1305":            0xcca8,
		"ECDHE-ECDSA-CHACHA20-POLY1305":          0xcca9,
		"TLS_ECDHE_RSA_WITH_CHACHA20_POLY1305":   0xcca8,
		"TLS_ECDHE_ECDSA_WITH_CHACHA20_POLY1305": 0xcca9,
	}
	// generate the remaining suites based off of what golang has implemented using RFC names
	for _, suite := range append(tls.CipherSuites(), tls.InsecureCipherSuites()...) {
		cipherMap[suite.Name] = suite.ID
	}

	var ciphers []string

	if len(strings.TrimSpace(c.CipherString)) == 0 {
		return nil, fmt.Errorf("must specify list of cipher suite when ssl is enabled")
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

func (c *Config) buildClientCertPool() error {
	var certPool *x509.CertPool
	var err error

	if c.OnlyTrustClientCACerts {
		certPool = x509.NewCertPool()
	} else {
		certPool, err = x509.SystemCertPool()
		if err != nil {
			return err
		}
	}

	if c.ClientCACerts == "" {
		if c.OnlyTrustClientCACerts && c.ClientCertificateValidation != tls.NoClientCert {
			return fmt.Errorf(`router.client_ca_certs cannot be empty if router.only_trust_client_ca_certs is 'true' and router.client_cert_validation is set to 'request' or 'require'.`)
		}
	} else {
		if ok := certPool.AppendCertsFromPEM([]byte(c.ClientCACerts)); !ok {
			return fmt.Errorf("Error while adding ClientCACerts to gorouter's client cert pool: \n%s\n", c.ClientCACerts)
		}
	}
	c.ClientCAPool = certPool

	if c.VerifyClientCertificatesBasedOnProvidedMetadata && c.VerifyClientCertificateMetadataRules != nil {
		bundle, err := pemutil.ParseCertificateBundle([]byte(c.ClientCACerts))
		if err != nil {
			return err
		}

		err = InitClientCertMetadataRules(c.VerifyClientCertificateMetadataRules, bundle)
		if err != nil {
			return err
		}
	}
	return nil
}

func convertCipherStringToInt(cipherStrs []string, cipherMap map[string]uint16) ([]uint16, error) {
	ciphers := []uint16{}
	for _, cipher := range cipherStrs {
		if val, ok := cipherMap[cipher]; ok {
			ciphers = append(ciphers, val)
		} else {
			supportedCipherSuites := []string{}
			for key := range cipherMap {
				supportedCipherSuites = append(supportedCipherSuites, key)
			}
			errMsg := fmt.Sprintf("Invalid cipher string configuration: %s, please choose from %v", cipher, supportedCipherSuites)
			//lint:ignore SA1006 - for consistency sake
			return nil, fmt.Errorf(errMsg)
		}
	}

	return ciphers, nil
}

func (c *Config) NatsServers() []string {
	var natsServers []string
	for _, host := range c.Nats.Hosts {
		uri := url.URL{
			Scheme: "nats",
			User:   url.UserPassword(c.Nats.User, c.Nats.Pass),
			Host:   fmt.Sprintf("%s:%d", host.Hostname, host.Port),
		}
		natsServers = append(natsServers, uri.String())
	}

	return natsServers
}

func (c *Config) RoutingApiEnabled() bool {
	return (c.RoutingApi.Uri != "") && (c.RoutingApi.Port != 0)
}

func (c *Config) Initialize(configYAML []byte) error {
	return yaml.Unmarshal(configYAML, &c)
}

func InitConfigFromFile(path string) (*Config, error) {
	c, err := DefaultConfig()
	if err != nil {
		return nil, err
	}

	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	err = c.Initialize(b)
	if err != nil {
		return nil, err
	}

	err = c.Process()
	if err != nil {
		return nil, err
	}

	return c, nil
}
