package config_test

import (
	"crypto/tls"

	. "code.cloudfoundry.org/gorouter/config"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	"time"
)

var _ = Describe("Config", func() {
	var config *Config

	BeforeEach(func() {
		config = DefaultConfig()
	})

	Describe("Initialize", func() {

		Context("load balance config", func() {
			It("sets default load balance strategy", func() {
				Expect(config.LoadBalance).To(Equal(LOAD_BALANCE_RR))
			})

			It("can override the load balance strategy", func() {
				cfg := DefaultConfig()
				var b = []byte(`
balancing_algorithm: least-connection
`)
				cfg.Initialize(b)
				cfg.Process()
				Expect(cfg.LoadBalance).To(Equal(LOAD_BALANCE_LC))
			})

			It("does not allow an invalid load balance strategy", func() {
				cfg := DefaultConfig()
				var b = []byte(`
balancing_algorithm: foo-bar
`)
				cfg.Initialize(b)
				Expect(cfg.Process).To(Panic())
			})
		})

		It("sets status config", func() {
			var b = []byte(`
status:
  port: 1234
  user: user
  pass: pass
`)

			err := config.Initialize(b)
			Expect(err).ToNot(HaveOccurred())

			Expect(config.Status.Port).To(Equal(uint16(1234)))
			Expect(config.Status.User).To(Equal("user"))
			Expect(config.Status.Pass).To(Equal("pass"))

		})

		It("sets endpoint timeout", func() {
			var b = []byte(`
endpoint_timeout: 10s
`)

			err := config.Initialize(b)
			Expect(err).ToNot(HaveOccurred())

			Expect(config.EndpointTimeout).To(Equal(10 * time.Second))
		})

		It("sets nats config", func() {
			var b = []byte(`
nats:
  - host: remotehost
    port: 4223
    user: user
    pass: pass
`)
			err := config.Initialize(b)
			Expect(err).ToNot(HaveOccurred())

			Expect(config.Nats).To(HaveLen(1))
			Expect(config.Nats[0].Host).To(Equal("remotehost"))
			Expect(config.Nats[0].Port).To(Equal(uint16(4223)))
			Expect(config.Nats[0].User).To(Equal("user"))
			Expect(config.Nats[0].Pass).To(Equal("pass"))
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
`)
			err := config.Initialize(b)
			Expect(err).ToNot(HaveOccurred())

			Expect(config.Logging.Syslog).To(Equal("syslog"))
			Expect(config.Logging.Level).To(Equal("debug2"))
			Expect(config.Logging.LoggregatorEnabled).To(Equal(true))
			Expect(config.Logging.JobName).To(Equal("gorouter"))
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

		It("sets the Routing Api config", func() {
			var b = []byte(`
routing_api:
  uri: http://bob.url/token
  port: 1234
`)

			err := config.Initialize(b)
			Expect(err).ToNot(HaveOccurred())

			Expect(config.RoutingApi.Uri).To(Equal("http://bob.url/token"))
			Expect(config.RoutingApi.Port).To(Equal(1234))
			Expect(config.RoutingApi.AuthDisabled).To(BeFalse())
		})

		It("sets the Routing Api config with optional values", func() {
			var b = []byte(`
routing_api:
  uri: http://bob.url/token
  port: 1234
  auth_disabled: true
`)

			err := config.Initialize(b)
			Expect(err).ToNot(HaveOccurred())

			Expect(config.RoutingApi.Uri).To(Equal("http://bob.url/token"))
			Expect(config.RoutingApi.Port).To(Equal(1234))
			Expect(config.RoutingApi.AuthDisabled).To(BeTrue())
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
			Expect(config.OAuth.Port).To(Equal(8443))
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
	})

	Describe("Process", func() {
		It("converts intervals to durations", func() {
			var b = []byte(`
publish_start_message_interval: 1s
prune_stale_droplets_interval: 2s
droplet_stale_threshold: 30s
publish_active_apps_interval: 4s
start_response_delay_interval: 15s
secure_cookies: true
token_fetcher_retry_interval: 10s
`)

			err := config.Initialize(b)
			Expect(err).ToNot(HaveOccurred())

			config.Process()

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
			It("panics when an invalid duration string is given", func() {
				var b = []byte("load_balancer_healthy_threshold: -5s")
				err := config.Initialize(b)
				Expect(err).ToNot(HaveOccurred())

				Expect(config.Process).To(Panic())
			})

			It("fails to initialize a non time string", func() {
				var b = []byte("load_balancer_healthy_threshold: test")
				err := config.Initialize(b)
				Expect(err).To(HaveOccurred())
			})

			It("process the string into a valid duration", func() {
				var b = []byte("load_balancer_healthy_threshold: 10s")
				err := config.Initialize(b)
				Expect(err).ToNot(HaveOccurred())
			})
		})

		It("converts extra headers to log into a map", func() {
			var b = []byte(`
extra_headers_to_log:
  - x-b3-trace-id
  - something
  - something
`)

			err := config.Initialize(b)
			Expect(err).ToNot(HaveOccurred())
			config.Process()

			Expect(config.ExtraHeadersToLog).To(ContainElement("something"))
			Expect(config.ExtraHeadersToLog).To(ContainElement("x-b3-trace-id"))
		})

		Context("When secure cookies is set to false", func() {
			It("set DropletStaleThreshold equal to StartResponseDelayInterval", func() {
				var b = []byte(`
secure_cookies: false
`)

				err := config.Initialize(b)
				Expect(err).ToNot(HaveOccurred())

				config.Process()

				Expect(config.SecureCookies).To(BeFalse())
			})

		})

		Describe("NatsServers", func() {
			var b = []byte(`
nats:
  - host: remotehost
    port: 4223
    user: user
    pass: pass
  - host: remotehost2
    port: 4223
    user: user2
    pass: pass2
`)

			It("returns a slice of the configured NATS servers", func() {
				err := config.Initialize(b)
				Expect(err).ToNot(HaveOccurred())

				natsServers := config.NatsServers()
				Expect(natsServers[0]).To(Equal("nats://user:pass@remotehost:4223"))
				Expect(natsServers[1]).To(Equal("nats://user2:pass2@remotehost2:4223"))
			})
		})

		Describe("RouteServiceEnabled", func() {
			var configYaml []byte
			Context("when the route service secrets is not configured", func() {
				BeforeEach(func() {
					configYaml = []byte(`other_key: other_value`)
				})
				It("disables route services", func() {
					err := config.Initialize(configYaml)
					Expect(err).ToNot(HaveOccurred())
					config.Process()
					Expect(config.RouteServiceEnabled).To(BeFalse())
				})
			})

			Context("when the route service secret is configured", func() {
				Context("when the route service secret is set", func() {
					BeforeEach(func() {
						configYaml = []byte(`
route_services_secret: my-route-service-secret
`)
						err := config.Initialize(configYaml)
						Expect(err).ToNot(HaveOccurred())
						config.Process()
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
						configYaml = []byte(`
route_services_secret: my-route-service-secret
route_services_secret_decrypt_only: my-decrypt-only-route-service-secret
`)
						err := config.Initialize(configYaml)
						Expect(err).ToNot(HaveOccurred())
						config.Process()
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
						configYaml = []byte(`
route_services_secret_decrypt_only: 1PfbARmvIn6cgyKorA1rqR2d34rBOo+z3qJGz17pi8Y=
`)
						err := config.Initialize(configYaml)
						Expect(err).ToNot(HaveOccurred())
						config.Process()
					})

					It("does NOT enabled route services", func() {
						Expect(config.RouteServiceEnabled).To(BeFalse())
					})
				})
			})
		})

		Describe("RoutingApiEnabled", func() {
			var b = []byte(`
routing_api:
  uri: http://jimisdabest.com
  port: 8080
`)
			Context("when the routing api is properly configured", func() {
				It("reports the routing api as enabled", func() {
					err := config.Initialize(b)
					Expect(err).ToNot(HaveOccurred())
					config.Process()
					Expect(config.RoutingApiEnabled()).To(BeTrue())
				})
			})

			Context("when the routing api is not properly configured", func() {
				It("reports the routing api as disabled", func() {
					err := config.Initialize([]byte{})
					Expect(err).ToNot(HaveOccurred())
					config.Process()
					Expect(config.RoutingApiEnabled()).To(BeFalse())
				})
			})
		})

		Context("When EnableSSL is set to true", func() {

			Context("When it is given valid values for a certificate", func() {
				var b = []byte(`
enable_ssl: true
ssl_cert_path: ../test/assets/certs/server.pem
ssl_key_path: ../test/assets/certs/server.key
cipher_suites: TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256
`)

				It("returns a valid certificate", func() {
					expectedCertificate, err := tls.LoadX509KeyPair("../test/assets/certs/server.pem", "../test/assets/certs/server.key")
					Expect(err).ToNot(HaveOccurred())

					err = config.Initialize(b)
					Expect(err).ToNot(HaveOccurred())

					Expect(config.EnableSSL).To(Equal(true))

					config.Process()
					Expect(config.SSLCertificate).To(Equal(expectedCertificate))
				})

			})

			Context("When it is given invalid values for a certificate", func() {
				var b = []byte(`
enable_ssl: true
ssl_cert: ../notathing
ssl_key: ../alsonotathing
cipher_suites: TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256
`)

				It("fails to create the certificate and panics", func() {
					err := config.Initialize(b)
					Expect(err).ToNot(HaveOccurred())

					Expect(config.Process).To(Panic())
				})
			})

			Context("When it is given valid cipher suites", func() {
				var b = []byte(`
enable_ssl: true
ssl_cert_path: ../test/assets/certs/server.pem
ssl_key_path: ../test/assets/certs/server.key
cipher_suites: TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256:TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384
`)

				It("Construct the proper array of cipher suites", func() {
					expectedSuites := []uint16{
						tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,
						tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
					}

					err := config.Initialize(b)
					Expect(err).ToNot(HaveOccurred())

					config.Process()

					Expect(config.CipherSuites).To(ConsistOf(expectedSuites))
				})
			})

			Context("When it is given invalid cipher suites", func() {
				var b = []byte(`
enable_ssl: true
ssl_cert_path: ../test/assets/certs/server.pem
ssl_key_path: ../test/assets/certs/server.key
cipher_suites: potato
`)

				It("panics", func() {
					err := config.Initialize(b)
					Expect(err).ToNot(HaveOccurred())

					Expect(config.Process).To(Panic())
				})
			})

			Context("When it is given an unsupported cipher suite", func() {
				var b = []byte(`
enable_ssl: true
ssl_cert_path: ../test/assets/certs/server.pem
ssl_key_path: ../test/assets/certs/server.key
cipher_suites: TLS_RSA_WITH_RC4_1280_SHA
`)

				It("panics", func() {
					err := config.Initialize(b)
					Expect(err).ToNot(HaveOccurred())

					Expect(config.Process).To(Panic())
				})
			})

		})

		Context("When given no cipher suites", func() {
			var b = []byte(`
enable_ssl: true
ssl_cert_path: ../test/assets/certs/server.pem
ssl_key_path: ../test/assets/certs/server.key
`)

			It("panics", func() {
				err := config.Initialize(b)
				Expect(err).ToNot(HaveOccurred())

				Expect(config.Process).To(Panic())
			})
		})

		Context("When given a routing_table_sharding_mode that is supported ", func() {
			Context("sharding mode `all`", func() {
				It("succeeds", func() {
					var b = []byte(`routing_table_sharding_mode: all`)
					err := config.Initialize(b)
					Expect(err).ToNot(HaveOccurred())

					Expect(config.Process).ToNot(Panic())
				})
			})
			Context("sharding mode `segments`", func() {
				var b []byte
				BeforeEach(func() {
					b = []byte("routing_table_sharding_mode: segments")
				})

				Context("with isolation segments provided", func() {
					BeforeEach(func() {
						b = append(b, []byte("\nisolation_segments: [is1, is2]")...)
					})
					It("succeeds", func() {
						err := config.Initialize(b)
						Expect(err).ToNot(HaveOccurred())

						Expect(config.Process).ToNot(Panic())
					})
				})

				Context("without isolation segments provided", func() {
					It("fails", func() {
						err := config.Initialize(b)
						Expect(err).ToNot(HaveOccurred())

						Expect(config.Process).To(Panic())
					})
				})
			})
			Context("sharding mode `shared-and-segments`", func() {
				var b []byte
				BeforeEach(func() {
					b = []byte("routing_table_sharding_mode: shared-and-segments")
				})

				Context("with isolation segments provided", func() {
					BeforeEach(func() {
						b = append(b, []byte("\nisolation_segments: [is1, is2]")...)
					})
					It("succeeds", func() {
						err := config.Initialize(b)
						Expect(err).ToNot(HaveOccurred())

						Expect(config.Process).ToNot(Panic())
					})
				})
			})
		})

		Context("When given a routing_table_sharding_mode that is not supported ", func() {
			var b = []byte(`routing_table_sharding_mode: foo`)

			It("panics", func() {
				err := config.Initialize(b)
				Expect(err).ToNot(HaveOccurred())

				Expect(config.Process).To(Panic())
			})
		})

		Describe("Timeout", func() {
			It("converts timeouts to a duration", func() {
				var b = []byte(`
endpoint_timeout: 10s
route_services_timeout: 10s
drain_timeout: 15s
`)

				err := config.Initialize(b)
				Expect(err).ToNot(HaveOccurred())

				config.Process()

				Expect(config.EndpointTimeout).To(Equal(10 * time.Second))
				Expect(config.RouteServiceTimeout).To(Equal(10 * time.Second))
				Expect(config.DrainTimeout).To(Equal(15 * time.Second))
			})

			It("defaults to the EndpointTimeout when not set", func() {
				var b = []byte(`
endpoint_timeout: 10s
`)

				err := config.Initialize(b)
				Expect(err).ToNot(HaveOccurred())

				config.Process()

				Expect(config.EndpointTimeout).To(Equal(10 * time.Second))
				Expect(config.DrainTimeout).To(Equal(10 * time.Second))
			})
		})
	})
})
