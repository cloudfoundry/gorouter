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
		healthListener *HealthListener
		addr           string
		req            *http.Request
		port           uint16
		h              *health.Health
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
	})

	AfterEach(func() {
		healthListener.Stop()
	})

	JustBeforeEach(func() {
		err := healthListener.ListenAndServe()
		Expect(err).ToNot(HaveOccurred())

		req, err = http.NewRequest("GET", fmt.Sprintf("http://%s:%d/health", addr, port), nil)
		Expect(err).ToNot(HaveOccurred())
	})

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
	})
})
