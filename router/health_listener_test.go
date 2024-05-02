package router

import (
	"crypto/tls"
	"fmt"
	"io"
	"net/http"

	"code.cloudfoundry.org/gorouter/common/health"
	"code.cloudfoundry.org/gorouter/handlers"
	"code.cloudfoundry.org/gorouter/test_util"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("HealthListener", func() {
	var (
		healthListener  *HealthListener
		addr            string
		healthcheckPath string
		req             *http.Request
		port            uint16
		h               *health.Health
	)

	BeforeEach(func() {
		port = test_util.NextAvailPort()
		addr = "127.0.0.1"
		h = &health.Health{}
		h.SetHealth(health.Healthy)

		healthListener = &HealthListener{
			Port:        port,
			HealthCheck: handlers.NewHealthcheck(h, test_util.NewTestZapLogger("test")),
		}
		healthcheckPath = "health"
	})

	AfterEach(func() {
		healthListener.Stop()
	})

	JustBeforeEach(func() {
		err := healthListener.ListenAndServe()
		Expect(err).ToNot(HaveOccurred())

		req, err = http.NewRequest("GET", fmt.Sprintf("http://%s:%d/%s", addr, port, healthcheckPath), nil)
		Expect(err).ToNot(HaveOccurred())
	})

	Context("the always-healthy process healthcheck", func() {
		BeforeEach(func() {
			healthcheckPath = "is-process-alive-do-not-use-for-loadbalancing"
		})
		It("returns healthy", func() {
			resp, err := http.DefaultClient.Do(req)
			Expect(err).ToNot(HaveOccurred())
			Expect(resp).ToNot(BeNil())

			Expect(resp.StatusCode).To(Equal(200))

			body, err := io.ReadAll(resp.Body)
			defer resp.Body.Close()
			Expect(err).ToNot(HaveOccurred())
			Expect(string(body)).To(Equal("ok\n"))
		})
	})

	Context("the healthcheck used by LBs to delay service for request draining + route table population", func() {
		It("returns the LB healthiness", func() {
			resp, err := http.DefaultClient.Do(req)
			Expect(err).ToNot(HaveOccurred())
			Expect(resp).ToNot(BeNil())

			Expect(resp.StatusCode).To(Equal(200))

			body, err := io.ReadAll(resp.Body)
			defer resp.Body.Close()
			Expect(err).ToNot(HaveOccurred())
			Expect(string(body)).To(Equal("ok\n"))
		})
		Context("when the health should be down", func() {
			BeforeEach(func() {
				h.SetHealth(health.Degraded)
			})
			It("returns unhealthiness of endpoint", func() {
				resp, err := http.DefaultClient.Do(req)
				Expect(err).ToNot(HaveOccurred())
				Expect(resp).ToNot(BeNil())

				Expect(resp.StatusCode).To(Equal(http.StatusServiceUnavailable))

				body, err := io.ReadAll(resp.Body)
				defer resp.Body.Close()
				Expect(err).ToNot(HaveOccurred())
				Expect(string(body)).To(BeEmpty())
			})
		})
	})
	It("stops listening", func() {
		healthListener.Stop()
		resp, err := http.DefaultClient.Do(req)
		Expect(err).To(HaveOccurred())
		Expect(err).To(MatchError(ContainSubstring(fmt.Sprintf("dial tcp 127.0.0.1:%d: connect: connection refused", port))))
		Expect(resp).To(BeNil())
	})
	Context("when TLS is provided", func() {
		BeforeEach(func() {
			healthListener.TLSConfig = &tls.Config{
				Certificates: []tls.Certificate{test_util.CreateCert("default")},
			}
		})
		JustBeforeEach(func() {
			var err error
			req, err = http.NewRequest("GET", fmt.Sprintf("https://%s:%d/health", addr, port), nil)
			Expect(err).ToNot(HaveOccurred())
		})

		It("listens as an https listener", func() {
			tr := &http.Transport{
				TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
			}
			client := http.Client{Transport: tr}
			resp, err := client.Do(req)
			Expect(err).ToNot(HaveOccurred())
			Expect(resp).ToNot(BeNil())

			Expect(resp.StatusCode).To(Equal(200))

			body, err := io.ReadAll(resp.Body)
			defer resp.Body.Close()
			Expect(err).ToNot(HaveOccurred())
			Expect(string(body)).To(Equal("ok\n"))
		})
		Context("when using non-standard TLS Ciphers", func() {
			BeforeEach(func() {
				healthListener.TLSConfig = &tls.Config{
					Certificates: []tls.Certificate{test_util.CreateCert("default")},
					CipherSuites: []uint16{tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384, tls.TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384},
				}
			})

			It("handles different ciphers gracefully", func() {
				tr := &http.Transport{
					TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
				}
				client := http.Client{Transport: tr}
				resp, err := client.Do(req)
				Expect(err).ToNot(HaveOccurred())
				Expect(resp).ToNot(BeNil())

				Expect(resp.StatusCode).To(Equal(200))

				body, err := io.ReadAll(resp.Body)
				defer resp.Body.Close()
				Expect(err).ToNot(HaveOccurred())
				Expect(string(body)).To(Equal("ok\n"))
			})
		})
	})
})
