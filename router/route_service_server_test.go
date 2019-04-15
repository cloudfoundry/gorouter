package router_test

import (
	"net/http"

	"code.cloudfoundry.org/gorouter/router"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("RouteServicesServer", func() {
	var (
		rss     *router.RouteServicesServer
		handler http.Handler
		errChan chan error
		req     *http.Request
	)

	BeforeEach(func() {
		handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusTeapot)
		})

		var err error
		rss, err = router.NewRouteServicesServer()
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
