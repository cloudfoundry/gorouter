package main_test

import (
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gbytes"
	"github.com/onsi/gomega/gexec"
)

var _ = Describe("RSS Cli", func() {
	var (
		session *gexec.Session
		err     error
	)

	Context("when no arguments are provided", func() {
		It("displays help", func() {
			command := rssCommand()
			session, err = gexec.Start(command, GinkgoWriter, GinkgoWriter)
			Eventually(session, "2s").Should(gexec.Exit(0))
			Eventually(session.Out).Should(gbytes.Say("rss - A CLI for generating"))
		})
	})

	Describe("Generate command", func() {
		Context("when no arguments are provided", func() {
			It("exits 1 and displays help", func() {
				command := rssCommand("generate")
				session, err = gexec.Start(command, GinkgoWriter, GinkgoWriter)
				Expect(err).NotTo(HaveOccurred())
				Eventually(session).Should(gexec.Exit(1))
				Eventually(session.Out).Should(gbytes.Say("generate - Generates a Route Service Signature"))
			})
		})

		Context("when url argument is provided", func() {
			Context("when key argument is not provided and default key exists", func() {
				It("generates a signature with the current time", func() {
					command := rssCommand("generate", "-u", "http://some-url.com")
					session, err = gexec.Start(command, GinkgoWriter, GinkgoWriter)
					Eventually(session, "2s").Should(gexec.Exit(0))
					Eventually(session.Out).Should(gbytes.Say("Encoded Signature:"))
					Eventually(session.Out).Should(gbytes.Say("Encoded Metadata:"))
				})

				Context("when time argument is provided", func() {
					It("generates a signature with the provided time", func() {
						command := rssCommand("generate", "-u", "http://some-url.com", "-t", "1439416554")
						session, err = gexec.Start(command, GinkgoWriter, GinkgoWriter)
						Eventually(session, "2s").Should(gexec.Exit(0))
						Eventually(session.Out).Should(gbytes.Say("Encoded Signature:"))
						Eventually(session.Out).Should(gbytes.Say("Encoded Metadata:"))
					})
				})
			})

			Context("when key argument is provided", func() {
				Context("when the key file does not exist", func() {
					It("displays unable to read key file error", func() {
						command := rssCommand("generate", "-u", "http://some-url.com", "-k", "fixtures/doesntexist")
						session, err = gexec.Start(command, GinkgoWriter, GinkgoWriter)
						Eventually(session).Should(gexec.Exit(1))
						Eventually(session.Out).Should(gbytes.Say("Unable to read key file"))
					})
				})

				Context("when the key file exists", func() {
					Context("when the key is valid", func() {
						It("generates a signature with the current time", func() {
							command := rssCommand("generate", "-u", "http://some-url.com", "-k", "fixtures/key")
							session, err = gexec.Start(command, GinkgoWriter, GinkgoWriter)
							Eventually(session, "2s").Should(gexec.Exit(0))
							Eventually(session.Out).Should(gbytes.Say("Encoded Signature:"))
							Eventually(session.Out).Should(gbytes.Say("Encoded Metadata:"))
						})
					})

				})
			})
		})
	})

	Describe("Read command", func() {
		Context("when no arguments are provided", func() {
			It("exits 1 and displays help", func() {
				command := rssCommand("read")
				session, err = gexec.Start(command, GinkgoWriter, GinkgoWriter)
				Expect(err).NotTo(HaveOccurred())
				Eventually(session).Should(gexec.Exit(1))
				Eventually(session.Out).Should(gbytes.Say("read - Decodes and decrypts a route service signature"))
			})
		})

		Context("when both signature and metadata is provided", func() {
			var (
				sig  string
				meta string
			)

			BeforeEach(func() {
				// generated using fixture/key file
				sig = "_RArsyg5lPJSfzcstt6sYJVl5J7RsGedUkrIVBaOY7Vm0Or1l9OdgdEbf1k6FfHI0-ij6YtuA0-hAqxSETZlhHLg6XtlV8Ff3C_STSOzhbKpS_YBD_elxfqlTfyrxv_vNA=="
				meta = "eyJub25jZSI6IjN6SFNYbCtPUlJ3YzNjaWQifQ=="
			})

			Context("when key argument is not provided and default key exists", func() {
				It("prints the signature", func() {
					command := rssCommand("read", "-s", sig, "-m", meta)
					session, err = gexec.Start(command, GinkgoWriter, GinkgoWriter)
					Expect(err).NotTo(HaveOccurred())
					Eventually(session, "2s").Should(gexec.Exit(0))
					Eventually(session.Out).Should(gbytes.Say("Decoded Signature"))
				})
			})

			Context("when key argument is provided", func() {
				Context("when the key is the same as the key used to encrypt signature", func() {
					It("prints the signature", func() {
						command := rssCommand("read", "-k", "fixtures/key", "-s", sig, "-m", meta)
						session, err = gexec.Start(command, GinkgoWriter, GinkgoWriter)
						Expect(err).NotTo(HaveOccurred())
						Eventually(session, "2s").Should(gexec.Exit(0))
						Eventually(session.Out).Should(gbytes.Say("Decoded Signature"))
					})
				})

				Context("when the key is different from the key used to encrypt signature", func() {
					It("prints the signature", func() {
						command := rssCommand("read", "-k", "fixtures/otherkey", "-s", sig, "-m", meta)
						session, err = gexec.Start(command, GinkgoWriter, GinkgoWriter)
						Expect(err).NotTo(HaveOccurred())
						Eventually(session).Should(gexec.Exit(1))
						Eventually(session.Out).Should(gbytes.Say("Failed to read signature"))
					})
				})
			})
		})
	})
})
