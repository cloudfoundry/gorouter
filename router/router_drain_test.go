package router_test

import (
	"crypto/tls"
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"sync/atomic"
	"syscall"
	"time"

	"code.cloudfoundry.org/gorouter/access_log"
	"code.cloudfoundry.org/gorouter/common/schema"
	cfg "code.cloudfoundry.org/gorouter/config"
	"code.cloudfoundry.org/gorouter/metrics/reporter/fakes"
	"code.cloudfoundry.org/gorouter/proxy"
	rregistry "code.cloudfoundry.org/gorouter/registry"
	"code.cloudfoundry.org/gorouter/route"
	"code.cloudfoundry.org/gorouter/router"
	"code.cloudfoundry.org/gorouter/test/common"
	"code.cloudfoundry.org/gorouter/test_util"
	vvarz "code.cloudfoundry.org/gorouter/varz"
	"code.cloudfoundry.org/lager"
	"code.cloudfoundry.org/lager/lagertest"
	"github.com/nats-io/nats"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("Router", func() {
	var (
		logger     lager.Logger
		natsRunner *test_util.NATSRunner
		config     *cfg.Config
		p          proxy.Proxy

		mbusClient  *nats.Conn
		registry    *rregistry.RouteRegistry
		varz        vvarz.Varz
		rtr         *router.Router
		natsPort    uint16
		healthCheck int32
	)

	testAndVerifyRouterStopsNoDrain := func(signals chan os.Signal, closeChannel chan struct{}, sigs ...os.Signal) {
		app := common.NewTestApp([]route.Uri{"drain.vcap.me"}, config.Port, mbusClient, nil, "")
		blocker := make(chan bool)
		resultCh := make(chan bool, 2)
		app.AddHandler("/", func(w http.ResponseWriter, r *http.Request) {
			blocker <- true

			_, err := ioutil.ReadAll(r.Body)
			defer r.Body.Close()
			Expect(err).ToNot(HaveOccurred())

			<-blocker

			w.WriteHeader(http.StatusNoContent)
		})

		app.Listen()

		Eventually(func() bool {
			return appRegistered(registry, app)
		}).Should(BeTrue())

		go func() {
			defer GinkgoRecover()
			req, err := http.NewRequest("GET", app.Endpoint(), nil)
			Expect(err).ToNot(HaveOccurred())

			client := http.Client{}
			resp, err := client.Do(req)
			Expect(err).ToNot(HaveOccurred())
			Expect(resp).ToNot(BeNil())
			Expect(resp.StatusCode).ToNot(Equal(http.StatusNoContent))
			defer resp.Body.Close()
			resultCh <- false
		}()

		<-blocker

		go func() {
			for _, s := range sigs {
				signals <- s
			}
		}()

		Eventually(closeChannel).Should(BeClosed())

		var result bool
		Eventually(resultCh).Should(Receive(&result))
		Expect(result).To(BeFalse())

		blocker <- false
	}

	runRouter := func(r *router.Router) (chan os.Signal, chan struct{}) {
		signals := make(chan os.Signal)
		readyChan := make(chan struct{})
		closeChannel := make(chan struct{})
		go func() {
			r.Run(signals, readyChan)
			close(closeChannel)
		}()
		select {
		case <-readyChan:
		}
		return signals, closeChannel
	}

	healthCheckWithEndpointReceives := func() int {
		url := fmt.Sprintf("http://%s:%d/health", config.Ip, config.Status.Port)
		req, _ := http.NewRequest("GET", url, nil)

		client := http.Client{}
		resp, err := client.Do(req)
		Expect(err).ToNot(HaveOccurred())
		defer resp.Body.Close()
		return resp.StatusCode
	}

	testRouterDrain := func(config *cfg.Config, mbusClient *nats.Conn, registry *rregistry.RouteRegistry, initiateDrain func()) {
		app := common.NewTestApp([]route.Uri{"drain.vcap.me"}, config.Port, mbusClient, nil, "")
		blocker := make(chan bool)
		resultCh := make(chan bool, 2)
		app.AddHandler("/", func(w http.ResponseWriter, r *http.Request) {
			blocker <- true

			_, err := ioutil.ReadAll(r.Body)
			defer r.Body.Close()
			Expect(err).ToNot(HaveOccurred())

			<-blocker

			w.WriteHeader(http.StatusNoContent)
		})

		app.Listen()

		Eventually(func() bool {
			return appRegistered(registry, app)
		}).Should(BeTrue())

		drainTimeout := 1 * time.Second

		go func() {
			defer GinkgoRecover()
			req, err := http.NewRequest("GET", app.Endpoint(), nil)
			Expect(err).ToNot(HaveOccurred())

			client := http.Client{}
			resp, err := client.Do(req)
			Expect(err).ToNot(HaveOccurred())
			Expect(resp).ToNot(BeNil())
			defer resp.Body.Close()
			_, err = ioutil.ReadAll(resp.Body)
			Expect(err).ToNot(HaveOccurred())
			resultCh <- false
		}()

		<-blocker

		go initiateDrain()

		Consistently(resultCh, drainTimeout/10).ShouldNot(Receive())

		blocker <- false

		var result bool
		Eventually(resultCh).Should(Receive(&result))
		Expect(result).To(BeFalse())

		req, err := http.NewRequest("GET", app.Endpoint(), nil)
		Expect(err).ToNot(HaveOccurred())

		client := http.Client{}
		_, err = client.Do(req)
		Expect(err).To(HaveOccurred())
	}

	BeforeEach(func() {
		logger = lagertest.NewTestLogger("test")
		natsPort = test_util.NextAvailPort()
		natsRunner = test_util.NewNATSRunner(int(natsPort))
		natsRunner.Start()

		proxyPort := test_util.NextAvailPort()
		statusPort := test_util.NextAvailPort()

		sslPort := test_util.NextAvailPort()

		cert, err := tls.LoadX509KeyPair("../test/assets/certs/server.pem", "../test/assets/certs/server.key")
		Expect(err).ToNot(HaveOccurred())

		config = test_util.SpecConfig(statusPort, proxyPort, natsPort)
		config.EnableSSL = true
		config.SSLPort = sslPort
		config.SSLCertificate = cert
		config.CipherSuites = []uint16{tls.TLS_RSA_WITH_AES_256_CBC_SHA}
		config.EndpointTimeout = 5 * time.Second

		mbusClient = natsRunner.MessageBus
		registry = rregistry.NewRouteRegistry(logger, config, new(fakes.FakeRouteRegistryReporter))
		varz = vvarz.NewVarz(registry)
		logcounter := schema.NewLogCounter()
		atomic.StoreInt32(&healthCheck, 0)
		p = proxy.NewProxy(proxy.ProxyArgs{
			Logger:               logger,
			EndpointTimeout:      config.EndpointTimeout,
			Ip:                   config.Ip,
			TraceKey:             config.TraceKey,
			Registry:             registry,
			Reporter:             varz,
			AccessLogger:         &access_log.NullAccessLogger{},
			HealthCheckUserAgent: "HTTP-Monitor/1.1",
			HeartbeatOK:          &healthCheck,
		})

		errChan := make(chan error, 2)
		rtr, err = router.NewRouter(logger, config, p, mbusClient, registry, varz, &healthCheck, logcounter, errChan)
		Expect(err).ToNot(HaveOccurred())
	})

	AfterEach(func() {
		if natsRunner != nil {
			natsRunner.Stop()
		}
	})

	Context("Drain", func() {
		BeforeEach(func() {
			runRouter(rtr)
		})

		AfterEach(func() {
			if rtr != nil {
				rtr.Stop()
			}
		})

		It("waits until the last request completes", func() {
			app := common.NewTestApp([]route.Uri{"drain.vcap.me"}, config.Port, mbusClient, nil, "")
			blocker := make(chan bool)
			drainDone := make(chan struct{})
			clientDone := make(chan struct{})

			app.AddHandler("/", func(w http.ResponseWriter, r *http.Request) {
				blocker <- true

				_, err := ioutil.ReadAll(r.Body)
				defer r.Body.Close()
				Expect(err).ToNot(HaveOccurred())

				<-blocker

				w.WriteHeader(http.StatusNoContent)
			})

			app.Listen()

			Eventually(func() bool {
				return appRegistered(registry, app)
			}).Should(BeTrue())

			drainTimeout := 1 * time.Second

			go func() {
				defer GinkgoRecover()
				req, err := http.NewRequest("GET", app.Endpoint(), nil)
				Expect(err).ToNot(HaveOccurred())

				client := http.Client{}
				resp, err := client.Do(req)
				Expect(err).ToNot(HaveOccurred())
				Expect(resp).ToNot(BeNil())
				defer resp.Body.Close()
				_, err = ioutil.ReadAll(resp.Body)
				Expect(err).ToNot(HaveOccurred())
				close(clientDone)
			}()

			<-blocker
			go func() {
				defer GinkgoRecover()
				err := rtr.Drain(0, drainTimeout)
				Expect(err).ToNot(HaveOccurred())
				close(drainDone)
			}()

			Consistently(drainDone, drainTimeout/10).ShouldNot(BeClosed())

			blocker <- false

			Eventually(drainDone).Should(BeClosed())
			Eventually(clientDone).Should(BeClosed())
		})

		It("times out if it takes too long", func() {
			app := common.NewTestApp([]route.Uri{"draintimeout.vcap.me"}, config.Port, mbusClient, nil, "")

			blocker := make(chan bool)
			resultCh := make(chan error, 2)
			app.AddHandler("/", func(w http.ResponseWriter, r *http.Request) {
				blocker <- true

				_, err := ioutil.ReadAll(r.Body)
				defer r.Body.Close()
				Expect(err).ToNot(HaveOccurred())

				time.Sleep(1 * time.Second)
			})
			app.Listen()

			Eventually(func() bool {
				return appRegistered(registry, app)
			}).Should(BeTrue())

			go func() {
				defer GinkgoRecover()
				req, err := http.NewRequest("GET", app.Endpoint(), nil)
				Expect(err).ToNot(HaveOccurred())

				client := http.Client{}
				resp, err := client.Do(req)
				Expect(err).ToNot(HaveOccurred())
				Expect(resp).ToNot(BeNil())
				defer resp.Body.Close()
			}()

			<-blocker

			go func() {
				defer GinkgoRecover()
				err := rtr.Drain(0, 500*time.Millisecond)
				resultCh <- err
			}()

			var result error
			Eventually(resultCh).Should(Receive(&result))
			Expect(result).To(Equal(router.DrainTimeout))
		})

		Context("with http and https servers", func() {
			It("it drains and stops the router", func() {
				app := common.NewTestApp([]route.Uri{"drain.vcap.me"}, config.Port, mbusClient, nil, "")
				blocker := make(chan bool)
				drainDone := make(chan struct{})
				clientDone := make(chan struct{})
				serviceUnavailable := make(chan bool)

				app.AddHandler("/", func(w http.ResponseWriter, r *http.Request) {
					blocker <- true

					_, err := ioutil.ReadAll(r.Body)
					defer r.Body.Close()
					Expect(err).ToNot(HaveOccurred())

					<-blocker

					w.WriteHeader(http.StatusNoContent)
				})

				app.Listen()

				Eventually(func() bool {
					return appRegistered(registry, app)
				}).Should(BeTrue())

				drainWait := 1 * time.Second
				drainTimeout := 1 * time.Second

				go func() {
					defer GinkgoRecover()
					httpsUrl := fmt.Sprintf("https://%s:%d", app.Urls()[0], config.SSLPort)
					req, err := http.NewRequest("GET", httpsUrl, nil)
					Expect(err).NotTo(HaveOccurred())

					client := http.Client{
						Transport: &http.Transport{
							TLSClientConfig: &tls.Config{
								InsecureSkipVerify: true,
							},
						},
					}
					resp, err := client.Do(req)
					Expect(err).NotTo(HaveOccurred())
					Expect(resp).ToNot(BeNil())
					defer resp.Body.Close()
					_, err = ioutil.ReadAll(resp.Body)
					Expect(err).ToNot(HaveOccurred())
					close(clientDone)
				}()

				// check for ok health
				Consistently(func() int {
					return healthCheckWithEndpointReceives()
				}, 100*time.Millisecond).Should(Equal(http.StatusOK))

				// wait for app to receive request
				<-blocker

				// check drain makes gorouter returns service unavailable
				go func() {
					defer GinkgoRecover()
					Eventually(func() int {
						result := healthCheckWithEndpointReceives()
						if result == http.StatusServiceUnavailable {
							serviceUnavailable <- true
						}
						return result
					}, 100*time.Millisecond).Should(Equal(http.StatusServiceUnavailable))
				}()

				// check that we can still connect within drainWait time
				go func() {
					defer GinkgoRecover()
					<-serviceUnavailable
					Consistently(func() int {
						return healthCheckWithEndpointReceives()
					}, 500*time.Millisecond).Should(Equal(http.StatusServiceUnavailable))
				}()

				// trigger drain
				go func() {
					defer GinkgoRecover()
					err := rtr.Drain(drainWait, drainTimeout)
					Expect(err).ToNot(HaveOccurred())
					close(drainDone)
				}()

				Consistently(drainDone, drainTimeout/10).ShouldNot(BeClosed())

				// drain in progress, continue with current request
				blocker <- false

				Eventually(drainDone).Should(BeClosed())
				Eventually(clientDone).Should(BeClosed())
			})
		})
	})

	Context("healthcheck with endpoint", func() {
		Context("when load balancer threshold is greater than start delay ", func() {
			var errChan chan error

			BeforeEach(func() {
				var err error
				logcounter := schema.NewLogCounter()

				errChan = make(chan error, 2)
				config.LoadBalancerHealthyThreshold = 2 * time.Second
				config.Port = 8347
				rtr, err = router.NewRouter(logger, config, p, mbusClient, registry, varz, &healthCheck, logcounter, errChan)
				Expect(err).ToNot(HaveOccurred())
				runRouterHealthcheck := func(r *router.Router) {
					signals := make(chan os.Signal)
					readyChan := make(chan struct{})
					go func() {
						r.Run(signals, readyChan)
					}()

					Eventually(func() int {
						return healthCheckWithEndpointReceives()
					}, time.Second).Should(Equal(http.StatusOK))
					select {
					case <-readyChan:
					}
				}
				runRouterHealthcheck(rtr)
			})

			It("should return valid healthchecks", func() {
				app := common.NewTestApp([]route.Uri{"drain.vcap.me"}, config.Port, mbusClient, nil, "")
				blocker := make(chan bool)
				serviceUnavailable := make(chan bool)

				app.AddHandler("/", func(w http.ResponseWriter, r *http.Request) {
					blocker <- true

					_, err := ioutil.ReadAll(r.Body)
					defer r.Body.Close()
					Expect(err).ToNot(HaveOccurred())

					<-blocker

					w.WriteHeader(http.StatusNoContent)
				})

				app.Listen()

				Eventually(func() bool {
					return appRegistered(registry, app)
				}).Should(BeTrue())

				drainWait := 1 * time.Second
				drainTimeout := 2 * time.Second

				go func() {
					defer GinkgoRecover()
					req, err := http.NewRequest("GET", app.Endpoint(), nil)
					Expect(err).ToNot(HaveOccurred())

					client := http.Client{}
					resp, err := client.Do(req)
					Expect(err).ToNot(HaveOccurred())
					Expect(resp).ToNot(BeNil())
					defer resp.Body.Close()
				}()

				// check for ok health
				Consistently(func() int {
					return healthCheckWithEndpointReceives()
				}, 2*time.Second, 100*time.Millisecond).Should(Equal(http.StatusOK))

				// wait for app to receive request
				<-blocker

				go func() {
					err := rtr.Drain(drainWait, drainTimeout)
					Expect(err).ToNot(HaveOccurred())
				}()
				blocker <- false
				// check drain makes gorouter returns service unavailable
				go func() {
					defer GinkgoRecover()
					Eventually(func() int {
						result := healthCheckWithEndpointReceives()
						if result == http.StatusServiceUnavailable {
							serviceUnavailable <- true
						}
						return result
					}, 100*time.Millisecond, drainTimeout).Should(Equal(http.StatusServiceUnavailable))
				}()

			})
		})

		Context("when the load balancer delay is less than the start repsonse delay ", func() {
			BeforeEach(func() {
				var err error
				logcounter := schema.NewLogCounter()

				errChan := make(chan error, 2)
				config.LoadBalancerHealthyThreshold = 2 * time.Second
				config.StartResponseDelayInterval = 4 * time.Second
				config.Port = 9348
				rtr, err = router.NewRouter(logger, config, p, mbusClient, registry, varz, &healthCheck, logcounter, errChan)
				Expect(err).ToNot(HaveOccurred())

				signals := make(chan os.Signal)
				readyChan := make(chan struct{})
				go func() {
					rtr.Run(signals, readyChan)
				}()
			})

			It("does not immediately make the health check endpoint available", func() {
				Consistently(func() int {
					return healthCheckWithEndpointReceives()
				}, time.Second).Should(Equal(http.StatusServiceUnavailable))
				Eventually(func() int {
					return healthCheckWithEndpointReceives()
				}, 4*time.Second).Should(Equal(http.StatusOK))

			})
		})

	})

	Context("OnErrOrSignal", func() {
		Context("when an error is received in the error channel", func() {
			var errChan chan error

			BeforeEach(func() {
				logcounter := schema.NewLogCounter()
				var healthCheck int32
				healthCheck = 0
				proxy := proxy.NewProxy(proxy.ProxyArgs{
					Logger:               logger,
					EndpointTimeout:      config.EndpointTimeout,
					Ip:                   config.Ip,
					TraceKey:             config.TraceKey,
					Registry:             registry,
					Reporter:             varz,
					AccessLogger:         &access_log.NullAccessLogger{},
					HealthCheckUserAgent: "HTTP-Moniter/1.1",
					HeartbeatOK:          &healthCheck,
				})

				errChan = make(chan error, 2)
				var err error
				rtr, err = router.NewRouter(logger, config, proxy, mbusClient, registry, varz, &healthCheck, logcounter, errChan)
				Expect(err).ToNot(HaveOccurred())
				runRouter(rtr)
			})

			It("it drains existing connections and stops the router", func() {
				testRouterDrain(config, mbusClient, registry, func() {
					errChan <- errors.New("Initiate drain error")
				})
			})
		})

		Context("when a USR1 signal is sent", func() {
			var (
				signals chan os.Signal
			)

			BeforeEach(func() {
				signals, _ = runRouter(rtr)
			})

			It("it drains and stops the router", func() {
				testRouterDrain(config, mbusClient, registry, func() {
					signals <- syscall.SIGUSR1
				})
			})
		})

		Context("when a SIGTERM signal is sent", func() {
			It("it drains and stops the router", func() {
				signals, closeChannel := runRouter(rtr)
				testAndVerifyRouterStopsNoDrain(signals, closeChannel, syscall.SIGTERM)
			})
		})

		Context("when a SIGINT signal is sent", func() {
			It("it drains and stops the router", func() {
				signals, closeChannel := runRouter(rtr)
				testAndVerifyRouterStopsNoDrain(signals, closeChannel, syscall.SIGINT)
			})
		})

		Context("when USR1 is the first of multiple signals sent", func() {
			var (
				signals chan os.Signal
			)

			BeforeEach(func() {
				signals, _ = runRouter(rtr)
			})

			It("it drains and stops the router", func() {
				testRouterDrain(config, mbusClient, registry, func() {
					signals <- syscall.SIGUSR1
					signals <- syscall.SIGUSR1
					signals <- syscall.SIGTERM
				})
			})
		})

		Context("when USR1 is not the first of multiple signals sent", func() {
			It("it does not drain and stops the router", func() {
				signals, closeChannel := runRouter(rtr)
				testAndVerifyRouterStopsNoDrain(signals, closeChannel, syscall.SIGINT, syscall.SIGUSR1)
			})
		})

		Context("when a non handlded signal is sent", func() {
			It("it drains and stops the router", func() {
				signals, closeChannel := runRouter(rtr)
				testAndVerifyRouterStopsNoDrain(signals, closeChannel, syscall.SIGUSR2)
			})
		})
	})
})
