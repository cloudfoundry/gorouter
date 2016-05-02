package config_test

import (
	"time"

	"github.com/cloudfoundry-incubator/routing-api/config"
	"github.com/cloudfoundry-incubator/routing-api/models"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("Config", func() {
	Describe("NewConfigFromFile", func() {
		Context("when auth is enabled", func() {
			Context("when the file exists", func() {
				It("returns a valid Config struct", func() {
					cfg_file := "../example_config/example.yml"
					cfg, err := config.NewConfigFromFile(cfg_file, false)

					Expect(err).NotTo(HaveOccurred())
					Expect(cfg.LogGuid).To(Equal("my_logs"))
					Expect(cfg.MetronConfig.Address).To(Equal("1.2.3.4"))
					Expect(cfg.MetronConfig.Port).To(Equal("4567"))
					Expect(cfg.DebugAddress).To(Equal("1.2.3.4:1234"))
					Expect(cfg.StatsdClientFlushInterval).To(Equal(10 * time.Millisecond))
					Expect(cfg.OAuth.TokenEndpoint).To(Equal("localhost"))
					Expect(cfg.OAuth.Port).To(Equal(3000))
				})

				Context("when there is no token endpoint specified", func() {
					It("returns an error", func() {
						cfg_file := "../example_config/missing_uaa_url.yml"
						_, err := config.NewConfigFromFile(cfg_file, false)
						Expect(err).To(HaveOccurred())
					})
				})
			})

			Context("when the file does not exists", func() {
				It("returns an error", func() {
					cfg_file := "notexist"
					_, err := config.NewConfigFromFile(cfg_file, false)

					Expect(err).To(HaveOccurred())
				})
			})
		})

		Context("when auth is disabled", func() {
			Context("when the file exists", func() {
				It("returns a valid config", func() {
					cfg_file := "../example_config/example.yml"
					cfg, err := config.NewConfigFromFile(cfg_file, true)

					Expect(err).NotTo(HaveOccurred())
					Expect(cfg.LogGuid).To(Equal("my_logs"))
					Expect(cfg.MetronConfig.Address).To(Equal("1.2.3.4"))
					Expect(cfg.MetronConfig.Port).To(Equal("4567"))
					Expect(cfg.DebugAddress).To(Equal("1.2.3.4:1234"))
					Expect(cfg.StatsdClientFlushInterval).To(Equal(10 * time.Millisecond))
					Expect(cfg.OAuth.TokenEndpoint).To(Equal("localhost"))
					Expect(cfg.OAuth.Port).To(Equal(3000))
				})

				Context("when there is no token endpoint url", func() {
					It("returns a valid config", func() {
						cfg_file := "../example_config/missing_uaa_url.yml"
						cfg, err := config.NewConfigFromFile(cfg_file, true)

						Expect(err).NotTo(HaveOccurred())
						Expect(cfg.LogGuid).To(Equal("my_logs"))
						Expect(cfg.MetronConfig.Address).To(Equal("1.2.3.4"))
						Expect(cfg.MetronConfig.Port).To(Equal("4567"))
						Expect(cfg.DebugAddress).To(Equal("1.2.3.4:1234"))
						Expect(cfg.StatsdClientFlushInterval).To(Equal(10 * time.Millisecond))
						Expect(cfg.OAuth.TokenEndpoint).To(BeEmpty())
						Expect(cfg.OAuth.Port).To(Equal(0))
					})
				})
			})
		})
	})

	Describe("Initialize", func() {
		var (
			cfg *config.Config
		)

		BeforeEach(func() {
			cfg = &config.Config{}
		})

		Context("when router groups are seeded in the configuration file", func() {
			var expectedGroups models.RouterGroups

			testConfig := func(ports string) string {
				return `log_guid: "my_logs"
metrics_reporting_interval: "500ms"
statsd_endpoint: "localhost:8125"
statsd_client_flush_interval: "10ms"
router_groups:
- name: router-group-1
  reservable_ports: ` + ports + `
  type: tcp
- name: router-group-2
  reservable_ports: 1024-10000,42000
  type: udp`
			}

			It("populates the router groups", func() {
				config := testConfig("12000")
				err := cfg.Initialize([]byte(config), true)
				Expect(err).NotTo(HaveOccurred())
				expectedGroups = models.RouterGroups{
					{
						Name:            "router-group-1",
						ReservablePorts: "12000",
						Type:            "tcp",
					},
					{
						Name:            "router-group-2",
						ReservablePorts: "1024-10000,42000",
						Type:            "udp",
					},
				}
				Expect(cfg.RouterGroups).To(Equal(expectedGroups))
			})

			It("returns error for invalid ports", func() {
				config := testConfig("abc")
				err := cfg.Initialize([]byte(config), true)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("Port must be between 1024 and 65535"))
			})

			It("returns error for invalid port", func() {
				config := testConfig("70000")
				err := cfg.Initialize([]byte(config), true)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("Port must be between 1024 and 65535"))
			})

			It("returns error for invalid ranges of ports", func() {
				config := testConfig("1024-65535,10000-20000")
				err := cfg.Initialize([]byte(config), true)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("Overlapping values: [1024-65535] and [10000-20000]"))
			})

			It("returns error for invalid range of ports", func() {
				config := testConfig("1023-65530")
				err := cfg.Initialize([]byte(config), true)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("Port must be between 1024 and 65535"))
			})

			It("returns error for invalid start range", func() {
				config := testConfig("1024-65535,-10000")
				err := cfg.Initialize([]byte(config), true)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("range (-10000) requires a starting port"))
			})

			It("returns error for invalid end range", func() {
				config := testConfig("10000-")
				err := cfg.Initialize([]byte(config), true)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("range (10000-) requires an ending port"))
			})

			It("returns error for invalid router group type", func() {
				missingType := `log_guid: "my_logs"
metrics_reporting_interval: "500ms"
statsd_endpoint: "localhost:8125"
statsd_client_flush_interval: "10ms"
router_groups:
- name: router-group-1
  reservable_ports: 1024-65535`
				err := cfg.Initialize([]byte(missingType), true)
				Expect(err).To(HaveOccurred())
			})

			It("returns error for invalid router group type", func() {
				missingName := `log_guid: "my_logs"
metrics_reporting_interval: "500ms"
statsd_endpoint: "localhost:8125"
statsd_client_flush_interval: "10ms"
router_groups:
- type: tcp
  reservable_ports: 1024-65535`
				err := cfg.Initialize([]byte(missingName), true)
				Expect(err).To(HaveOccurred())
			})

			It("returns error for missing reservable port range", func() {
				missingRouterGroup := `log_guid: "my_logs"
metrics_reporting_interval: "500ms"
statsd_endpoint: "localhost:8125"
statsd_client_flush_interval: "10ms"
router_groups:
- type: tcp
  name: default-tcp`
				err := cfg.Initialize([]byte(missingRouterGroup), true)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("Missing `reservable_ports` in router group:"))
			})
		})

		Context("when there are errors in the yml file", func() {
			var test_config string
			Context("UAA errors", func() {
				BeforeEach(func() {
					test_config = `log_guid: "my_logs"
debug_address: "1.2.3.4:1234"
metron_config:
  address: "1.2.3.4"
  port: "4567"
metrics_reporting_interval: "500ms"
statsd_endpoint: "localhost:8125"
statsd_client_flush_interval: "10ms"`
				})

				Context("when auth is enabled", func() {
					It("errors if no token endpoint url is found", func() {
						err := cfg.Initialize([]byte(test_config), false)
						Expect(err).To(HaveOccurred())
					})
				})

				Context("when auth is disabled", func() {
					It("it return valid config", func() {
						err := cfg.Initialize([]byte(test_config), true)
						Expect(err).NotTo(HaveOccurred())
					})
				})
			})
		})
	})
})
