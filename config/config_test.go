package config_test

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"gopkg.in/yaml.v3"

	. "code.cloudfoundry.org/gorouter/config"
	"code.cloudfoundry.org/gorouter/test_util"
)

var _ = Describe("Config", func() {
	var config, cfgForSnippet *Config

	BeforeEach(func() {
		var err error
		config, err = DefaultConfig()
		Expect(err).ToNot(HaveOccurred())
		cfgForSnippet = baseConfigFixture()
	})

	createYMLSnippet := func(snippet *Config) []byte {
		cfgBytes, err := yaml.Marshal(snippet)
		Expect(err).ToNot(HaveOccurred())
		return cfgBytes
	}

	Describe("Initialize", func() {

		Context("zone", func() {
			It("can set the zone", func() {
				cfg, err := DefaultConfig()
				Expect(err).ToNot(HaveOccurred())
				var b = []byte(`
zone: meow-zone
`)
				cfg.Initialize(b)
				cfg.Process()
				Expect(cfg.Zone).To(Equal("meow-zone"))
			})
		})

		Context("Load Balancing Algorithm Validator", func() {
			It("Returns false if the value is not in the list of configured load balancing strategies", func() {
				Expect(IsLoadBalancingAlgorithmValid("wrong.algo")).To(Equal(false))
			})

			It("Returns true if the value is in the list of configured load balancing strategies", func() {
				Expect(IsLoadBalancingAlgorithmValid(LOAD_BALANCE_RR)).To(Equal(true))
			})
		})

		Context("load balance config", func() {
			It("sets default load balance strategy", func() {
				Expect(config.LoadBalance).To(Equal(LOAD_BALANCE_RR))
			})

			It("can override the load balance strategy", func() {
				cfg, err := DefaultConfig()
				Expect(err).ToNot(HaveOccurred())
				var b = []byte(`
balancing_algorithm: least-connection
`)
				cfg.Initialize(b)
				cfg.Process()
				Expect(cfg.LoadBalance).To(Equal(LOAD_BALANCE_LC))
			})

			It("does not allow an invalid load balance strategy", func() {
				cfg, err := DefaultConfig()
				Expect(err).ToNot(HaveOccurred())
				cfgForSnippet.LoadBalance = "foo-bar"
				cfg.Initialize(createYMLSnippet(cfgForSnippet))
				Expect(cfg.Process()).To(MatchError("Invalid load balancing algorithm foo-bar. Allowed values are [round-robin least-connection]"))
			})
		})

		Context("load balance az preference config", func() {
			It("sets default load balance az preference", func() {
				Expect(config.LoadBalanceAZPreference).To(Equal(AZ_PREF_NONE))
			})

			It("can override the load balance az preference", func() {
				cfg, err := DefaultConfig()
				Expect(err).ToNot(HaveOccurred())
				var b = []byte(`
balancing_algorithm_az_preference: locally-optimistic
`)
				cfg.Initialize(b)
				cfg.Process()
				Expect(cfg.LoadBalanceAZPreference).To(Equal(AZ_PREF_LOCAL))
			})

			It("does not allow an invalid load balance strategy", func() {
				cfg, err := DefaultConfig()
				Expect(err).ToNot(HaveOccurred())
				cfgForSnippet.LoadBalanceAZPreference = "meow-only"
				cfg.Initialize(createYMLSnippet(cfgForSnippet))
				Expect(cfg.Process()).To(MatchError("Invalid load balancing AZ preference meow-only. Allowed values are [none locally-optimistic]"))
			})
		})

		It("sets status config", func() {
			var b = []byte(`
status:
  port: 1234
  user: user
  pass: pass
  routes:
    port: 8082
`)

			err := config.Initialize(b)
			Expect(err).ToNot(HaveOccurred())

			Expect(config.Status.Port).To(Equal(uint16(1234)))
			Expect(config.Status.TLSCert).To(Equal(tls.Certificate{}))
			Expect(config.Status.User).To(Equal("user"))
			Expect(config.Status.Pass).To(Equal("pass"))
			Expect(config.Status.Routes.Port).To(Equal(uint16(8082)))
		})
		Context("when neither tls nor nontls health endpoints are enabled", func() {
			JustBeforeEach(func() {
				cfgForSnippet.Status.EnableNonTLSHealthChecks = false
				cfgForSnippet.Status.TLS.Certificate = ""
				cfgForSnippet.Status.TLS.Key = ""
				err := config.Initialize(createYMLSnippet(cfgForSnippet))
				Expect(err).ToNot(HaveOccurred())
			})
			It("throws an error", func() {
				err := config.Process()
				Expect(err).To(HaveOccurred())
				Expect(err).To(MatchError("Neither TLS nor non-TLS health endpoints are enabled. Refusing to start gorouter."))
			})
		})
		Context("when tls is specified for the status config", func() {
			var certPEM, keyPEM []byte
			var tlsPort uint16

			BeforeEach(func() {
				keyPEM, certPEM = test_util.CreateKeyPair("default")
				tlsPort = 8443
			})
			JustBeforeEach(func() {
				cfgForSnippet.Status.TLS = StatusTLSConfig{
					Port:        tlsPort,
					Certificate: string(certPEM),
					Key:         string(keyPEM),
				}

				err := config.Initialize(createYMLSnippet(cfgForSnippet))
				Expect(err).ToNot(HaveOccurred())
			})
			It("parses the cert + key", func() {
				err := config.Process()
				Expect(err).ToNot(HaveOccurred())

				Expect(config.Status.TLS.Port).To(Equal(tlsPort))
				Expect(config.Status.TLS.Certificate).To(Equal(string(certPEM)))
				Expect(config.Status.TLS.Key).To(Equal(string(keyPEM)))

				cert, err := tls.X509KeyPair(certPEM, keyPEM)
				Expect(err).ToNot(HaveOccurred())
				Expect(config.Status.TLSCert).To(Equal(cert))
			})
			Context("and the certificate is invalid", func() {
				BeforeEach(func() {
					certPEM = []byte("blarg")
				})
				It("throws an error", func() {
					err := config.Process()
					Expect(err).To(HaveOccurred())
					Expect(err).To(MatchError(ContainSubstring("Error loading router.status.tls certificate/key pair")))
				})
			})
			Context("and the key is invalid", func() {
				BeforeEach(func() {
					keyPEM = []byte("blarg")
				})
				It("throws an error", func() {
					err := config.Process()
					Expect(err).To(HaveOccurred())
					Expect(err).To(MatchError(ContainSubstring("Error loading router.status.tls certificate/key pair")))
				})
			})
			Context("and the cert is missing", func() {
				BeforeEach(func() {
					certPEM = []byte{}
				})
				It("throws an error", func() {
					err := config.Process()
					Expect(err).To(HaveOccurred())
					Expect(err).To(MatchError(ContainSubstring("router.status.tls.certificate must be provided")))
				})
			})
			Context("and the key is missing", func() {
				BeforeEach(func() {
					keyPEM = []byte{}
				})
				It("throws an error", func() {
					err := config.Process()
					Expect(err).To(HaveOccurred())
					Expect(err).To(MatchError(ContainSubstring("router.status.tls.key must be provided")))
				})
			})
		})
		It("sets MaxHeaderBytes", func() {
			var b = []byte(`
max_request_header_bytes: 10
`)

			err := config.Initialize(b)
			Expect(err).ToNot(HaveOccurred())

			Expect(config.MaxRequestHeaderBytes).To(Equal(10))
		})

		It("sets prometheus endpoint config", func() {
			cfg, err := DefaultConfig()
			Expect(err).ToNot(HaveOccurred())

			var b = []byte(`
prometheus:
  enabled: true
  port: 1234
  cert_path: /some-cert-path
  key_path: /some-key-path
  ca_path: /some-ca-path
`)

			err = config.Initialize(b)
			Expect(err).ToNot(HaveOccurred())

			Expect(config.Prometheus.Enabled).To(BeTrue())
			Expect(config.Prometheus.Port).To(Equal(uint16(1234)))
			Expect(config.Prometheus.CertPath).To(Equal("/some-cert-path"))
			Expect(config.Prometheus.KeyPath).To(Equal("/some-key-path"))
			Expect(config.Prometheus.CAPath).To(Equal("/some-ca-path"))
			Expect(config.Prometheus.Meters).To(Equal(cfg.Prometheus.Meters))
		})

		It("sets prometheus histogram buckets config", func() {
			var b = []byte(`
prometheus:
  meters:
    route_lookup_time_histogram_buckets: [0, 100, 10000]
    route_registration_latency_histogram_buckets: [-10, 0, 10]
    gorouter_time_histogram_buckets: [1,2,4]
    routing_response_latency_histogram_buckets: [0.1, 0.5, 1]
`)

			err := config.Initialize(b)
			Expect(err).ToNot(HaveOccurred())

			Expect(config.Prometheus.Meters.RouteLookupTimeHistogramBuckets).To(Equal([]float64{0, 100, 10000}))
			Expect(config.Prometheus.Meters.RouteRegistrationLatencyHistogramBuckets).To(Equal([]float64{-10, 0, 10}))
			Expect(config.Prometheus.Meters.GorouterTimeHistogramBuckets).To(Equal([]float64{1, 2, 4}))
			Expect(config.Prometheus.Meters.RoutingResponseLatencyHistogramBuckets).To(Equal([]float64{0.1, 0.5, 1}))
		})

		It("defaults frontend idle timeout to 900", func() {
			Expect(config.FrontendIdleTimeout).To(Equal(900 * time.Second))
		})

		It("sets frontend idle timeout", func() {
			var b = []byte(`
frontend_idle_timeout: 5s
`)

			err := config.Initialize(b)
			Expect(err).ToNot(HaveOccurred())

			Expect(config.FrontendIdleTimeout).To(Equal(5 * time.Second))
		})

		It("sets read header timeout", func() {
			var b = []byte(`
read_header_timeout: 30s
`)

			err := config.Initialize(b)
			Expect(err).ToNot(HaveOccurred())
			Expect(config.ReadHeaderTimeout).To(Equal(30 * time.Second))
		})

		It("sets endpoint timeout", func() {
			var b = []byte(`
endpoint_timeout: 10s
`)

			err := config.Initialize(b)
			Expect(err).ToNot(HaveOccurred())

			Expect(config.EndpointTimeout).To(Equal(10 * time.Second))
		})

		It("sets endpoint dial timeout", func() {
			var b = []byte(`
endpoint_dial_timeout: 6s
`)

			err := config.Initialize(b)
			Expect(err).ToNot(HaveOccurred())

			Expect(config.EndpointDialTimeout).To(Equal(6 * time.Second))
		})

		It("sets websocket dial timeout", func() {
			var b = []byte(`
websocket_dial_timeout: 6s
`)

			err := config.Initialize(b)
			Expect(err).ToNot(HaveOccurred())

			Expect(config.WebsocketDialTimeout).To(Equal(6 * time.Second))
		})

		It("defaults websocket dial timeout to endpoint dial timeout if not set", func() {
			b := createYMLSnippet(cfgForSnippet)
			b = append(b, []byte(`
endpoint_dial_timeout: 6s
`)...)
			err := config.Initialize(b)
			Expect(err).ToNot(HaveOccurred())

			err = config.Process()
			Expect(err).ToNot(HaveOccurred())

			Expect(config.EndpointDialTimeout).To(Equal(6 * time.Second))
			Expect(config.WebsocketDialTimeout).To(Equal(6 * time.Second))
		})

		It("defaults keep alive probe interval to 1 second", func() {
			Expect(config.FrontendIdleTimeout).To(Equal(900 * time.Second))
			Expect(config.EndpointKeepAliveProbeInterval).To(Equal(1 * time.Second))
		})

		It("sets keep alive probe interval", func() {
			var b = []byte(`
endpoint_keep_alive_probe_interval: 500ms
`)

			err := config.Initialize(b)
			Expect(err).ToNot(HaveOccurred())

			Expect(config.EndpointKeepAliveProbeInterval).To(Equal(500 * time.Millisecond))
		})

		Context("NATS Config", func() {
			It("handles basic nats config", func() {
				var b = []byte(`
nats:
  user: user
  pass: pass
  hosts:
  - hostname: remotehost
    port: 4223
`)
				err := config.Initialize(b)
				Expect(err).ToNot(HaveOccurred())

				Expect(config.Nats.User).To(Equal("user"))
				Expect(config.Nats.Pass).To(Equal("pass"))
				Expect(config.Nats.Hosts).To(HaveLen(1))
				Expect(config.Nats.Hosts[0].Hostname).To(Equal("remotehost"))
				Expect(config.Nats.Hosts[0].Port).To(Equal(uint16(4223)))
			})

			Context("when TLSEnabled is set to true", func() {
				var (
					err           error
					configSnippet *Config
					caCert        tls.Certificate
					clientPair    tls.Certificate
				)

				createYMLSnippet := func(snippet *Config) []byte {
					cfgBytes, err := yaml.Marshal(snippet)
					Expect(err).ToNot(HaveOccurred())
					return cfgBytes
				}

				BeforeEach(func() {
					caCertChain := test_util.CreateSignedCertWithRootCA(test_util.CertNames{SANs: test_util.SubjectAltNames{DNS: "spinach.com"}})
					clientKeyPEM, clientCertPEM := test_util.CreateKeyPair("potato.com")

					caCert, err = tls.X509KeyPair(append(caCertChain.CertPEM, caCertChain.CACertPEM...), caCertChain.PrivKeyPEM)
					Expect(err).ToNot(HaveOccurred())
					clientPair, err = tls.X509KeyPair(clientCertPEM, clientKeyPEM)
					Expect(err).ToNot(HaveOccurred())

					configSnippet = &Config{
						Nats: NatsConfig{
							TLSEnabled: true,
							CACerts:    fmt.Sprintf("%s%s", caCertChain.CertPEM, caCertChain.CACertPEM),
							TLSPem: TLSPem{
								CertChain:  string(clientCertPEM),
								PrivateKey: string(clientKeyPEM),
							},
						},
						Status: StatusConfig{
							TLS: StatusTLSConfig{
								Port:        8443,
								Certificate: string(clientCertPEM),
								Key:         string(clientKeyPEM),
							},
						},
					}
				})

				It("configures TLS", func() {
					configBytes := createYMLSnippet(configSnippet)
					err = config.Initialize(configBytes)
					Expect(err).NotTo(HaveOccurred())
					err = config.Process()
					Expect(err).NotTo(HaveOccurred())

					Expect(config.Nats.CAPool).ToNot(BeNil())
					//lint:ignore SA1019 - ignoring tlsCert.RootCAs.Subjects is deprecated ERR because cert does not come from SystemCertPool.
					poolSubjects := config.Nats.CAPool.Subjects()
					parsedCert, err := x509.ParseCertificate(caCert.Certificate[0])
					Expect(err).NotTo(HaveOccurred())
					expectedSubject := parsedCert.RawSubject

					Expect(poolSubjects).To(ContainElement(expectedSubject))
					Expect(config.Nats.ClientAuthCertificate).To(Equal(clientPair))
				})
			})
		})

		Context("Suspend Pruning option", func() {
			It("sets default suspend_pruning_if_nats_unavailable", func() {
				Expect(config.SuspendPruningIfNatsUnavailable).To(BeFalse())
			})

			It("sets default suspend_pruning_if_nats_unavailable", func() {
				var b = []byte(`
suspend_pruning_if_nats_unavailable: true
`)
				err := config.Initialize(b)
				Expect(err).ToNot(HaveOccurred())

				Expect(config.SuspendPruningIfNatsUnavailable).To(BeTrue())
			})
		})

		It("sets default logging configs", func() {
			Expect(config.Logging.Syslog).To(Equal(""))
			Expect(config.Logging.Level).To(Equal("debug"))
			Expect(config.Logging.LoggregatorEnabled).To(Equal(false))
			Expect(config.Logging.DisableLogForwardedFor).To(Equal(false))
			Expect(config.Logging.DisableLogSourceIP).To(Equal(false))
			Expect(config.Logging.RedactQueryParams).To(Equal("none"))
			Expect(config.Logging.Format.Timestamp).To(Equal("unix-epoch"))
		})

		It("sets default access log config", func() {
			// access entries not present in config
			Expect(config.AccessLog.File).To(Equal(""))
			Expect(config.AccessLog.EnableStreaming).To(BeFalse())
		})

		It("sets default sharding mode config", func() {
			Expect(config.RoutingTableShardingMode).To(Equal("all"))
		})

		It("sets the load_balancer_healthy_threshold configuration", func() {
			var b = []byte(`
load_balancer_healthy_threshold: 20s
`)
			err := config.Initialize(b)
			Expect(err).ToNot(HaveOccurred())

			Expect(config.LoadBalancerHealthyThreshold).To(Equal(20 * time.Second))
		})

		It("sets access log config to file only", func() {
			var b = []byte(`
access_log:
  file: "/var/vcap/sys/log/gorouter/access.log"
`)
			err := config.Initialize(b)
			Expect(err).ToNot(HaveOccurred())

			Expect(config.AccessLog.File).To(Equal("/var/vcap/sys/log/gorouter/access.log"))
			Expect(config.AccessLog.EnableStreaming).To(BeFalse())
		})

		It("sets access log config to file and no streaming", func() {
			var b = []byte(`
access_log:
  file: "/var/vcap/sys/log/gorouter/access.log"
  enable_streaming: false
`)
			err := config.Initialize(b)
			Expect(err).ToNot(HaveOccurred())

			Expect(config.AccessLog.File).To(Equal("/var/vcap/sys/log/gorouter/access.log"))
			Expect(config.AccessLog.EnableStreaming).To(BeFalse())
		})

		It("sets access log config to file and streaming", func() {
			var b = []byte(`
access_log:
  file: "/var/vcap/sys/log/gorouter/access.log"
  enable_streaming: true
`)
			err := config.Initialize(b)
			Expect(err).ToNot(HaveOccurred())

			Expect(config.AccessLog.File).To(Equal("/var/vcap/sys/log/gorouter/access.log"))
			Expect(config.AccessLog.EnableStreaming).To(BeTrue())
		})

		It("sets logging config", func() {
			var b = []byte(`
logging:
  syslog: syslog
  level: debug2
  loggregator_enabled: true
  format:
    timestamp: just_log_something
`)
			err := config.Initialize(b)
			Expect(err).ToNot(HaveOccurred())

			Expect(config.Logging.Syslog).To(Equal("syslog"))
			Expect(config.Logging.Level).To(Equal("debug2"))
			Expect(config.Logging.LoggregatorEnabled).To(Equal(true))
			Expect(config.Logging.JobName).To(Equal("gorouter"))
			Expect(config.Logging.Format.Timestamp).To(Equal("just_log_something"))
		})

		It("sets the rest of config", func() {
			var b = []byte(`
port: 8082
index: 1
go_max_procs: 2
trace_key: "foo"
access_log:
    file: "/tmp/access_log"
ssl_port: 4443
enable_ssl: true
isolation_segments: [test-iso-seg-1, test-iso-seg-2]
routing_table_sharding_mode: "segments"
`)

			err := config.Initialize(b)
			Expect(err).ToNot(HaveOccurred())

			Expect(config.Port).To(Equal(uint16(8082)))
			Expect(config.Index).To(Equal(uint(1)))
			Expect(config.GoMaxProcs).To(Equal(2))
			Expect(config.TraceKey).To(Equal("foo"))
			Expect(config.AccessLog.File).To(Equal("/tmp/access_log"))
			Expect(config.AccessLog.EnableStreaming).To(BeFalse())
			Expect(config.EnableSSL).To(Equal(true))
			Expect(config.SSLPort).To(Equal(uint16(4443)))
			Expect(config.RouteServiceRecommendHttps).To(BeFalse())
			Expect(config.IsolationSegments).To(ConsistOf("test-iso-seg-1", "test-iso-seg-2"))
			Expect(config.RoutingTableShardingMode).To(Equal("segments"))
		})

		Describe("routing API configuration", func() {
			Context("when the routing API configuration is set", func() {
				var (
					cfg       *Config
					certChain test_util.CertChain
				)

				BeforeEach(func() {
					certChain = test_util.CreateSignedCertWithRootCA(test_util.CertNames{SANs: test_util.SubjectAltNames{DNS: "spinach.com"}})
					cfg = &Config{
						RoutingApi: RoutingApiConfig{
							Uri:          "http://bob.url/token",
							Port:         1234,
							AuthDisabled: true,
							TLSPem: TLSPem{
								CertChain:  string(certChain.CertPEM),
								PrivateKey: string(certChain.PrivKeyPEM),
							},
							CACerts: string(certChain.CACertPEM),
						},
						Status: StatusConfig{
							TLS: StatusTLSConfig{
								Port:        8443,
								Certificate: string(certChain.CertPEM),
								Key:         string(certChain.PrivKeyPEM),
							},
						},
					}
				})

				Context("when the config is valid", func() {
					BeforeEach(func() {
						b, err := yaml.Marshal(cfg)
						Expect(err).ToNot(HaveOccurred())

						err = config.Initialize(b)
						Expect(err).ToNot(HaveOccurred())

						err = config.Process()
						Expect(err).ToNot(HaveOccurred())
					})

					It("pulls out the values into Go objects that we can use", func() {
						Expect(config.RoutingApi.Uri).To(Equal("http://bob.url/token"))
						Expect(config.RoutingApi.Port).To(Equal(1234))
						Expect(config.RoutingApi.AuthDisabled).To(BeTrue())

						//lint:ignore SA1019 - ignoring tlsCert.RootCAs.Subjects is deprecated ERR because cert does not come from SystemCertPool.
						Expect(config.RoutingApi.CAPool.Subjects()).To(ContainElement(certChain.CACert.RawSubject))
						Expect(config.RoutingApi.ClientAuthCertificate).To(Equal(certChain.AsTLSConfig().Certificates[0]))
					})

					It("reports that the routing API is enabled", func() {
						Expect(config.RoutingApiEnabled()).To(BeTrue())
					})
				})

				Context("when the routing api config is invalid", func() {
					processConfig := func(malformedConfig *Config) error {
						b, err := yaml.Marshal(malformedConfig)
						Expect(err).ToNot(HaveOccurred())

						err = config.Initialize(b)
						Expect(err).ToNot(HaveOccurred())

						return config.Process()
					}

					It("returns an error if the certificate is malformed", func() {
						cfg.RoutingApi.CertChain = "ya ya ya ya"
						Expect(processConfig(cfg)).ToNot(Succeed())
					})

					It("returns an error if the private key is malformed", func() {
						cfg.RoutingApi.PrivateKey = "ya ya ya ya"
						Expect(processConfig(cfg)).ToNot(Succeed())
					})

					It("returns an error if the ca is malformed", func() {
						cfg.RoutingApi.CACerts = "ya ya ya ya"
						Expect(processConfig(cfg)).ToNot(Succeed())
					})
				})
			})

			Context("when the routing API configuration is not set", func() {
				It("reports that the routing API is disabled", func() {
					Expect(config.RoutingApiEnabled()).To(BeFalse())
				})
			})
		})

		It("sets the OAuth config", func() {
			var b = []byte(`
oauth:
  token_endpoint: uaa.internal
  port: 8443
  skip_ssl_validation: true
  client_name: client-name
  client_secret: client-secret
  ca_certs: ca-cert
`)

			err := config.Initialize(b)
			Expect(err).ToNot(HaveOccurred())

			Expect(config.OAuth.TokenEndpoint).To(Equal("uaa.internal"))
			Expect(config.OAuth.Port).To(Equal(uint16(8443)))
			Expect(config.OAuth.SkipSSLValidation).To(Equal(true))
			Expect(config.OAuth.ClientName).To(Equal("client-name"))
			Expect(config.OAuth.ClientSecret).To(Equal("client-secret"))
			Expect(config.OAuth.CACerts).To(Equal("ca-cert"))
		})

		It("sets the SkipSSLValidation config", func() {
			var b = []byte(`
skip_ssl_validation: true
`)
			err := config.Initialize(b)
			Expect(err).ToNot(HaveOccurred())

			Expect(config.SkipSSLValidation).To(BeTrue())
		})

		It("defaults the SkipSSLValidation config to false", func() {
			var b = []byte(``)
			err := config.Initialize(b)
			Expect(err).ToNot(HaveOccurred())

			Expect(config.SkipSSLValidation).To(BeFalse())
		})

		It("sets the route service recommend https config", func() {
			var b = []byte(`
route_services_recommend_https: true
`)
			err := config.Initialize(b)
			Expect(err).ToNot(HaveOccurred())

			Expect(config.RouteServiceRecommendHttps).To(BeTrue())
		})

		It("sets the route service secret config", func() {
			var b = []byte(`
route_services_secret: super-route-service-secret
`)
			err := config.Initialize(b)
			Expect(err).ToNot(HaveOccurred())

			Expect(config.RouteServiceSecret).To(Equal("super-route-service-secret"))
		})

		It("sets the route service secret decrypt only config", func() {
			var b = []byte(`
route_services_secret_decrypt_only: decrypt-only-super-route-service-secret
`)
			err := config.Initialize(b)
			Expect(err).ToNot(HaveOccurred())

			Expect(config.RouteServiceSecretPrev).To(Equal("decrypt-only-super-route-service-secret"))
		})

		It("sets the token fetcher config", func() {
			var b = []byte(`
token_fetcher_max_retries: 4
token_fetcher_retry_interval: 10s
token_fetcher_expiration_buffer_time: 40
`)
			err := config.Initialize(b)
			Expect(err).ToNot(HaveOccurred())

			Expect(config.TokenFetcherMaxRetries).To(Equal(uint32(4)))
			Expect(config.TokenFetcherRetryInterval).To(Equal(10 * time.Second))
			Expect(config.TokenFetcherExpirationBufferTimeInSeconds).To(Equal(int64(40)))
		})

		It("default the token fetcher config", func() {
			var b = []byte(``)
			err := config.Initialize(b)
			Expect(err).ToNot(HaveOccurred())

			Expect(config.TokenFetcherMaxRetries).To(Equal(uint32(3)))
			Expect(config.TokenFetcherRetryInterval).To(Equal(5 * time.Second))
			Expect(config.TokenFetcherExpirationBufferTimeInSeconds).To(Equal(int64(30)))
		})

		It("sets proxy protocol", func() {
			var b = []byte(`
enable_proxy: true
`)

			err := config.Initialize(b)
			Expect(err).ToNot(HaveOccurred())

			Expect(config.EnablePROXY).To(Equal(true))
		})

		It("sets the healthcheck User-Agent", func() {
			var b = []byte("healthcheck_user_agent: ELB-HealthChecker/1.0")
			err := config.Initialize(b)
			Expect(err).ToNot(HaveOccurred())

			Expect(config.HealthCheckUserAgent).To(Equal("ELB-HealthChecker/1.0"))
		})

		It("defaults the healthcheck User-Agent", func() {
			var b = []byte(``)
			err := config.Initialize(b)
			Expect(err).ToNot(HaveOccurred())

			Expect(config.HealthCheckUserAgent).To(Equal("HTTP-Monitor/1.1"))
		})

		It("sets Tracing.EnableZipkin", func() {
			var b = []byte("tracing:\n  enable_zipkin: true")
			err := config.Initialize(b)
			Expect(err).ToNot(HaveOccurred())

			Expect(config.Tracing.EnableZipkin).To(BeTrue())

		})

		It("defaults Tracing.EnableZipkin", func() {
			var b = []byte(``)
			err := config.Initialize(b)
			Expect(err).ToNot(HaveOccurred())

			Expect(config.Tracing.EnableZipkin).To(BeFalse())
		})

		It("sets Tracing.EnableW3C", func() {
			var b = []byte("tracing:\n  enable_w3c: true")
			err := config.Initialize(b)
			Expect(err).ToNot(HaveOccurred())

			Expect(config.Tracing.EnableW3C).To(BeTrue())

		})

		It("defaults Tracing.W3C", func() {
			var b = []byte(``)
			err := config.Initialize(b)
			Expect(err).ToNot(HaveOccurred())

			Expect(config.Tracing.EnableW3C).To(BeFalse())
		})

		It("sets Tracing.W3CTenantID", func() {
			var b = []byte("tracing:\n  w3c_tenant_id: cf")
			err := config.Initialize(b)
			Expect(err).ToNot(HaveOccurred())

			Expect(config.Tracing.W3CTenantID).To(Equal("cf"))

		})

		It("defaults Tracing.W3CTenantID", func() {
			var b = []byte(``)
			err := config.Initialize(b)
			Expect(err).ToNot(HaveOccurred())

			Expect(config.Tracing.W3CTenantID).To(BeEmpty())
		})

		It("sets the proxy forwarded proto header", func() {
			var b = []byte("force_forwarded_proto_https: true")
			config.Initialize(b)
			Expect(config.ForceForwardedProtoHttps).To(Equal(true))
		})

		It("defaults DisableKeepAlives to true", func() {
			var b = []byte("")
			err := config.Initialize(b)
			Expect(err).ToNot(HaveOccurred())

			Expect(config.DisableKeepAlives).To(BeTrue())
		})

		It("defaults MaxIdleConns to 100", func() {
			var b = []byte("")
			err := config.Initialize(b)
			Expect(err).ToNot(HaveOccurred())

			Expect(config.MaxIdleConns).To(Equal(100))
		})

		It("defaults MaxConns to 0", func() {
			var b = []byte("")
			err := config.Initialize(b)
			Expect(err).ToNot(HaveOccurred())

			Expect(config.Backends.MaxConns).To(Equal(int64(0)))
		})

		It("sets MaxConns", func() {
			var b = []byte(`
backends:
  max_conns: 10`)
			err := config.Initialize(b)
			Expect(err).ToNot(HaveOccurred())

			Expect(config.Backends.MaxConns).To(Equal(int64(10)))
		})

		It("defaults MaxIdleConnsPerHost to 2", func() {
			var b = []byte("")
			err := config.Initialize(b)
			Expect(err).ToNot(HaveOccurred())

			Expect(config.MaxIdleConnsPerHost).To(Equal(2))
		})

		It("sets DisableKeepAlives", func() {
			var b = []byte("disable_keep_alives: false")
			err := config.Initialize(b)
			Expect(err).ToNot(HaveOccurred())

			Expect(config.DisableKeepAlives).To(BeFalse())
		})

		It("sets KeepAlive100ContinueRequests", func() {
			var b = []byte("keep_alive_100_continue_requests: true")
			err := config.Initialize(b)
			Expect(err).ToNot(HaveOccurred())

			Expect(config.KeepAlive100ContinueRequests).To(BeTrue())
		})

		It("sets MaxIdleConns", func() {
			var b = []byte("max_idle_conns: 200")
			err := config.Initialize(b)
			Expect(err).ToNot(HaveOccurred())

			Expect(config.MaxIdleConns).To(Equal(200))
		})

		It("sets MaxIdleConnsPerHost", func() {
			var b = []byte("max_idle_conns_per_host: 10")
			err := config.Initialize(b)
			Expect(err).ToNot(HaveOccurred())

			Expect(config.MaxIdleConnsPerHost).To(Equal(10))
		})

		It("defaults DisableHTTP to false", func() {
			Expect(config.DisableHTTP).To(BeFalse())
		})

		It("sets DisableHTTP", func() {
			var b = []byte("disable_http: true")
			err := config.Initialize(b)
			Expect(err).ToNot(HaveOccurred())
			Expect(config.DisableHTTP).To(BeTrue())
		})

		It("defaults HTMLErrorTemplateFile to empty", func() {
			Expect(config.HTMLErrorTemplateFile).To(Equal(""))
		})

		It("sets HTMLErrorTemplateFile", func() {
			var b = []byte(`html_error_template_file: "/path/to/file"`)
			err := config.Initialize(b)
			Expect(err).ToNot(HaveOccurred())
			Expect(config.HTMLErrorTemplateFile).To(Equal("/path/to/file"))
		})

		It("defaults PerRequestMetricsReporting to true", func() {
			Expect(config.PerRequestMetricsReporting).To(Equal(true))
		})

		It("sets PerRequestMetricsReporting", func() {
			var b = []byte(`per_request_metrics_reporting: false`)
			err := config.Initialize(b)
			Expect(err).ToNot(HaveOccurred())
			Expect(config.PerRequestMetricsReporting).To(BeFalse())
		})

		It("defaults EnableEnvelopeV1Metrics to true", func() {
			Expect(config.EnableEnvelopeV1Metrics).To(Equal(true))
		})

		It("sets EnableEnvelopeV1Metrics", func() {
			var b = []byte(`enable_envelope_v1_metrics: false`)
			err := config.Initialize(b)
			Expect(err).ToNot(HaveOccurred())
			Expect(config.EnableEnvelopeV1Metrics).To(BeFalse())
		})

		It("defaults SendHttpStartStopServerEvent to true", func() {
			Expect(config.SendHttpStartStopServerEvent).To(Equal(true))
		})

		It("sets SendHttpStartStopServerEvent", func() {
			var b = []byte(`send_http_start_stop_server_event: false`)
			err := config.Initialize(b)
			Expect(err).ToNot(HaveOccurred())
			Expect(config.SendHttpStartStopServerEvent).To(BeFalse())
		})

		It("defaults SendHttpStartStopClientEvent to true", func() {
			Expect(config.SendHttpStartStopClientEvent).To(Equal(true))
		})

		It("sets SendHttpStartStopClientEvent", func() {
			var b = []byte(`send_http_start_stop_client_event: false`)
			err := config.Initialize(b)
			Expect(err).ToNot(HaveOccurred())
			Expect(config.SendHttpStartStopClientEvent).To(BeFalse())
		})

	})

	Describe("Process", func() {
		It("converts intervals to durations", func() {
			b := createYMLSnippet(cfgForSnippet)
			b = append(b, []byte(`
publish_start_message_interval: 1s
prune_stale_droplets_interval: 2s
droplet_stale_threshold: 30s
publish_active_apps_interval: 4s
start_response_delay_interval: 15s
secure_cookies: true
token_fetcher_retry_interval: 10s
`)...)

			err := config.Initialize(b)
			Expect(err).ToNot(HaveOccurred())

			Expect(config.Process()).To(Succeed())

			Expect(config.PublishStartMessageInterval).To(Equal(1 * time.Second))
			Expect(config.PruneStaleDropletsInterval).To(Equal(2 * time.Second))
			Expect(config.DropletStaleThreshold).To(Equal(30 * time.Second))
			Expect(config.PublishActiveAppsInterval).To(Equal(4 * time.Second))
			Expect(config.StartResponseDelayInterval).To(Equal(15 * time.Second))
			Expect(config.TokenFetcherRetryInterval).To(Equal(10 * time.Second))
			Expect(config.NatsClientPingInterval).To(Equal(20 * time.Second))
			Expect(config.SecureCookies).To(BeTrue())
		})

		Context("When LoadBalancerHealthyThreshold is provided", func() {
			var b []byte
			BeforeEach(func() {
				b = createYMLSnippet(cfgForSnippet)
			})
			It("returns a meaningful error when an invalid duration string is given", func() {
				b = append(b, []byte(`
load_balancer_healthy_threshold: -5s
`)...)
				err := config.Initialize(b)
				Expect(err).ToNot(HaveOccurred())

				Expect(config.Process()).To(MatchError("Invalid load balancer healthy threshold: -5s"))
			})

			It("fails to initialize a non time string", func() {
				b = append(b, []byte(`
load_balancer_healthy_threshold: test
`)...)

				Expect(config.Initialize(b)).To(MatchError(ContainSubstring("cannot unmarshal")))
			})

			It("process the string into a valid duration", func() {
				b = append(b, []byte(`
load_balancer_healthy_threshold: 10s
`)...)
				err := config.Initialize(b)
				Expect(err).ToNot(HaveOccurred())
			})
		})

		It("converts extra headers to log into a map", func() {
			cfgForSnippet.ExtraHeadersToLog = []string{"x-b3-trace-id", "something", "something"}
			err := config.Initialize(createYMLSnippet(cfgForSnippet))
			Expect(err).ToNot(HaveOccurred())
			Expect(config.Process()).To(Succeed())

			Expect(config.ExtraHeadersToLog).To(ContainElement("something"))
			Expect(config.ExtraHeadersToLog).To(ContainElement("x-b3-trace-id"))
		})

		Describe("StickySessionCookieNames", func() {
			BeforeEach(func() {
				cfgForSnippet.StickySessionCookieNames = StringSet{"someName": struct{}{}, "anotherName": struct{}{}}
			})
			It("converts the provided list to a set of StickySessionCookieNames", func() {

				err := config.Initialize(createYMLSnippet(cfgForSnippet))
				Expect(err).ToNot(HaveOccurred())
				Expect(config.Process()).To(Succeed())

				Expect(config.StickySessionCookieNames).To(HaveKey("someName"))
				Expect(config.StickySessionCookieNames).To(HaveKey("anotherName"))
			})
		})

		Context("When secure cookies is set to false", func() {
			BeforeEach(func() {
				cfgForSnippet.SecureCookies = false
			})
			It("set DropletStaleThreshold equal to StartResponseDelayInterval", func() {

				err := config.Initialize(createYMLSnippet(cfgForSnippet))
				Expect(err).ToNot(HaveOccurred())

				Expect(config.Process()).To(Succeed())

				Expect(config.SecureCookies).To(BeFalse())
			})

		})

		Describe("NatsServers", func() {
			BeforeEach(func() {
				cfgForSnippet.Nats = NatsConfig{
					User: "user",
					Pass: "pass",
					Hosts: []NatsHost{{
						Hostname: "remotehost",
						Port:     4223,
					}, {
						Hostname: "remotehost2",
						Port:     4224,
					}},
				}
			})

			It("returns a slice of the configured NATS servers", func() {
				err := config.Initialize(createYMLSnippet(cfgForSnippet))
				Expect(err).ToNot(HaveOccurred())

				natsServers := config.NatsServers()
				Expect(natsServers[0]).To(Equal("nats://user:pass@remotehost:4223"))
				Expect(natsServers[1]).To(Equal("nats://user:pass@remotehost2:4224"))
			})
		})

		Describe("RouteServiceEnabled", func() {
			Context("when the route service secrets is not configured", func() {
				BeforeEach(func() {
					cfgForSnippet.RouteServiceSecret = ""
					cfgForSnippet.RouteServiceSecretPrev = ""
				})
				It("disables route services", func() {
					err := config.Initialize(createYMLSnippet(cfgForSnippet))
					Expect(err).ToNot(HaveOccurred())
					Expect(config.Process()).To(Succeed())
					Expect(config.RouteServiceEnabled).To(BeFalse())
				})
			})

			Context("when the route service secret is configured", func() {
				Context("when the route service secret is set", func() {
					BeforeEach(func() {
						cfgForSnippet.RouteServiceSecret = "my-route-service-secret"
						err := config.Initialize(createYMLSnippet(cfgForSnippet))
						Expect(err).ToNot(HaveOccurred())
						Expect(config.Process()).To(Succeed())
					})

					It("enables route services", func() {
						Expect(config.RouteServiceEnabled).To(BeTrue())
					})

					It("sets route service secret", func() {
						Expect(config.RouteServiceSecret).To(Equal("my-route-service-secret"))
					})
				})

				Context("when the route service secret and the decrypt only route service secret are set", func() {
					BeforeEach(func() {
						cfgForSnippet.RouteServiceSecret = "my-route-service-secret"
						cfgForSnippet.RouteServiceSecretPrev = "my-decrypt-only-route-service-secret"
						err := config.Initialize(createYMLSnippet(cfgForSnippet))
						Expect(err).ToNot(HaveOccurred())
						Expect(config.Process()).To(Succeed())
					})

					It("enables route services", func() {
						Expect(config.RouteServiceEnabled).To(BeTrue())
					})

					It("sets route service secret", func() {
						Expect(config.RouteServiceSecret).To(Equal("my-route-service-secret"))
					})

					It("sets previous route service secret", func() {
						Expect(config.RouteServiceSecretPrev).To(Equal("my-decrypt-only-route-service-secret"))
					})
				})

				Context("when only the decrypt only route service secret is set", func() {
					BeforeEach(func() {
						cfgForSnippet.RouteServiceSecretPrev = "1PfbARmvIn6cgyKorA1rqR2d34rBOo+z3qJGz17pi8Y="
						err := config.Initialize(createYMLSnippet(cfgForSnippet))
						Expect(err).ToNot(HaveOccurred())
						Expect(config.Process()).To(Succeed())
					})

					It("does NOT enabled route services", func() {
						Expect(config.RouteServiceEnabled).To(BeFalse())
					})
				})

				Context("when the route service server port is not set", func() {
					It("uses the default route service server port", func() {
						Expect(config.RouteServicesServerPort).To(Equal(uint16(7070)))
					})
				})

				Context("when the route service server port is set", func() {
					BeforeEach(func() {
						cfgForSnippet.RouteServicesServerPort = 7878
						err := config.Initialize(createYMLSnippet(cfgForSnippet))
						Expect(err).ToNot(HaveOccurred())
						Expect(config.Process()).To(Succeed())
					})

					It("uses the route service server port", func() {
						Expect(config.RouteServicesServerPort).To(Equal(uint16(7878)))
					})
				})
			})
		})

		Context("When EnableSSL is set to true", func() {
			var (
				expectedCAPEMs           []string
				expectedSSLCertificates  []tls.Certificate
				keyPEM1, certPEM1        []byte
				rootRSAPEM, rootECDSAPEM []byte
				expectedTLSPEMs          []TLSPem
				configSnippet            *Config
			)

			createYMLSnippet := func(snippet *Config) []byte {
				cfgBytes, err := yaml.Marshal(snippet)
				Expect(err).ToNot(HaveOccurred())
				return cfgBytes
			}

			BeforeEach(func() {
				certChain := test_util.CreateSignedCertWithRootCA(test_util.CertNames{SANs: test_util.SubjectAltNames{DNS: "spinach.com"}})
				keyPEM1, certPEM1 = test_util.CreateKeyPair("potato.com")
				keyPEM2, certPEM2 := test_util.CreateKeyPair("potato2.com")

				tlsPem1 := TLSPem{
					CertChain:  string(certPEM1),
					PrivateKey: string(keyPEM1),
				}
				tlsPem2 := TLSPem{
					CertChain:  string(certPEM2),
					PrivateKey: string(keyPEM2),
				}
				tlsPemCertChain := TLSPem{
					CertChain:  fmt.Sprintf("%s%s", certChain.CertPEM, certChain.CACertPEM),
					PrivateKey: string(certChain.PrivKeyPEM),
				}
				expectedTLSPEMs = []TLSPem{tlsPem1, tlsPem2, tlsPemCertChain}

				cert1, err := tls.X509KeyPair(certPEM1, keyPEM1)
				Expect(err).ToNot(HaveOccurred())
				cert2, err := tls.X509KeyPair(certPEM2, keyPEM2)
				Expect(err).ToNot(HaveOccurred())
				cert3, err := tls.X509KeyPair(append(certChain.CertPEM, certChain.CACertPEM...), certChain.PrivKeyPEM)
				Expect(err).ToNot(HaveOccurred())

				expectedSSLCertificates = []tls.Certificate{cert1, cert2, cert3}

				_, rootRSAPEM = test_util.CreateKeyPair("carrots.com")
				_, rootECDSAPEM = test_util.CreateECKeyPair("carrots.net")

				expectedCAPEMs = []string{
					string(rootRSAPEM),
					string(rootECDSAPEM),
				}

				configSnippet = &Config{
					EnableSSL:                         true,
					MinTLSVersionString:               "TLSv1.0",
					ClientCertificateValidationString: "none",
					CipherString:                      "ECDHE-RSA-AES128-GCM-SHA256",
					TLSPEM:                            expectedTLSPEMs,
					Status: StatusConfig{
						TLS: StatusTLSConfig{
							Port:        8443,
							Certificate: string(certPEM1),
							Key:         string(keyPEM1),
						},
					},
				}

			})

			Context("when valid value for client_cert_validation is set", func() {

				DescribeTable("client certificate validation",
					func(clientCertValidation string, expectedAuthType tls.ClientAuthType) {
						configSnippet.ClientCertificateValidationString = clientCertValidation
						configBytes := createYMLSnippet(configSnippet)
						err := config.Initialize(configBytes)
						Expect(err).NotTo(HaveOccurred())
						Expect(config.Process()).To(Succeed())
						Expect(config.ClientCertificateValidation).To(Equal(expectedAuthType))
					},
					Entry("none", "none", tls.NoClientCert),
					Entry("request", "request", tls.VerifyClientCertIfGiven),
					Entry("require", "require", tls.RequireAndVerifyClientCert),
				)

				Context("when ClientCertificateValidation is invalid", func() {
					BeforeEach(func() {
						configSnippet.ClientCertificateValidationString = "meow"
					})
					It("returns a meaningful error", func() {
						configBytes := createYMLSnippet(configSnippet)
						err := config.Initialize(configBytes)
						Expect(err).NotTo(HaveOccurred())
						Expect(config.Process()).To(MatchError("router.client_cert_validation must be one of 'none', 'request' or 'require'."))
					})
				})

			})

			Context("when valid value for min_tls_version is set", func() {
				BeforeEach(func() {
					configSnippet.MinTLSVersionString = "TLSv1.1"
				})
				It("populates MinTLSVersion", func() {
					configBytes := createYMLSnippet(configSnippet)
					err := config.Initialize(configBytes)
					Expect(err).NotTo(HaveOccurred())
					Expect(config.Process()).To(Succeed())
					Expect(config.MinTLSVersion).To(Equal(uint16(tls.VersionTLS11)))
				})
			})
			Context("when invalid value for min_tls_version is set", func() {
				BeforeEach(func() {
					configSnippet.MinTLSVersionString = "fake-tls"
				})
				It("returns a meaningful error", func() {
					configBytes := createYMLSnippet(configSnippet)
					err := config.Initialize(configBytes)
					Expect(err).NotTo(HaveOccurred())
					Expect(config.Process()).To(MatchError(`router.min_tls_version should be one of "", "TLSv1.3", "TLSv1.2", "TLSv1.1", "TLSv1.0"`))
				})
			})
			Context("when min_tls_version is not set", func() {
				BeforeEach(func() {
					configSnippet.MinTLSVersionString = ""
				})
				It("sets the default to TLSv1.2", func() {
					configBytes := createYMLSnippet(configSnippet)
					err := config.Initialize(configBytes)
					Expect(err).NotTo(HaveOccurred())
					Expect(config.Process()).To(Succeed())
					Expect(config.MinTLSVersion).To(Equal(uint16(tls.VersionTLS12)))
				})
			})

			Context("when valid value for max_tls_version is set", func() {
				BeforeEach(func() {
					configSnippet.MaxTLSVersionString = "TLSv1.3"
				})
				It("populates MaxTLSVersion", func() {
					configBytes := createYMLSnippet(configSnippet)
					err := config.Initialize(configBytes)
					Expect(err).NotTo(HaveOccurred())
					Expect(config.Process()).To(Succeed())
					Expect(config.MaxTLSVersion).To(Equal(uint16(tls.VersionTLS13)))
				})
			})
			Context("when invalid value for max_tls_version is set", func() {
				BeforeEach(func() {
					configSnippet.MaxTLSVersionString = "fake-tls"
				})
				It("returns a meaningful error", func() {
					configBytes := createYMLSnippet(configSnippet)
					err := config.Initialize(configBytes)
					Expect(err).NotTo(HaveOccurred())
					Expect(config.Process()).To(MatchError(`router.max_tls_version should be one of "TLSv1.2" or "TLSv1.3"`))
				})
			})
			Context("when max_tls_version is not set", func() {
				BeforeEach(func() {
					configSnippet.MaxTLSVersionString = ""
				})
				It("sets the default to TLSv1.2", func() {
					configBytes := createYMLSnippet(configSnippet)
					err := config.Initialize(configBytes)
					Expect(err).NotTo(HaveOccurred())
					Expect(config.Process()).To(Succeed())
					Expect(config.MaxTLSVersion).To(Equal(uint16(tls.VersionTLS12)))
				})
			})

			Context("when a valid CACerts is provided", func() {
				BeforeEach(func() {
					configSnippet.CACerts = []string{
						string(rootRSAPEM), string(rootECDSAPEM),
					}
				})

				It("populates the CACerts and CAPool property", func() {
					configBytes := createYMLSnippet(configSnippet)
					err := config.Initialize(configBytes)
					Expect(err).ToNot(HaveOccurred())

					Expect(config.EnableSSL).To(Equal(true))
					Expect(config.Process()).To(Succeed())
					Expect(config.CACerts).To(Equal(expectedCAPEMs))

					for _, cert := range config.CACerts {
						certDER, _ := pem.Decode([]byte(cert))
						Expect(err).NotTo(HaveOccurred())
						c, err := x509.ParseCertificate(certDER.Bytes)
						Expect(err).NotTo(HaveOccurred())
						//lint:ignore SA1019 - ignoring tlsCert.RootCAs.Subjects is deprecated ERR because cert does not come from SystemCertPool.
						Expect(config.CAPool.Subjects()).To(ContainElement(c.RawSubject))
					}
				})
			})

			Context("when it is given a valid tls_pem value", func() {
				It("populates the TLSPEM field and generates the SSLCertificates", func() {
					configBytes := createYMLSnippet(configSnippet)
					err := config.Initialize(configBytes)
					Expect(err).ToNot(HaveOccurred())

					Expect(config.EnableSSL).To(Equal(true))

					Expect(config.Process()).To(Succeed())
					Expect(config.TLSPEM).To(ConsistOf(expectedTLSPEMs))

					Expect(config.SSLCertificates).To(ConsistOf(expectedSSLCertificates))
				})
			})

			Context("PEM with ECDSA cipher algorithm", func() {
				BeforeEach(func() {
					keyPEM, certPEM := test_util.CreateECKeyPair("parsnip.com")
					cert, err := tls.X509KeyPair(certPEM, keyPEM)
					Expect(err).ToNot(HaveOccurred())

					expectedTLSPEMs = []TLSPem{
						TLSPem{
							CertChain:  string(certPEM),
							PrivateKey: string(keyPEM),
						},
					}
					configSnippet.TLSPEM = expectedTLSPEMs

					expectedSSLCertificates = []tls.Certificate{cert}
				})

				It("supports ECDSA PEM block", func() {
					configBytes := createYMLSnippet(configSnippet)
					err := config.Initialize(configBytes)
					Expect(err).ToNot(HaveOccurred())

					Expect(config.EnableSSL).To(Equal(true))

					Expect(config.Process()).To(Succeed())
					Expect(config.TLSPEM).To(ConsistOf(expectedTLSPEMs))

					Expect(config.SSLCertificates).To(ConsistOf(expectedSSLCertificates))
				})
			})

			Context("when TLSPEM is missing", func() {
				BeforeEach(func() {
					configSnippet.TLSPEM = nil
				})
				It("fails to validate", func() {
					configBytes := createYMLSnippet(configSnippet)
					err := config.Initialize(configBytes)
					Expect(err).ToNot(HaveOccurred())
					Expect(config.Process()).To(MatchError("router.tls_pem must be provided if router.enable_ssl is set to true"))
				})
			})

			Context("when TLSPEM does not contain both key and cert", func() {

				BeforeEach(func() {
					keyPEM, _ := test_util.CreateECKeyPair("parsnip.com")
					tlsPEMArray := []TLSPem{TLSPem{PrivateKey: string(keyPEM)}}
					configSnippet.TLSPEM = tlsPEMArray
				})
				It("fails to validate", func() {
					configBytes := createYMLSnippet(configSnippet)
					err := config.Initialize(configBytes)
					Expect(err).ToNot(HaveOccurred())

					Expect(config.Process()).To(MatchError("Error parsing PEM blocks of router.tls_pem, missing cert or key."))
				})
			})

			Context("TLSPEM does not contain a supported type", func() {
				BeforeEach(func() {
					invalidPEMString := "-----BEGIN INVALID-----\ndGVzdA==\n-----END INVALID-----"
					invalidPEM := []byte(invalidPEMString)
					tlsPEMArray := []TLSPem{TLSPem{
						PrivateKey: string(keyPEM1),
						CertChain:  string(invalidPEM),
					}}
					configSnippet.TLSPEM = tlsPEMArray
				})

				It("fails to validate", func() {
					configBytes := createYMLSnippet(configSnippet)
					err := config.Initialize(configBytes)
					Expect(err).ToNot(HaveOccurred())

					Expect(config.Process()).To(MatchError(HavePrefix("Error loading key pair: ")))
				})
			})

			Context("when cipher suites are of openssl format", func() {
				BeforeEach(func() {
					configSnippet.CipherString = "RC4-SHA:DES-CBC3-SHA:AES128-SHA:AES256-SHA:AES128-GCM-SHA256:AES256-GCM-SHA384:ECDHE-ECDSA-RC4-SHA:ECDHE-ECDSA-AES128-SHA:ECDHE-ECDSA-AES256-SHA:ECDHE-RSA-RC4-SHA:ECDHE-RSA-DES-CBC3-SHA:ECDHE-RSA-AES128-SHA:ECDHE-RSA-AES256-SHA:ECDHE-RSA-AES128-GCM-SHA256:ECDHE-ECDSA-AES128-GCM-SHA256:ECDHE-RSA-AES256-GCM-SHA384:ECDHE-ECDSA-AES256-GCM-SHA384:AES128-SHA256:ECDHE-ECDSA-AES128-SHA256:ECDHE-RSA-AES128-SHA256:ECDHE-RSA-CHACHA20-POLY1305:ECDHE-ECDSA-CHACHA20-POLY1305"
				})
				It("constructs the proper array of cipher suites", func() {
					expectedSuites := []uint16{
						tls.TLS_RSA_WITH_RC4_128_SHA,
						tls.TLS_RSA_WITH_3DES_EDE_CBC_SHA,
						tls.TLS_RSA_WITH_AES_128_CBC_SHA,
						tls.TLS_RSA_WITH_AES_256_CBC_SHA,
						tls.TLS_RSA_WITH_AES_128_GCM_SHA256,
						tls.TLS_RSA_WITH_AES_256_GCM_SHA384,
						tls.TLS_ECDHE_ECDSA_WITH_RC4_128_SHA,
						tls.TLS_ECDHE_ECDSA_WITH_AES_128_CBC_SHA,
						tls.TLS_ECDHE_ECDSA_WITH_AES_256_CBC_SHA,
						tls.TLS_ECDHE_RSA_WITH_RC4_128_SHA,
						tls.TLS_ECDHE_RSA_WITH_3DES_EDE_CBC_SHA,
						tls.TLS_ECDHE_RSA_WITH_AES_128_CBC_SHA,
						tls.TLS_ECDHE_RSA_WITH_AES_256_CBC_SHA,
						tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,
						tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256,
						tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
						tls.TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384,
						tls.TLS_RSA_WITH_AES_128_CBC_SHA256,
						tls.TLS_ECDHE_ECDSA_WITH_AES_128_CBC_SHA256,
						tls.TLS_ECDHE_RSA_WITH_AES_128_CBC_SHA256,
						tls.TLS_ECDHE_RSA_WITH_CHACHA20_POLY1305,
						tls.TLS_ECDHE_ECDSA_WITH_CHACHA20_POLY1305,
					}

					configBytes := createYMLSnippet(configSnippet)
					err := config.Initialize(configBytes)
					Expect(err).ToNot(HaveOccurred())

					Expect(config.Process()).To(Succeed())

					Expect(config.CipherSuites).To(ConsistOf(expectedSuites))
				})
			})
			Context("when cipher suites are of RFC format", func() {
				BeforeEach(func() {
					configSnippet.CipherString = "TLS_RSA_WITH_RC4_128_SHA:TLS_RSA_WITH_3DES_EDE_CBC_SHA:TLS_RSA_WITH_AES_128_CBC_SHA:TLS_RSA_WITH_AES_256_CBC_SHA:TLS_RSA_WITH_AES_128_GCM_SHA256:TLS_RSA_WITH_AES_256_GCM_SHA384:TLS_ECDHE_ECDSA_WITH_RC4_128_SHA:TLS_ECDHE_ECDSA_WITH_AES_128_CBC_SHA:TLS_ECDHE_ECDSA_WITH_AES_256_CBC_SHA:TLS_ECDHE_RSA_WITH_RC4_128_SHA:TLS_ECDHE_RSA_WITH_3DES_EDE_CBC_SHA:TLS_ECDHE_RSA_WITH_AES_128_CBC_SHA:TLS_ECDHE_RSA_WITH_AES_256_CBC_SHA:TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256:TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256:TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384:TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384:TLS_RSA_WITH_AES_128_CBC_SHA256:TLS_ECDHE_ECDSA_WITH_AES_128_CBC_SHA256:TLS_ECDHE_RSA_WITH_AES_128_CBC_SHA256:TLS_ECDHE_RSA_WITH_CHACHA20_POLY1305:TLS_ECDHE_ECDSA_WITH_CHACHA20_POLY1305:TLS_CHACHA20_POLY1305_SHA256:TLS_AES_128_GCM_SHA256:TLS_AES_256_GCM_SHA384"
				})
				It("constructs the proper array of cipher suites", func() {
					expectedSuites := []uint16{
						tls.TLS_RSA_WITH_RC4_128_SHA,
						tls.TLS_RSA_WITH_3DES_EDE_CBC_SHA,
						tls.TLS_RSA_WITH_AES_128_CBC_SHA,
						tls.TLS_RSA_WITH_AES_256_CBC_SHA,
						tls.TLS_RSA_WITH_AES_128_GCM_SHA256,
						tls.TLS_RSA_WITH_AES_256_GCM_SHA384,
						tls.TLS_ECDHE_ECDSA_WITH_RC4_128_SHA,
						tls.TLS_ECDHE_ECDSA_WITH_AES_128_CBC_SHA,
						tls.TLS_ECDHE_ECDSA_WITH_AES_256_CBC_SHA,
						tls.TLS_ECDHE_RSA_WITH_RC4_128_SHA,
						tls.TLS_ECDHE_RSA_WITH_3DES_EDE_CBC_SHA,
						tls.TLS_ECDHE_RSA_WITH_AES_128_CBC_SHA,
						tls.TLS_ECDHE_RSA_WITH_AES_256_CBC_SHA,
						tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,
						tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256,
						tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
						tls.TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384,
						tls.TLS_RSA_WITH_AES_128_CBC_SHA256,
						tls.TLS_ECDHE_ECDSA_WITH_AES_128_CBC_SHA256,
						tls.TLS_ECDHE_RSA_WITH_AES_128_CBC_SHA256,
						tls.TLS_ECDHE_RSA_WITH_CHACHA20_POLY1305,
						tls.TLS_ECDHE_ECDSA_WITH_CHACHA20_POLY1305,
						tls.TLS_CHACHA20_POLY1305_SHA256,
						tls.TLS_AES_128_GCM_SHA256,
						tls.TLS_AES_256_GCM_SHA384,
					}

					configBytes := createYMLSnippet(configSnippet)
					err := config.Initialize(configBytes)
					Expect(err).ToNot(HaveOccurred())

					Expect(config.Process()).To(Succeed())

					Expect(config.CipherSuites).To(ConsistOf(expectedSuites))
				})
			})

			Context("when cipher suites are invalid", func() {
				BeforeEach(func() {
					configSnippet.CipherString = "potato"
				})

				It("returns a meaningful error", func() {
					configBytes := createYMLSnippet(configSnippet)
					err := config.Initialize(configBytes)
					Expect(err).ToNot(HaveOccurred())

					Expect(config.Process()).To(MatchError(HavePrefix("Invalid cipher string configuration: potato, please choose from")))
				})
			})

			Context("when an unsupported cipher suite is provided", func() {
				BeforeEach(func() {
					configSnippet.CipherString = "TLS_RSA_WITH_RC4_1280_SHA"
				})

				It("returns a meaningful error", func() {
					configBytes := createYMLSnippet(configSnippet)
					err := config.Initialize(configBytes)
					Expect(err).ToNot(HaveOccurred())

					Expect(config.Process()).To(MatchError(HavePrefix("Invalid cipher string configuration: TLS_RSA_WITH_RC4_1280_SHA, please choose from")))
				})
			})

			Context("no cipher suites are provided", func() {
				BeforeEach(func() {
					configSnippet.CipherString = ""
				})

				It("returns a meaningful error", func() {
					configBytes := createYMLSnippet(configSnippet)
					err := config.Initialize(configBytes)
					Expect(err).ToNot(HaveOccurred())

					Expect(config.Process()).To(MatchError("must specify list of cipher suite when ssl is enabled"))
				})
			})

			Context("when value for tls_handshake_timeout is set", func() {
				BeforeEach(func() {
					configSnippet.TLSHandshakeTimeout = 2 * time.Second
				})
				It("populates TLSHandshakeTimeout", func() {
					configBytes := createYMLSnippet(configSnippet)
					err := config.Initialize(configBytes)
					Expect(err).NotTo(HaveOccurred())
					Expect(config.Process()).To(Succeed())
					Expect(config.TLSHandshakeTimeout).To(Equal(2 * time.Second))
				})
			})

			Context("client_ca_certs", func() {
				var (
					expectedClientCAPEMs        []string
					expectedUnionCAClientCAPEMs []string
					clientRSAPEM                []byte
				)

				BeforeEach(func() {
					_, clientRSAPEM = test_util.CreateKeyPair("cauliflower.net")

					expectedClientCAPEMs = []string{
						string(clientRSAPEM),
					}

					expectedUnionCAClientCAPEMs = []string{
						string(rootRSAPEM),
						string(rootECDSAPEM),
						string(clientRSAPEM),
					}

					configSnippet.CACerts = []string{string(rootRSAPEM), string(rootECDSAPEM)}
				})
				Context("when verify_client_certificate_metadata is enabled, and one of the provided CA subjects is not in the ClientCAPool", func() {
					BeforeEach(func() {
						configSnippet.ClientCACerts = string(clientRSAPEM)
						configSnippet.VerifyClientCertificatesBasedOnProvidedMetadata = true
						configSnippet.VerifyClientCertificateMetadataRules = []VerifyClientCertificateMetadataRule{
							{
								CASubject:     CertSubject{CommonName: "abc.com"},
								ValidSubjects: []CertSubject{CertSubject{Organization: []string{"abc, Inc."}}},
							},
						}
					})
					It("returns a meaningful error", func() {
						configBytes := createYMLSnippet(configSnippet)
						err := config.Initialize(configBytes)
						Expect(err).ToNot(HaveOccurred())
						err = config.Process()
						Expect(err).To(HaveOccurred())
						Expect(err).Should(MatchError(ContainSubstring("no CA certificate found for rule with ca subject")))
					})
				})
				Context("When only_trust_client_ca_certs is true", func() {
					BeforeEach(func() {
						configSnippet.OnlyTrustClientCACerts = true
						configSnippet.ClientCACerts = string(clientRSAPEM)
					})

					It("client_ca_pool only contains CAs from client_ca_certs", func() {
						configBytes := createYMLSnippet(configSnippet)
						err := config.Initialize(configBytes)
						Expect(err).ToNot(HaveOccurred())

						Expect(config.Process()).To(Succeed())
						Expect(config.ClientCACerts).To(Equal(strings.Join(expectedClientCAPEMs, "")))
						Expect(config.OnlyTrustClientCACerts).To(BeTrue())

						for _, caCert := range config.CACerts {
							caCertDER, _ := pem.Decode([]byte(caCert))
							Expect(err).NotTo(HaveOccurred())
							c, err := x509.ParseCertificate(caCertDER.Bytes)
							Expect(err).NotTo(HaveOccurred())
							//lint:ignore SA1019 - ignoring tlsCert.RootCAs.Subjects is deprecated ERR because cert does not come from SystemCertPool.
							Expect(config.ClientCAPool.Subjects()).NotTo(ContainElement(c.Subject.CommonName))

							clientCACertDER, _ := pem.Decode([]byte(config.ClientCACerts))
							Expect(err).NotTo(HaveOccurred())
							c, err = x509.ParseCertificate(clientCACertDER.Bytes)
							Expect(err).NotTo(HaveOccurred())
							//lint:ignore SA1019 - ignoring tlsCert.RootCAs.Subjects is deprecated ERR because cert does not come from SystemCertPool.
							Expect(config.ClientCAPool.Subjects()).To(ContainElement(c.RawSubject))
						}

						certPool, err := x509.SystemCertPool()
						Expect(err).NotTo(HaveOccurred())

						//lint:ignore SA1019 - ignoring tlsCert.RootCAs.Subjects is deprecated ERR because cert does not come from SystemCertPool.
						for _, subj := range certPool.Subjects() {
							//lint:ignore SA1019 - ignoring tlsCert.RootCAs.Subjects is deprecated ERR because cert does not come from SystemCertPool.
							Expect(config.ClientCAPool.Subjects()).NotTo(ContainElement(subj))
						}
					})

					Context("but no client_ca_certs are provided and client certs are being validated", func() {
						It("fails with a meaningful error message", func() {
							for _, clientCertValidation := range []string{"request", "require"} {
								configSnippet.ClientCACerts = ""
								configSnippet.ClientCertificateValidationString = clientCertValidation

								configBytes := createYMLSnippet(configSnippet)
								err := config.Initialize(configBytes)
								Expect(err).ToNot(HaveOccurred())

								Expect(config.Process()).To(MatchError("router.client_ca_certs cannot be empty if router.only_trust_client_ca_certs is 'true' and router.client_cert_validation is set to 'request' or 'require'."))
							}
						})
					})
				})

				Context("When only_trust_client_ca_certs is false", func() {
					BeforeEach(func() {
						configSnippet.OnlyTrustClientCACerts = false
						configSnippet.ClientCACerts = strings.Join(configSnippet.CACerts, "") + string(clientRSAPEM)
					})

					It("client_ca_pool contains CAs from client_ca_certs, ca_certs, and the system CAs", func() {
						configBytes := createYMLSnippet(configSnippet)
						err := config.Initialize(configBytes)
						Expect(err).ToNot(HaveOccurred())

						Expect(config.Process()).To(Succeed())
						Expect(config.OnlyTrustClientCACerts).To(BeFalse())
						Expect(config.ClientCACerts).To(Equal(strings.Join(expectedUnionCAClientCAPEMs, "")))

						clientCACertDER, _ := pem.Decode([]byte(config.ClientCACerts))
						Expect(err).NotTo(HaveOccurred())
						c, err := x509.ParseCertificate(clientCACertDER.Bytes)
						Expect(err).NotTo(HaveOccurred())
						//lint:ignore SA1019 - ignoring tlsCert.RootCAs.Subjects is deprecated ERR because cert does not come from SystemCertPool.
						Expect(config.ClientCAPool.Subjects()).To(ContainElement(c.RawSubject))

						for _, caCert := range config.CACerts {
							caCertDER, _ := pem.Decode([]byte(caCert))
							Expect(err).NotTo(HaveOccurred())
							c, err = x509.ParseCertificate(caCertDER.Bytes)
							Expect(err).NotTo(HaveOccurred())
							//lint:ignore SA1019 - ignoring tlsCert.RootCAs.Subjects is deprecated ERR because cert does not come from SystemCertPool.
							Expect(config.ClientCAPool.Subjects()).To(ContainElement(c.RawSubject))
						}

						certPool, err := x509.SystemCertPool()
						Expect(err).NotTo(HaveOccurred())

						//lint:ignore SA1019 - ignoring tlsCert.RootCAs.Subjects is deprecated ERR because cert does not come from SystemCertPool.
						for _, subj := range certPool.Subjects() {
							//lint:ignore SA1019 - ignoring tlsCert.RootCAs.Subjects is deprecated ERR because cert does not come from SystemCertPool.
							Expect(config.ClientCAPool.Subjects()).To(ContainElement(subj))
						}
					})
				})
			})
		})

		Context("When enable_ssl is set to false", func() {
			Context("When disable_http is set to false", func() {
				BeforeEach(func() {
					cfgForSnippet.EnableSSL = false
					cfgForSnippet.DisableHTTP = false
				})
				It("succeeds", func() {
					err := config.Initialize(createYMLSnippet(cfgForSnippet))
					Expect(err).NotTo(HaveOccurred())
					Expect(config.Process()).To(Succeed())
					Expect(config.DisableHTTP).To(BeFalse())
				})
			})
			Context("When disable_http is set to true", func() {
				BeforeEach(func() {
					cfgForSnippet.EnableSSL = false
					cfgForSnippet.DisableHTTP = true
				})
				It("returns a meaningful error", func() {
					err := config.Initialize(createYMLSnippet(cfgForSnippet))
					Expect(err).NotTo(HaveOccurred())
					Expect(config.Process()).To(MatchError(HavePrefix("neither http nor https listener is enabled")))
				})
			})
		})

		Context("enable_http2", func() {
			It("defaults to true", func() {
				config.Status.TLS = cfgForSnippet.Status.TLS
				Expect(config.Process()).To(Succeed())
				Expect(config.EnableHTTP2).To(BeTrue())
			})

			It("setting enable_http2 succeeds", func() {
				cfgForSnippet.EnableHTTP2 = false
				err := config.Initialize(createYMLSnippet(cfgForSnippet))
				Expect(err).NotTo(HaveOccurred())
				Expect(config.Process()).To(Succeed())
				Expect(config.EnableHTTP2).To(BeFalse())
			})
		})

		Context("hop_by_hop_headers_to_filter", func() {
			BeforeEach(func() {
				cfgForSnippet.HopByHopHeadersToFilter = []string{"X-ME", "X-Foo"}
			})
			It("setting hop_by_hop_headers_to_filter succeeds", func() {
				err := config.Initialize(createYMLSnippet(cfgForSnippet))
				Expect(err).NotTo(HaveOccurred())
				Expect(config.Process()).To(Succeed())
				Expect(config.HopByHopHeadersToFilter).To(Equal([]string{"X-ME", "X-Foo"}))
			})
		})

		Context("When given a routing_table_sharding_mode that is supported ", func() {
			Context("sharding mode `all`", func() {
				BeforeEach(func() {
					cfgForSnippet.RoutingTableShardingMode = "all"
				})
				It("succeeds", func() {
					err := config.Initialize(createYMLSnippet(cfgForSnippet))
					Expect(err).ToNot(HaveOccurred())

					Expect(config.Process()).To(Succeed())
				})
			})
			Context("sharding mode `segments`", func() {
				BeforeEach(func() {
					cfgForSnippet.RoutingTableShardingMode = "segments"
				})

				Context("with isolation segments provided", func() {
					BeforeEach(func() {
						cfgForSnippet.IsolationSegments = []string{"is1", "is2"}
					})
					It("succeeds", func() {
						err := config.Initialize(createYMLSnippet(cfgForSnippet))
						Expect(err).ToNot(HaveOccurred())

						Expect(config.Process()).To(Succeed())
					})
				})

				Context("without isolation segments provided", func() {
					It("returns a meaningful error", func() {
						err := config.Initialize(createYMLSnippet(cfgForSnippet))
						Expect(err).ToNot(HaveOccurred())

						Expect(config.Process()).To(MatchError("Expected isolation segments; routing table sharding mode set to segments and none provided."))
					})
				})
			})
			Context("sharding mode `shared-and-segments`", func() {
				BeforeEach(func() {
					cfgForSnippet.RoutingTableShardingMode = "shared-and-segments"
				})

				Context("with isolation segments provided", func() {
					BeforeEach(func() {
						cfgForSnippet.IsolationSegments = []string{"is1", "is2"}
					})
					It("succeeds", func() {
						err := config.Initialize(createYMLSnippet(cfgForSnippet))
						Expect(err).ToNot(HaveOccurred())

						Expect(config.Process()).To(Succeed())
					})
				})
			})
		})

		Context("When given a routing_table_sharding_mode that is not supported ", func() {
			BeforeEach(func() {
				cfgForSnippet.RoutingTableShardingMode = "foo"
			})

			It("returns a meaningful error", func() {
				err := config.Initialize(createYMLSnippet(cfgForSnippet))
				Expect(err).ToNot(HaveOccurred())

				Expect(config.Process()).To(MatchError("Invalid sharding mode: foo. Allowed values are [all segments shared-and-segments]"))
			})
		})

		Context("defaults forwarded_client_cert value to always_forward", func() {
			It("correctly sets the value", func() {
				Expect(config.ForwardedClientCert).To(Equal("always_forward"))
			})
		})

		Context("When given a forwarded_client_cert value that is supported", func() {
			Context("when forwarded_client_cert is always_forward", func() {
				BeforeEach(func() {
					cfgForSnippet.ForwardedClientCert = "always_forward"
				})
				It("correctly sets the value", func() {
					err := config.Initialize(createYMLSnippet(cfgForSnippet))
					Expect(err).ToNot(HaveOccurred())

					Expect(config.ForwardedClientCert).To(Equal("always_forward"))
				})
			})
			Context("when forwarded_client_cert is forward", func() {
				BeforeEach(func() {
					cfgForSnippet.ForwardedClientCert = "forward"
				})
				It("correctly sets the value", func() {
					err := config.Initialize(createYMLSnippet(cfgForSnippet))
					Expect(err).ToNot(HaveOccurred())

					Expect(config.ForwardedClientCert).To(Equal("forward"))
				})
			})
			Context("when forwarded_client_cert is sanitize_set", func() {
				BeforeEach(func() {
					cfgForSnippet.ForwardedClientCert = "sanitize_set"
				})
				It("correctly sets the value", func() {
					err := config.Initialize(createYMLSnippet(cfgForSnippet))
					Expect(err).ToNot(HaveOccurred())

					Expect(config.ForwardedClientCert).To(Equal("sanitize_set"))
				})
			})
		})

		Context("When given a forwarded_client_cert value that is not supported ", func() {
			BeforeEach(func() {
				cfgForSnippet.ForwardedClientCert = "foo"
			})

			It("returns a meaningful error", func() {
				err := config.Initialize(createYMLSnippet(cfgForSnippet))
				Expect(err).ToNot(HaveOccurred())

				Expect(config.Process()).To(MatchError("Invalid forwarded client cert mode: foo. Allowed values are [always_forward forward sanitize_set]"))
			})
		})

		Describe("Timeout", func() {
			var b []byte
			It("converts timeouts to a duration", func() {
				b = []byte(`
endpoint_timeout: 10s
endpoint_dial_timeout: 6s
websocket_dial_timeout: 8s
route_services_timeout: 10s
drain_timeout: 15s
tls_handshake_timeout: 9s
`)
				err := config.Initialize(b)
				Expect(err).ToNot(HaveOccurred())

				Expect(config.Process()).To(Succeed())

				Expect(config.EndpointTimeout).To(Equal(10 * time.Second))
				Expect(config.EndpointDialTimeout).To(Equal(6 * time.Second))
				Expect(config.WebsocketDialTimeout).To(Equal(8 * time.Second))
				Expect(config.RouteServiceTimeout).To(Equal(10 * time.Second))
				Expect(config.DrainTimeout).To(Equal(15 * time.Second))
				Expect(config.TLSHandshakeTimeout).To(Equal(9 * time.Second))
			})

			It("defaults to the EndpointTimeout when not set", func() {
				b = []byte(`
endpoint_timeout: 10s
`)
				err := config.Initialize(b)
				Expect(err).ToNot(HaveOccurred())

				Expect(config.Process()).To(Succeed())

				Expect(config.EndpointTimeout).To(Equal(10 * time.Second))
				Expect(config.EndpointDialTimeout).To(Equal(5 * time.Second))
				Expect(config.DrainTimeout).To(Equal(10 * time.Second))
			})

			It("lets drain_timeout be 60 if it wants", func() {
				b = []byte(`
endpoint_timeout: 10s
route_services_timeout: 11s
drain_timeout: 60s
`)
				err := config.Initialize(b)
				Expect(err).ToNot(HaveOccurred())

				Expect(config.Process()).To(Succeed())

				Expect(config.DrainTimeout).To(Equal(60 * time.Second))
			})
		})

		Describe("configuring client (mTLS) authentication to backends", func() {
			Context("when provided PEM for backends cert_chain and private_key", func() {
				var expectedTLSPEM TLSPem
				var certChain test_util.CertChain

				BeforeEach(func() {
					certChain = test_util.CreateSignedCertWithRootCA(test_util.CertNames{SANs: test_util.SubjectAltNames{DNS: "spinach.com"}})
					expectedTLSPEM = TLSPem{
						CertChain:  string(certChain.CertPEM),
						PrivateKey: string(certChain.PrivKeyPEM),
					}
					cfgForSnippet.Backends = BackendConfig{
						TLSPem: expectedTLSPEM,
					}
				})

				It("populates the ClientAuthCertificates", func() {
					err := config.Initialize(createYMLSnippet(cfgForSnippet))
					Expect(err).ToNot(HaveOccurred())
					Expect(config.Backends.TLSPem).To(Equal(expectedTLSPEM))

					Expect(config.Process()).To(Succeed())
					Expect(config.Backends.ClientAuthCertificate).To(Equal(certChain.AsTLSConfig().Certificates[0]))
				})

				Context("cert or key are invalid", func() {
					BeforeEach(func() {
						cfgForSnippet.Backends = BackendConfig{
							TLSPem: TLSPem{
								CertChain:  "invalid-cert",
								PrivateKey: "invalid-key",
							},
						}
					})

					It("returns a meaningful error", func() {
						err := config.Initialize(createYMLSnippet(cfgForSnippet))
						Expect(err).ToNot(HaveOccurred())

						Expect(config.Process()).To(MatchError("Error loading key pair: tls: failed to find any PEM data in certificate input"))
					})
				})
			})
		})

	})
})

func baseConfigFixture() *Config {
	key, cert := test_util.CreateKeyPair("healthTLSEndpoint")
	cfg := &Config{
		Status: StatusConfig{
			TLS: StatusTLSConfig{
				Port:        8443,
				Certificate: string(cert),
				Key:         string(key),
			},
		},
	}
	return cfg
}
