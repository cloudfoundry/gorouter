package watchdog_test

import (
	"context"
	"net"
	"net/http"
	"time"

	"code.cloudfoundry.org/gorouter/healthchecker/watchdog"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("Watchdog", func() {
	var (
		srv                http.Server
		httpHandler        http.ServeMux
		dog                *watchdog.Watchdog
		addr               string
		pollInterval       time.Duration
		healthcheckTimeout time.Duration
	)

	BeforeEach(func() {
		httpHandler = *http.NewServeMux()
		addr = "localhost:8888"
		pollInterval = 10 * time.Millisecond
		healthcheckTimeout = 5 * time.Millisecond
		srv = http.Server{
			Addr:    addr,
			Handler: &httpHandler,
		}
		go func() {
			defer GinkgoRecover()
			srv.ListenAndServe()
		}()
		Eventually(func() error {
			_, err := net.Dial("tcp", addr)
			return err
		}).Should(Not(HaveOccurred()))

	})

	JustBeforeEach(func() {
		dog, _ = watchdog.NewWatchdog("http://"+addr, pollInterval, healthcheckTimeout)
	})

	AfterEach(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond)
		defer cancel()
		srv.Shutdown(ctx)
		srv.Close()
	})

	It("does not return an error if the endpoint responds with a 200", func() {
		httpHandler.HandleFunc("/healthz", func(rw http.ResponseWriter, r *http.Request) {
			rw.WriteHeader(http.StatusOK)
			r.Close = true
		})

		err := dog.HitHealthcheckEndpoint()

		// Consistently(errorChannel).ShouldNot(Receive())
		// Consistently(errorChannel).ShouldNot(BeClosed())
		// Consistently(len(errorChannel)).Should(BeNumerically("==", 0))
		// Consistently(func() error { return <-errorChannel }).ShouldNot(HaveOccurred())
		Expect(err).NotTo(HaveOccurred())
	})

	It("returns an error if the endpoint does not respond with a 200", func() {
		httpHandler.HandleFunc("/healthz", func(rw http.ResponseWriter, r *http.Request) {
			rw.WriteHeader(http.StatusServiceUnavailable)
			r.Close = true
		})

		err := dog.HitHealthcheckEndpoint()

		// Eventually(errorChannel).Should(Receive())
		// Eventually(errorChannel).Should(BeClosed())
		Expect(err).To(HaveOccurred())
	})


	Context("the endpoint does not respond in the configured timeout", func() {
		BeforeEach(func() {
			healthcheckTimeout = 5 * time.Millisecond
			httpHandler.HandleFunc("/healthz", func(rw http.ResponseWriter, r *http.Request) {
				time.Sleep(5 * healthcheckTimeout)
				rw.WriteHeader(http.StatusOK)
				r.Close = true
			})
		})

		It("returns an error", func() {
			ctx, cancel := context.WithTimeout(context.Background(), 100*healthcheckTimeout)
			defer cancel()
			err := dog.WatchHealthcheckEndpoint(ctx)
			Expect(err).To(HaveOccurred())
		})
	})

	Context("the healthcheck passes repeatedly", func() {
		BeforeEach(func() {
			httpHandler.HandleFunc("/healthz", func(rw http.ResponseWriter, r *http.Request) {
				rw.WriteHeader(http.StatusOK)
				r.Close = true
			})
		})

		It("does not return an error", func() {
			ctx, cancel := context.WithTimeout(context.Background(), 10*pollInterval)
			defer cancel()
			err := dog.WatchHealthcheckEndpoint(ctx)
			Expect(err).NotTo(HaveOccurred())
		})
	})

	Context("the healthcheck first passes, and subsequently fails", func() {
		BeforeEach(func() {
			var visitCount int
			httpHandler.HandleFunc("/healthz", func(rw http.ResponseWriter, r *http.Request) {
				if visitCount == 0 {
					rw.WriteHeader(http.StatusOK)
				} else {
					rw.WriteHeader(http.StatusNotAcceptable)
				}
				r.Close = true
				visitCount++
			})
		})

		It("returns an error", func() {
			err := dog.WatchHealthcheckEndpoint(context.Background())
			Expect(err).To(HaveOccurred())
		})
	})

	Context("context is canceled", func() {
		var ctx context.Context
		var visitCount int

		BeforeEach(func() {
			var cancel context.CancelFunc
			ctx, cancel = context.WithCancel(context.Background())
			httpHandler.HandleFunc("/healthz", func(rw http.ResponseWriter, r *http.Request) {
				rw.WriteHeader(http.StatusOK)
				r.Close = true
				visitCount++
				if visitCount == 3 {
					cancel()
				}
			})
		})

		It("stops the healthchecker", func() {
			err := dog.WatchHealthcheckEndpoint(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(visitCount).To(Equal(3))
		})
	})
})
