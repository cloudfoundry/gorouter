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
		server  *http.Server
		errChan chan error
		req     *http.Request
	)

	BeforeEach(func() {
		server = &http.Server{
			Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusTeapot)
			}),
		}

		var err error
		rss, err = router.NewRouteServicesServer()
		Expect(err).NotTo(HaveOccurred())

		errChan = make(chan error)

		Expect(rss.Serve(server, errChan)).To(Succeed())

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

	Describe("ArrivedViaMe", func() {
		BeforeEach(func() {
			// create new rss with new server
			rss.Stop()
			Eventually(errChan).Should(Receive())
			var err error

			rss, err = router.NewRouteServicesServer()
			Expect(err).NotTo(HaveOccurred())

			server = &http.Server{
				Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if rss.ArrivedViaARouteServicesServer(r) {
						w.WriteHeader(200)
					} else {
						w.WriteHeader(401)
					}
				}),
			}
			Expect(rss.Serve(server, errChan)).To(Succeed())
		})

		It("returns true for requests that arrived via the RouteServicesServer", func() {
			resp, err := rss.GetRoundTripper().RoundTrip(req)
			Expect(err).NotTo(HaveOccurred())
			resp.Body.Close()

			Expect(resp.StatusCode).To(Equal(200))
		})

		It("returns false for requests that arrived via any other server", func() {
			otherRSS, err := router.NewRouteServicesServer()
			Expect(err).NotTo(HaveOccurred())

			otherErrChan := make(chan error)
			Expect(otherRSS.Serve(server, otherErrChan)).To(Succeed())

			resp, err := otherRSS.GetRoundTripper().RoundTrip(req)
			Expect(err).NotTo(HaveOccurred())
			resp.Body.Close()

			Expect(resp.StatusCode).To(Equal(401))

			otherRSS.Stop()
			Eventually(otherErrChan).Should(Receive())
		})

		It("returns false for requests that haven't transited any RouteServicesServer", func() {
			Expect(rss.ArrivedViaARouteServicesServer(req)).To(BeFalse())
		})
	})
})
