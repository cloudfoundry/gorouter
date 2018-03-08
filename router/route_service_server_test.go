package router_test

import (
	"net/http"

	"code.cloudfoundry.org/gorouter/router"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("RouteServicesServer", func() {
	var (
		rss          *router.RouteServicesServer
		server       *http.Server
		roundTripper router.RouteServiceRoundTripper
		errChan      chan error
	)

	Describe("Serve", func() {
		BeforeEach(func() {
			server = &http.Server{
				Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(http.StatusTeapot)
				}),
			}

			rss = router.NewRouteServicesServer()
			roundTripper = rss.GetRoundTripper()
			errChan = make(chan error)
		})

		AfterEach(func() {
			rss.Stop()
			Eventually(errChan).Should(Receive())
		})

		It("responds to a TLS request using the client cert", func() {
			err := rss.Serve(server, errChan)
			Expect(err).NotTo(HaveOccurred())

			req, err := http.NewRequest("GET", "/foo", nil)
			Expect(err).NotTo(HaveOccurred())

			resp, err := roundTripper.RoundTrip(req)
			Expect(err).NotTo(HaveOccurred())
			resp.Body.Close()

			Expect(resp.StatusCode).To(Equal(http.StatusTeapot))
		})

	})
})
