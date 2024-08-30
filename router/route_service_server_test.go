package router_test

import (
	"net/http"

	"code.cloudfoundry.org/gorouter/config"
	"code.cloudfoundry.org/gorouter/router"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
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
		rss, err = router.NewRouteServicesServer(cfg)
		Expect(err).NotTo(HaveOccurred())

		errChan = make(chan error)

		Expect(rss.Serve(handler, errChan)).To(Succeed())

		req, err = http.NewRequest("GET", "/foo", nil)
		Expect(err).NotTo(HaveOccurred())
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
})
