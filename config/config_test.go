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
`)

			config.Initialize(b)

			Ω(config.Port).To(Equal(uint16(8082)))
			Ω(config.Index).To(Equal(uint(1)))
			Ω(config.GoMaxProcs).To(Equal(2))
			Ω(config.TraceKey).To(Equal("foo"))
			Ω(config.AccessLog).To(Equal("/tmp/access_log"))
			Ω(config.EnableSSL).To(Equal(true))
			Ω(config.SSLPort).To(Equal(uint16(4443)))
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

		Context("When EnableSSL is set to true", func() {

			Context("When it is given valid values for a certificate", func() {
				var b = []byte(`
enable_ssl: true
ssl_cert: |
  -----BEGIN CERTIFICATE-----
  MIIDBjCCAe4CCQCz3nn1SWrDdTANBgkqhkiG9w0BAQUFADBFMQswCQYDVQQGEwJB
  VTETMBEGA1UECBMKU29tZS1TdGF0ZTEhMB8GA1UEChMYSW50ZXJuZXQgV2lkZ2l0
  cyBQdHkgTHRkMB4XDTE1MDMwMzE4NTMyNloXDTE2MDMwMjE4NTMyNlowRTELMAkG
  A1UEBhMCQVUxEzARBgNVBAgTClNvbWUtU3RhdGUxITAfBgNVBAoTGEludGVybmV0
  IFdpZGdpdHMgUHR5IEx0ZDCCASIwDQYJKoZIhvcNAQEBBQADggEPADCCAQoCggEB
  AKtTK9xq/ycRO3fWbk1abunYf9CY6sl0Wlqm9UPMkI4j0itY2OyGyn1YuCCiEdM3
  b8guGSWB0XSL5PBq33e7ioiaH98UEe+Ai+TBxnJsro5WQ/TMywzRDhZ4E7gxDBav
  88ZY+y7ts0HznfxqEIn0Gu/UK+s6ajYcIy7d9L988+hA3K1FSdes8MavXhrI4xA1
  fY21gESfFkD4SsqvrkISC012pa7oVw1f94slIVcAG+l9MMAkatBGxgWAQO6kxk5o
  oH1Z5q2m0afeQBfFqzu5lCITLfgTWCUZUmbF6UpRhmD850/LqNtryAPrLLqXxdig
  OHiWqvFpCusOu/4z1uGC5xECAwEAATANBgkqhkiG9w0BAQUFAAOCAQEAV5RAFVQy
  8Krs5c9ebYRseXO6czL9/Rfrt/weiC1XLcDkE2i2yYsBXazMYr58o4hACJwe2hoC
  bihBZ9XnVpASEYHDLwDj3zxFP/bTuKs7tLhP7wz0lo8i6k5VSPAGBq2kjc/cO9a3
  TMmLPks/Xm42MCSWGDnCEX1854B3+JK3CNEGqSY7FYXU4W9pZtHPZ3gBoy0ymSpg
  mpleiY1Tbn5I2X7vviMW7jeviB5ivkZaXtObjyM3vtPLB+ILpa15ZhDSE5o71sjA
  jXqrE1n5o/GXHX+1M8v3aJc30Az7QAqWohW/tw5SoiSmVQZWd7gFht9vSzaH2WgO
  LwcpBC7+cUJEww==
  -----END CERTIFICATE-----
