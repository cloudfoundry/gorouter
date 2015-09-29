package config_test

import (
	"crypto/tls"

	. "github.com/cloudfoundry/gorouter/config"

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

		It("sets status config", func() {
			var b = []byte(`
status:
  port: 1234
  user: user
  pass: pass
`)

			config.Initialize(b)

			Expect(config.Status.Port).To(Equal(uint16(1234)))
			Expect(config.Status.User).To(Equal("user"))
			Expect(config.Status.Pass).To(Equal("pass"))

		})

		It("sets endpoint timeout", func() {
			var b = []byte(`
endpoint_timeout: 10
`)

			config.Initialize(b)

			Expect(config.EndpointTimeoutInSeconds).To(Equal(10))
		})

		It("sets drain timeout", func() {
			var b = []byte(`
drain_timeout: 10
`)

			config.Initialize(b)

			Expect(config.DrainTimeoutInSeconds).To(Equal(10))
		})

		It("sets nats config", func() {
			var b = []byte(`
nats:
  - host: remotehost
    port: 4223
    user: user
    pass: pass
`)
			config.Initialize(b)

			Expect(config.Nats).To(HaveLen(1))
			Expect(config.Nats[0].Host).To(Equal("remotehost"))
			Expect(config.Nats[0].Port).To(Equal(uint16(4223)))
			Expect(config.Nats[0].User).To(Equal("user"))
			Expect(config.Nats[0].Pass).To(Equal("pass"))
		})

		It("sets default logging configs", func() {
			Expect(config.Logging.File).To(Equal(""))
			Expect(config.Logging.Syslog).To(Equal(""))
			Expect(config.Logging.Level).To(Equal("debug"))
			Expect(config.Logging.LoggregatorEnabled).To(Equal(false))
		})

		It("sets logging config", func() {
			var b = []byte(`
logging:
  file: /tmp/file
  syslog: syslog
  level: debug2
  loggregator_enabled: true
`)
			config.Initialize(b)

			Expect(config.Logging.File).To(Equal("/tmp/file"))
			Expect(config.Logging.Syslog).To(Equal("syslog"))
			Expect(config.Logging.Level).To(Equal("debug2"))
			Expect(config.Logging.LoggregatorEnabled).To(Equal(true))
		})

		It("sets the rest of config", func() {
			var b = []byte(`
port: 8082
index: 1
go_max_procs: 2
trace_key: "foo"
access_log: "/tmp/access_log"
ssl_port: 4443
enable_ssl: true
`)

			config.Initialize(b)

			Expect(config.Port).To(Equal(uint16(8082)))
			Expect(config.Index).To(Equal(uint(1)))
			Expect(config.GoMaxProcs).To(Equal(2))
			Expect(config.TraceKey).To(Equal("foo"))
			Expect(config.AccessLog).To(Equal("/tmp/access_log"))
			Expect(config.EnableSSL).To(Equal(true))
			Expect(config.SSLPort).To(Equal(uint16(4443)))
		})

		It("sets the Routing Api config", func() {
			var b = []byte(`
routing_api:
  uri: http://bob.url/token
  port: 1234
`)

			config.Initialize(b)

			Expect(config.RoutingApi.Uri).To(Equal("http://bob.url/token"))
			Expect(config.RoutingApi.Port).To(Equal(1234))
		})

		It("sets the OAuth config", func() {
			var b = []byte(`
oauth:
  token_endpoint: http://bob.url/token
  client_name: client-name
  client_secret: client-secret
  port: 1234
`)

			config.Initialize(b)

			Expect(config.OAuth.TokenEndpoint).To(Equal("http://bob.url/token"))
			Expect(config.OAuth.ClientName).To(Equal("client-name"))
			Expect(config.OAuth.ClientSecret).To(Equal("client-secret"))
			Expect(config.OAuth.Port).To(Equal(1234))
		})

		It("sets the SkipSSLValidation config", func() {
			var b = []byte(`
ssl_skip_validation: true
`)
			config.Initialize(b)
			Expect(config.SSLSkipValidation).To(BeTrue())
		})

		It("defaults the SkipSSLValidation config to false", func() {
			var b = []byte(``)
			config.Initialize(b)
			Expect(config.SSLSkipValidation).To(BeFalse())
		})

		It("sets the route service secret config", func() {
			var b = []byte(`
route_services_secret: tWPE+sWJq+ZnGJpyKkIPYg==
`)
			config.Initialize(b)
			Expect(config.RouteServiceSecret).To(Equal("tWPE+sWJq+ZnGJpyKkIPYg=="))
		})

		It("sets the route service secret decrypt only config", func() {
			var b = []byte(`
route_services_secret_decrypt_only: OVhlXPLHIHjJL3oPIHoqjw==
`)
			config.Initialize(b)
			Expect(config.RouteServiceSecretPrev).To(Equal("OVhlXPLHIHjJL3oPIHoqjw=="))
		})
	})

	Describe("Process", func() {
		It("converts intervals to durations", func() {
			var b = []byte(`
publish_start_message_interval: 1
prune_stale_droplets_interval: 2
droplet_stale_threshold: 30
publish_active_apps_interval: 4
start_response_delay_interval: 15
secure_cookies: true
`)

			config.Initialize(b)
			config.Process()

			Expect(config.PublishStartMessageIntervalInSeconds).To(Equal(1))
			Expect(config.PruneStaleDropletsInterval).To(Equal(2 * time.Second))
			Expect(config.DropletStaleThreshold).To(Equal(30 * time.Second))
			Expect(config.PublishActiveAppsInterval).To(Equal(4 * time.Second))
			Expect(config.StartResponseDelayInterval).To(Equal(15 * time.Second))
			Expect(config.SecureCookies).To(BeTrue())
		})

		Context("When StartResponseDelayInterval is greater than DropletStaleThreshold", func() {
			It("set DropletStaleThreshold equal to StartResponseDelayInterval", func() {
				var b = []byte(`
droplet_stale_threshold: 14
start_response_delay_interval: 15
`)

				config.Initialize(b)
				config.Process()

				Expect(config.DropletStaleThreshold).To(Equal(15 * time.Second))
				Expect(config.StartResponseDelayInterval).To(Equal(15 * time.Second))
			})
		})

		Context("When secure cookies is set to false", func() {
			It("set DropletStaleThreshold equal to StartResponseDelayInterval", func() {
				var b = []byte(`
secure_cookies: false
`)

				config.Initialize(b)
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

			It("returns a slice of of the configured NATS servers", func() {
				config.Initialize(b)

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
					config.Initialize(configYaml)
					config.Process()
					Expect(config.RouteServiceEnabled).To(BeFalse())
				})
			})

			Context("when the route service secret is configured", func() {
				Context("when the route service secret is set", func() {
					BeforeEach(func() {
						configYaml = []byte(`
route_services_secret: 1PfbARmvIn6cgyKorA1rqR2d34rBOo+z3qJGz17pi8Y=
`)
						config.Initialize(configYaml)
						config.Process()
					})

					It("enables route services", func() {
						Expect(config.RouteServiceEnabled).To(BeTrue())
					})

					It("sets route service secret", func() {
						Expect(config.RouteServiceSecret).To(Equal("1PfbARmvIn6cgyKorA1rqR2d34rBOo+z3qJGz17pi8Y="))
					})
				})

				Context("when the route service secret and the decrypt only route service secret are are set", func() {
					BeforeEach(func() {
						configYaml = []byte(`
route_services_secret: 1PfbARmvIn6cgyKorA1rqR2d34rBOo+z3qJGz17pi8Y=
route_services_secret_decrypt_only: KU0x+zcd/dUU47jGnBG55d70N+1kko/PHuQUYsfL7Qc=
`)
						config.Initialize(configYaml)
						config.Process()
					})

					It("enables route services", func() {
						Expect(config.RouteServiceEnabled).To(BeTrue())
					})

					It("sets route service secret", func() {
						Expect(config.RouteServiceSecret).To(Equal("1PfbARmvIn6cgyKorA1rqR2d34rBOo+z3qJGz17pi8Y="))
					})

					It("sets previous route service secret", func() {
						Expect(config.RouteServiceSecretPrev).To(Equal("KU0x+zcd/dUU47jGnBG55d70N+1kko/PHuQUYsfL7Qc="))
					})
				})

				Context("when only the decrypt only route service secret is set", func() {
					BeforeEach(func() {
						configYaml = []byte(`
route_services_secret_decrypt_only: 1PfbARmvIn6cgyKorA1rqR2d34rBOo+z3qJGz17pi8Y=
`)
						config.Initialize(configYaml)
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
					config.Initialize(b)
					config.Process()
					Expect(config.RoutingApiEnabled()).To(BeTrue())
				})
			})

			Context("when the routing api is not properly configured", func() {
				It("reports the routing api as disabled", func() {
					config.Initialize([]byte{})
					config.Process()
					Expect(config.RoutingApiEnabled()).To(BeFalse())
				})
			})
		})

		Context("When EnableSSL is set to true", func() {

			Context("When it is given valid values for a certificate", func() {
				var b = []byte(`
enable_ssl: true
ssl_cert_path: ../test/assets/public.pem
ssl_key_path: ../test/assets/private.pem
`)

				It("returns a valid valid certificate", func() {
					expectedCertificate, err := tls.LoadX509KeyPair("../test/assets/public.pem", "../test/assets/private.pem")
					Expect(err).ToNot(HaveOccurred())

					config.Initialize(b)
					Expect(config.EnableSSL).To(Equal(true))

					config.Process()
					Expect(config.SSLCertificate).To(Equal(expectedCertificate))
				})

				It("Sets the default cipher suites", func() {
					expectedSuites := []uint16{
						tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,
						tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256,
						tls.TLS_ECDHE_RSA_WITH_RC4_128_SHA,
						tls.TLS_ECDHE_ECDSA_WITH_RC4_128_SHA,
						tls.TLS_ECDHE_RSA_WITH_AES_128_CBC_SHA,
						tls.TLS_ECDHE_ECDSA_WITH_AES_128_CBC_SHA,
						tls.TLS_ECDHE_RSA_WITH_AES_256_CBC_SHA,
						tls.TLS_ECDHE_ECDSA_WITH_AES_256_CBC_SHA,
						tls.TLS_RSA_WITH_RC4_128_SHA,
						tls.TLS_RSA_WITH_AES_128_CBC_SHA,
						tls.TLS_RSA_WITH_AES_256_CBC_SHA,
					}

					config.Initialize(b)
					config.Process()

					Expect(config.CipherSuites).To(ConsistOf(expectedSuites))

				})
			})

			Context("When it is given invalid values for a certificate", func() {
				var b = []byte(`
enable_ssl: true
ssl_cert: ../notathing
ssl_key: ../alsonotathing
`)

				It("fails to create the certificate and panics", func() {
					config.Initialize(b)

					Expect(config.Process).To(Panic())
				})
			})

			Context("When it is given valid cipher suites", func() {
				var b = []byte(`
enable_ssl: true
ssl_cert_path: ../test/assets/public.pem
ssl_key_path: ../test/assets/private.pem
cipher_suites: TLS_RSA_WITH_RC4_128_SHA:TLS_RSA_WITH_AES_128_CBC_SHA
`)

				It("Construct the proper array of cipher suites", func() {
					expectedSuites := []uint16{
						tls.TLS_RSA_WITH_RC4_128_SHA,
						tls.TLS_RSA_WITH_AES_128_CBC_SHA,
					}

					config.Initialize(b)
					config.Process()

					Expect(config.CipherSuites).To(ConsistOf(expectedSuites))
				})
			})

			Context("When it is given invalid cipher suites", func() {
				var b = []byte(`
enable_ssl: true
ssl_cert_path: ../test/assets/public.pem
ssl_key_path: ../test/assets/private.pem
cipher_suites: potato
`)

				It("panics", func() {
					config.Initialize(b)

					Expect(config.Process).To(Panic())
				})
			})
		})

		Describe("Timeout", func() {
			It("converts timeouts to a duration", func() {
				var b = []byte(`
endpoint_timeout: 10
route_service_timeout: 10
drain_timeout: 15
`)

				config.Initialize(b)
				config.Process()

				Expect(config.EndpointTimeout).To(Equal(10 * time.Second))
				Expect(config.RouteServiceTimeout).To(Equal(10 * time.Second))
				Expect(config.DrainTimeout).To(Equal(15 * time.Second))
			})

			It("defaults to the EndpointTimeout when not set", func() {
				var b = []byte(`
endpoint_timeout: 10
`)

				config.Initialize(b)
				config.Process()

				Expect(config.EndpointTimeout).To(Equal(10 * time.Second))
				Expect(config.DrainTimeout).To(Equal(10 * time.Second))
			})
		})
	})
})
