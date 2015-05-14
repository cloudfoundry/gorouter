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

			Ω(config.Status.Port).To(Equal(uint16(1234)))
			Ω(config.Status.User).To(Equal("user"))
			Ω(config.Status.Pass).To(Equal("pass"))

		})

		It("sets endpoint timeout", func() {
			var b = []byte(`
endpoint_timeout: 10
`)

			config.Initialize(b)

			Ω(config.EndpointTimeoutInSeconds).To(Equal(10))
		})

		It("sets drain timeout", func() {
			var b = []byte(`
drain_timeout: 10
`)

			config.Initialize(b)

			Ω(config.DrainTimeoutInSeconds).To(Equal(10))
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

			Ω(config.Nats).To(HaveLen(1))
			Ω(config.Nats[0].Host).To(Equal("remotehost"))
			Ω(config.Nats[0].Port).To(Equal(uint16(4223)))
			Ω(config.Nats[0].User).To(Equal("user"))
			Ω(config.Nats[0].Pass).To(Equal("pass"))
		})

		It("sets default logging configs", func() {
			Ω(config.Logging.File).To(Equal(""))
			Ω(config.Logging.Syslog).To(Equal(""))
			Ω(config.Logging.Level).To(Equal("debug"))
			Ω(config.Logging.LoggregatorEnabled).To(Equal(false))
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

			Ω(config.Logging.File).To(Equal("/tmp/file"))
			Ω(config.Logging.Syslog).To(Equal("syslog"))
			Ω(config.Logging.Level).To(Equal("debug2"))
			Ω(config.Logging.LoggregatorEnabled).To(Equal(true))
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
name: router_z1
`)

			config.Initialize(b)

			Ω(config.Port).To(Equal(uint16(8082)))
			Ω(config.Index).To(Equal(uint(1)))
			Ω(config.GoMaxProcs).To(Equal(2))
			Ω(config.TraceKey).To(Equal("foo"))
			Ω(config.AccessLog).To(Equal("/tmp/access_log"))
			Ω(config.EnableSSL).To(Equal(true))
			Ω(config.SSLPort).To(Equal(uint16(4443)))
			Ω(config.Name).To(Equal("router_z1"))
		})

		It("sets the Routing Api config", func() {
			var b = []byte(`
routing_api:
  uri: http://bob.url/token
  port: 1234
`)

			config.Initialize(b)

			Ω(config.RoutingApi.Uri).To(Equal("http://bob.url/token"))
			Ω(config.RoutingApi.Port).To(Equal(1234))
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

			Ω(config.OAuth.TokenEndpoint).To(Equal("http://bob.url/token"))
			Ω(config.OAuth.ClientName).To(Equal("client-name"))
			Ω(config.OAuth.ClientSecret).To(Equal("client-secret"))
			Ω(config.OAuth.Port).To(Equal(1234))
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

			Ω(config.PublishStartMessageIntervalInSeconds).To(Equal(1))
			Ω(config.PruneStaleDropletsInterval).To(Equal(2 * time.Second))
			Ω(config.DropletStaleThreshold).To(Equal(30 * time.Second))
			Ω(config.PublishActiveAppsInterval).To(Equal(4 * time.Second))
			Ω(config.StartResponseDelayInterval).To(Equal(15 * time.Second))
			Ω(config.SecureCookies).To(BeTrue())
		})

		Context("When StartResponseDelayInterval is greater than DropletStaleThreshold", func() {
			It("set DropletStaleThreshold equal to StartResponseDelayInterval", func() {
				var b = []byte(`
droplet_stale_threshold: 14
start_response_delay_interval: 15
`)

				config.Initialize(b)
				config.Process()

				Ω(config.DropletStaleThreshold).To(Equal(15 * time.Second))
				Ω(config.StartResponseDelayInterval).To(Equal(15 * time.Second))
			})
		})

		Context("When secure cookies is set to false", func() {
			It("set DropletStaleThreshold equal to StartResponseDelayInterval", func() {
				var b = []byte(`
secure_cookies: false
`)

				config.Initialize(b)
				config.Process()

				Ω(config.SecureCookies).To(BeFalse())
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
						tls.TLS_RSA_WITH_RC4_128_SHA,
						tls.TLS_RSA_WITH_AES_128_CBC_SHA,
						tls.TLS_RSA_WITH_AES_256_CBC_SHA,
						tls.TLS_ECDHE_ECDSA_WITH_RC4_128_SHA,
						tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256,
						tls.TLS_ECDHE_ECDSA_WITH_AES_128_CBC_SHA,
						tls.TLS_ECDHE_ECDSA_WITH_AES_256_CBC_SHA,
						tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,
						tls.TLS_ECDHE_RSA_WITH_AES_256_CBC_SHA,
						tls.TLS_ECDHE_RSA_WITH_AES_128_CBC_SHA,
						tls.TLS_ECDHE_RSA_WITH_RC4_128_SHA,
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
drain_timeout: 15
`)

				config.Initialize(b)
				config.Process()

				Ω(config.EndpointTimeout).To(Equal(10 * time.Second))
				Ω(config.DrainTimeout).To(Equal(15 * time.Second))
			})

			It("defaults to the EndpointTimeout when not set", func() {
				var b = []byte(`
endpoint_timeout: 10
`)

				config.Initialize(b)
				config.Process()

				Ω(config.EndpointTimeout).To(Equal(10 * time.Second))
				Ω(config.DrainTimeout).To(Equal(10 * time.Second))
			})
		})
	})
})
