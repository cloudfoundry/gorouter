package router_test

import (
	"bufio"
	"crypto/tls"
	"fmt"
	"net/http"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"code.cloudfoundry.org/gorouter/config"
	"code.cloudfoundry.org/gorouter/router"
)

var _ = Describe("RouteServicesServer", func() {
	var (
		rss     *router.RouteServicesServer
		handler http.Handler
		errChan chan error
		req     *http.Request
		cfg     *config.Config
	)

	BeforeEach(func() {
		handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusTeapot)
		})

		var err error
		cfg, err = config.DefaultConfig()
		Expect(err).NotTo(HaveOccurred())

		req, err = http.NewRequest("GET", "/foo", nil)
		Expect(err).NotTo(HaveOccurred())
	})

	JustBeforeEach(func() {
		var err error
		rss, err = router.NewRouteServicesServer(cfg)
		Expect(err).NotTo(HaveOccurred())

		errChan = make(chan error)

		Expect(rss.Serve(handler, errChan)).To(Succeed())
	})

	AfterEach(func() {
		rss.Stop()
		Eventually(errChan).Should(Receive())
	})

	Describe("Serve", func() {
		It("responds to a TLS request using the client cert", func() {
			resp, err := rss.GetRoundTripper().RoundTrip(req)
			Expect(err).NotTo(HaveOccurred())
			resp.Body.Close()

			Expect(resp.StatusCode).To(Equal(http.StatusTeapot))
		})
	})

	Describe("ReadHeaderTimeout", func() {
		BeforeEach(func() {
			cfg.ReadHeaderTimeout = 100 * time.Millisecond
		})

		It("closes requests when their header write exceeds ReadHeaderTimeout", func() {
			roundTripper := rss.GetRoundTripper()
			conn, err := tls.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", cfg.RouteServicesServerPort), roundTripper.TLSClientConfig())
			Expect(err).NotTo(HaveOccurred())
			defer conn.Close()

			writer := bufio.NewWriter(conn)

			fmt.Fprintf(writer, "GET /some-request HTTP/1.1\r\n")

			// started writing headers
			fmt.Fprintf(writer, "Host: localhost\r\n")
			writer.Flush()

			time.Sleep(300 * time.Millisecond)

			fmt.Fprintf(writer, "User-Agent: CustomClient/1.0\r\n")
			writer.Flush()

			// done
			fmt.Fprintf(writer, "\r\n")
			writer.Flush()

			resp := bufio.NewReader(conn)
			_, err = resp.ReadString('\n')
			Expect(err).To(HaveOccurred())
		})
	})
})
