package watchdog_test

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"syscall"
	"time"

	"code.cloudfoundry.org/gorouter/healthchecker/watchdog"
	"code.cloudfoundry.org/gorouter/logger"
	"code.cloudfoundry.org/gorouter/test_util"
	"github.com/onsi/ginkgo"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("Watchdog", func() {
	var (
		srv                *http.Server
		dog                *watchdog.Watchdog
		addr               string
		pollInterval       time.Duration
		healthcheckTimeout time.Duration
		logger             logger.Logger
	)

	healthcheckTimeout = 5 * time.Millisecond
	runServer := func(httpHandler http.Handler) *http.Server {
		localSrv := http.Server{
			Addr:    addr,
			Handler: httpHandler,
		}
		go func() {
			defer GinkgoRecover()
			localSrv.ListenAndServe()
		}()
		Eventually(func() error {
			_, err := net.Dial("tcp", addr)
			return err
		}).Should(Not(HaveOccurred()))
		return &localSrv
	}

	BeforeEach(func() {
		addr = fmt.Sprintf("localhost:%d", 9850+ginkgo.GinkgoParallelNode())
		pollInterval = 10 * time.Millisecond
		logger = test_util.NewTestZapLogger("router-test")
	})

	JustBeforeEach(func() {
		dog = watchdog.NewWatchdog("http://"+addr, pollInterval, healthcheckTimeout, logger)
	})

	AfterEach(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond)
		defer cancel()
		srv.Shutdown(ctx)
		srv.Close()
	})

	Context("HitHealthcheckEndpoint", func() {
		var statusCode int
		BeforeEach(func() {
			httpHandler := http.NewServeMux()
			httpHandler.HandleFunc("/healthz", func(rw http.ResponseWriter, r *http.Request) {
				rw.WriteHeader(statusCode)
				r.Close = true
			})
			srv = runServer(httpHandler)
		})
		It("does not return an error if the endpoint responds with a 200", func() {
			statusCode = http.StatusOK
			err := dog.HitHealthcheckEndpoint()
			Expect(err).NotTo(HaveOccurred())
		})

		It("returns an error if the endpoint does not respond with a 200", func() {
			statusCode = http.StatusServiceUnavailable

			err := dog.HitHealthcheckEndpoint()
			Expect(err).To(HaveOccurred())
		})
	})

	Context("WatchHealthcheckEndpoint", func() {
		var signals chan os.Signal

		BeforeEach(func() {
			signals = make(chan os.Signal)
		})

		Context("the healthcheck passes repeatedly", func() {
			BeforeEach(func() {
				httpHandler := http.NewServeMux()
				httpHandler.HandleFunc("/healthz", func(rw http.ResponseWriter, r *http.Request) {
					rw.WriteHeader(http.StatusOK)
					r.Close = true
				})
				srv = runServer(httpHandler)
			})

			It("does not return an error", func() {
				ctx, cancel := context.WithTimeout(context.Background(), 10*pollInterval)
				defer cancel()
				err := dog.WatchHealthcheckEndpoint(ctx, signals)
				Expect(err).NotTo(HaveOccurred())
			})
		})

		Context("the healthcheck first passes, and subsequently fails", func() {
			BeforeEach(func() {
				var visitCount int
				httpHandler := http.NewServeMux()
				httpHandler.HandleFunc("/healthz", func(rw http.ResponseWriter, r *http.Request) {
					if visitCount == 0 {
						rw.WriteHeader(http.StatusOK)
					} else {
						rw.WriteHeader(http.StatusNotAcceptable)
					}
					r.Close = true
					visitCount++
				})
				srv = runServer(httpHandler)
			})

			It("returns an error", func() {
				err := dog.WatchHealthcheckEndpoint(context.Background(), signals)
				Expect(err).To(HaveOccurred())
			})
		})

		Context("the endpoint does not respond in the configured timeout", func() {
			BeforeEach(func() {
				httpHandler := http.NewServeMux()
				httpHandler.HandleFunc("/healthz", func(rw http.ResponseWriter, r *http.Request) {
					time.Sleep(5 * healthcheckTimeout)
					rw.WriteHeader(http.StatusOK)
					r.Close = true
				})
				srv = runServer(httpHandler)
			})

			It("returns an error", func() {
				ctx, cancel := context.WithTimeout(context.Background(), 100*healthcheckTimeout)
				defer cancel()
				err := dog.WatchHealthcheckEndpoint(ctx, signals)
				Expect(err).To(HaveOccurred())
			})
		})

		Context("context is canceled", func() {
			var ctx context.Context
			var visitCount int

			BeforeEach(func() {
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(context.Background())
				httpHandler := http.NewServeMux()
				httpHandler.HandleFunc("/healthz", func(rw http.ResponseWriter, r *http.Request) {
					rw.WriteHeader(http.StatusOK)
					r.Close = true
					visitCount++
					if visitCount == 3 {
						cancel()
					}
				})
				srv = runServer(httpHandler)
			})

			It("stops the healthchecker", func() {
				err := dog.WatchHealthcheckEndpoint(ctx, signals)
				Expect(err).NotTo(HaveOccurred())
				Expect(visitCount).To(Equal(3))
			})
		})

		Context("received USR1 signal", func() {
			var visitCount int

			BeforeEach(func() {
				httpHandler := http.NewServeMux()
				httpHandler.HandleFunc("/healthz", func(rw http.ResponseWriter, r *http.Request) {
					rw.WriteHeader(http.StatusOK)
					r.Close = true
					visitCount++
					if visitCount == 3 {
						go func() {
							signals <- syscall.SIGUSR1
						}()
					}
				})
				srv = runServer(httpHandler)
			})

			It("stops the healthchecker without an error", func() {
				err := dog.WatchHealthcheckEndpoint(context.Background(), signals)
				Expect(err).NotTo(HaveOccurred())
				Expect(visitCount).To(Equal(3))
			})
		})

		Context("gorouter exited before we received USR1 signal", func() {
			BeforeEach(func() {
				httpHandler := http.NewServeMux()
				httpHandler.HandleFunc("/healthz", func(rw http.ResponseWriter, r *http.Request) {
					rw.WriteHeader(http.StatusServiceUnavailable)
					r.Close = true
					go func() {
						signals <- syscall.SIGUSR1
					}()
				})
				srv = runServer(httpHandler)
			})

			It("stops the healthchecker without an error", func() {
				err := dog.WatchHealthcheckEndpoint(context.Background(), signals)
				Expect(err).NotTo(HaveOccurred())
			})
		})
	})
})