ssl_key: |
  -----BEGIN RSA PRIVATE KEY-----
  MIIEpAIBAAKCAQEAq1Mr3Gr/JxE7d9ZuTVpu6dh/0JjqyXRaWqb1Q8yQjiPSK1jY
  7IbKfVi4IKIR0zdvyC4ZJYHRdIvk8Grfd7uKiJof3xQR74CL5MHGcmyujlZD9MzL
  DNEOFngTuDEMFq/zxlj7Lu2zQfOd/GoQifQa79Qr6zpqNhwjLt30v3zz6EDcrUVJ
  16zwxq9eGsjjEDV9jbWARJ8WQPhKyq+uQhILTXalruhXDV/3iyUhVwAb6X0wwCRq
  0EbGBYBA7qTGTmigfVnmrabRp95AF8WrO7mUIhMt+BNYJRlSZsXpSlGGYPznT8uo
  22vIA+ssupfF2KA4eJaq8WkK6w67/jPW4YLnEQIDAQABAoIBAQCDVqpcOoZKK9K8
  Bt3eXQKEMJ2ji2cKczFFJ5MEm9EBtoJLCryZbqfSue3Fzpj9pBUEkBpk/4VT5F7o
  0/Vmc5Y7LHRcbqVlRtV30/lPBPQ4V/eWtly/AZDcNsdfP/J1fgPSvaoqCr2ORLWL
  qL/vEfyIeM4GcWy0+JMcPbmABslw9O6Ptc5RGiP98vCLHQh/++sOtj6PH1pt+2X/
  Uecv3b1Hk/3Oe+M8ySorJD3KA94QTRnKX+zubkxRg/zCAki+as8rQc/d+BfVG698
  ylUT5LVLNuwbWnffY2Zt5x5CDqH01mJnHmxzQEfn68rb3bGFaYPEn9EP+maQijv6
  SsUM9A3lAoGBAODRDRn4gEIxjPICp6aawRrMDlRc+k6IWDF7wudjxJlaxFr2t7FF
  rFYm+jrcG6qMTyq+teR8uHpcKm9X8ax0L6N6gw5rVzIeIOGma/ZuYIYXX2XJx5SW
  SOas1xW6qEIbOMv+Xu9w2SWbhTgyRmtlxxjr2e7gQLz9z/vuTReJpInnAoGBAMMW
  sq5lqUfAQzqxlhTobQ7tnB48rUQvkGPE92SlDj2TUt9phek2/TgRJT6mdcozvimt
  JPhxKg3ioxG8NPmN0EytjpSiKqlxS1R2po0fb75vputfpw16Z8/2Vik+xYqNMTLo
  SpeVkHu7fbtNYEK2qcU44OyOZ/V+5Oo9TuBIFRhHAoGACkqHhwDRHjaWdR2Z/w5m
  eIuOvF3lN2MWZm175ouynDKDeoaAsiS2VttB6R/aRFxX42UHfoYXC8LcTmyAK5zF
  8X3SMf7H5wtqBepQVt+Gm5zGSSqLcEnQ3H5c+impOh105CGoxt0rk4Ui/AeRIalv
  C70AJOcvD3eu5aFq9gDe/1ECgYBAhkVbASzYGnMh+pKVH7rScSxto8v6/XBYT1Ez
  7JOlMhD667/qvtFJtgIHkq7qzepbhnTv5x3tscQVnZY34/u9ILpD1s8dc+dibEvx
  6S/gYLVorB5ois/DLMqaobRcew6Gs+XX9RPwmLahOJpZ9mh4XrOmCgPAYtP71YM9
  ExpHCQKBgQCMMDDWGMRdFMJgXbx1uMere7OoniBdZaOexjbglRh1rMVSXqzBoU8+
  yhEuHGAsHGWQdSBHnqRe9O0Bj/Vlw2VVEaJeL1ewRHb+jXSnuKclZOJgMsJAvgGm
  SOWIahDrATA4g1T6yLBWQPhj3ZXD3eCMxT1Q3DvpG1DjgvXwmXQJAA==
  -----END RSA PRIVATE KEY-----
`)

				It("returns a valid valid certificate", func() {
					expectedCertificate, err := tls.LoadX509KeyPair("../test/assets/public.pem", "../test/assets/private.pem")
					Expect(err).ToNot(HaveOccurred())

					config.Initialize(b)
					Expect(config.EnableSSL).To(Equal(true))

					config.Process()
					Expect(config.SSLCertificate).To(Equal(expectedCertificate))
				})
			})

			Context("When it is given invalid values for a certificate", func() {
				var b = []byte(`
enable_ssl: true
ssl_cert: |
  -----BEGIN CERTIFICATE-----
  bad data
  -----END CERTIFICATE-----
ssl_key: |
  -----BEGIN RSA PRIVATE KEY-----
  worse data
  -----END RSA PRIVATE KEY-----
`)

				It("fails to create the certificate and panics", func() {
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
