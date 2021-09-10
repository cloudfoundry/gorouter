package watchdog_test

import (
	"context"
	"fmt"
	"net/http"

	"code.cloudfoundry.org/gorouter/healthchecker/watchdog"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("Watchdog", func() {
	var srv http.Server
	var httpHandler http.ServeMux
	BeforeEach(func() {
		httpHandler = *http.NewServeMux()
		srv = http.Server{
			Addr:    "localhost:8888",
			Handler: &httpHandler,
		}
		go srv.ListenAndServe()
		fmt.Fprintf(GinkgoWriter, "[DEBUG] OUTPUT LINE: %s\n", "setup")
	})

	AfterEach(func() {
		srv.Shutdown(context.Background())
		srv.Close()
	})

	It("does not allow use of an unbuffered channel", func() {
		errorChannel := make(chan error)

		dog, err := watchdog.NewWatchdog(errorChannel, "http://localhost:8888")

		Expect(dog).To(BeNil())
		Expect(err).To(HaveOccurred())
	})

	It("does not return an error if the endpoint does not respond with a 200", func() {
		errorChannel := make(chan error, 2)
		dog, _ := watchdog.NewWatchdog(errorChannel, "http://localhost:8888")
		httpHandler.HandleFunc("/healthz", func(rw http.ResponseWriter, r *http.Request) {
			rw.WriteHeader(http.StatusOK)
			r.Close = true
		})

		dog.HitHealthcheckEndpoint()

		Consistently(errorChannel).ShouldNot(Receive())
		Consistently(errorChannel).ShouldNot(BeClosed())
		Consistently(len(errorChannel)).Should(BeNumerically("==", 0))
		// Consistently(func() error { return <-errorChannel }).ShouldNot(HaveOccurred())
		// Expect(err).NotTo(HaveOccurred())
	})

	It("returns an error if the endpoint does not respond with a 200", func() {
		errorChannel := make(chan error, 2)
		dog, _ := watchdog.NewWatchdog(errorChannel, "http://localhost:8888")
		httpHandler.HandleFunc("/healthz", func(rw http.ResponseWriter, r *http.Request) {
			rw.WriteHeader(http.StatusServiceUnavailable)
			r.Close = true
		})

		dog.HitHealthcheckEndpoint()

		Eventually(errorChannel).Should(Receive())
		Eventually(errorChannel).Should(BeClosed())
		// Expect(err).To(HaveOccurred())
	})

	XIt("returns an error if the endpoint does not respond in the configured timeout", func() {

	})

	Context("the healthcheck first passes, and subsequently fails", func() {
		XIt("returns an error", func() {

		})
	})
})
