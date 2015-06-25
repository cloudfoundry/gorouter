package config_test

import (
	"time"

	"github.com/cloudfoundry-incubator/routing-api/config"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("Config", func() {
	Describe("NewConfigFromFile", func() {
		Context("when the file exists", func() {
			It("returns a valid Config struct", func() {
				cfg_file := "../example_config/example.yml"
				cfg, err := config.NewConfigFromFile(cfg_file)

				Expect(err).NotTo(HaveOccurred())
				Expect(cfg.LogGuid).To(Equal("my_logs"))
				Expect(cfg.MetronConfig.Address).To(Equal("1.2.3.4"))
				Expect(cfg.MetronConfig.Port).To(Equal("4567"))
				Expect(cfg.DebugAddress).To(Equal("1.2.3.4:1234"))
				Expect(cfg.MaxConcurrentETCDRequests).To(Equal((uint)(10)))
				Expect(cfg.UAAPublicKey).To(ContainSubstring("-----BEGIN PUBLIC KEY-----"))
				Expect(cfg.StatsdClientFlushInterval).To(Equal(10 * time.Millisecond))
			})
		})

		Context("when the file does not exists", func() {
			It("returns an error", func() {
				cfg_file := "notexist"
				_, err := config.NewConfigFromFile(cfg_file)

				Expect(err).To(HaveOccurred())
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

		Context("when there are errors in the yml file", func() {
			var test_config string
			Context("UAA errors", func() {
				BeforeEach(func() {
					test_config = `
					uaa:`
				})

				It("errors if no UaaPublicKey is found", func() {
					err := cfg.Initialize([]byte(test_config))
					Expect(err).To(HaveOccurred())
				})
			})

			Context("MaxConcurrentETCDRequests errors", func() {
				BeforeEach(func() {
					test_config = `uaa_verification_key: "public_key"
log_guid: "some-guid"
max_concurrent_etcd_requests: '-1'`
				})

				It("errors if max concurrent requests is negative", func() {
					err := cfg.Initialize([]byte(test_config))
					Expect(err).To(HaveOccurred())
				})
			})
		})
	})
})
