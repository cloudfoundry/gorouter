package router_test

import (
	"crypto/tls"
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"syscall"
	"time"

	"github.com/cloudfoundry/gorouter/access_log"
	"github.com/cloudfoundry/gorouter/common/schema"
	cfg "github.com/cloudfoundry/gorouter/config"
	"github.com/cloudfoundry/gorouter/metrics/reporter/fakes"
	"github.com/cloudfoundry/gorouter/proxy"
	rregistry "github.com/cloudfoundry/gorouter/registry"
	"github.com/cloudfoundry/gorouter/route"
	. "github.com/cloudfoundry/gorouter/router"
	"github.com/cloudfoundry/gorouter/test"
	"github.com/cloudfoundry/gorouter/test_util"
	vvarz "github.com/cloudfoundry/gorouter/varz"
	"github.com/nats-io/nats"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/pivotal-golang/lager"
	"github.com/pivotal-golang/lager/lagertest"
)

var _ = Describe("Router", func() {
	var (
		logger     lager.Logger
		natsRunner *test_util.NATSRunner
		config     *cfg.Config

		mbusClient *nats.Conn
		registry   *rregistry.RouteRegistry
		varz       vvarz.Varz
		router     *Router
		natsPort   uint16
	)

	testAndVerifyRouterStopsNoDrain := func(signals chan os.Signal, closeChannel chan struct{}, sigs ...os.Signal) {
		app := test.NewTestApp([]route.Uri{"drain.vcap.me"}, config.Port, mbusClient, nil, "")
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

	runRouter := func(r *Router) (chan os.Signal, chan struct{}) {
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

	healthCheckReceives := func() int {
		url := fmt.Sprintf("http://%s:%d/", config.Ip, config.Port)
		req, _ := http.NewRequest("GET", url, nil)
		req.Header.Set("User-Agent", "HTTP-Monitor/1.1")

		client := http.Client{}
		resp, err := client.Do(req)
		Expect(err).ToNot(HaveOccurred())
		Expect(resp).ToNot(BeNil())
		defer resp.Body.Close()
		return resp.StatusCode
	}

	testRouterDrain := func(config *cfg.Config, mbusClient *nats.Conn, registry *rregistry.RouteRegistry, initiateDrain func()) {
		app := test.NewTestApp([]route.Uri{"drain.vcap.me"}, config.Port, mbusClient, nil, "")
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

		cert, err := tls.LoadX509KeyPair("../test/assets/public.pem", "../test/assets/private.pem")
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
		proxy := proxy.NewProxy(proxy.ProxyArgs{
			Logger:          logger,
			EndpointTimeout: config.EndpointTimeout,
			Ip:              config.Ip,
			TraceKey:        config.TraceKey,
			Registry:        registry,
			Reporter:        varz,
			AccessLogger:    &access_log.NullAccessLogger{},
		})

		errChan := make(chan error, 2)
		router, err = NewRouter(logger, config, proxy, mbusClient, registry, varz, logcounter, errChan)
		Expect(err).ToNot(HaveOccurred())
	})

	AfterEach(func() {
		if natsRunner != nil {
			natsRunner.Stop()
		}
	})

	Context("Drain", func() {
		BeforeEach(func() {
			runRouter(router)
		})

		AfterEach(func() {
			if router != nil {
				router.Stop()
			}
		})

		It("waits until the last request completes", func() {
			app := test.NewTestApp([]route.Uri{"drain.vcap.me"}, config.Port, mbusClient, nil, "")
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
				err := router.Drain(0, drainTimeout)
				Expect(err).ToNot(HaveOccurred())
				close(drainDone)
			}()

			Consistently(drainDone, drainTimeout/10).ShouldNot(BeClosed())

			blocker <- false

			Eventually(drainDone).Should(BeClosed())
			Eventually(clientDone).Should(BeClosed())
		})

		It("times out if it takes too long", func() {
			app := test.NewTestApp([]route.Uri{"draintimeout.vcap.me"}, config.Port, mbusClient, nil, "")

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
				err := router.Drain(0, 500*time.Millisecond)
				resultCh <- err
			}()

			var result error
			Eventually(resultCh).Should(Receive(&result))
			Expect(result).To(Equal(DrainTimeout))
		})

		Context("with http and https servers", func() {
			It("it drains and stops the router", func() {
				app := test.NewTestApp([]route.Uri{"drain.vcap.me"}, config.Port, mbusClient, nil, "")
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
					return healthCheckReceives()
				}, 100*time.Millisecond).Should(Equal(http.StatusOK))

				// wait for app to receive request
				<-blocker

				// check drain makes gorouter returns service unavailable
				go func() {
					defer GinkgoRecover()
					Eventually(func() int {
						result := healthCheckReceives()
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
						return healthCheckReceives()
					}, 500*time.Millisecond).Should(Equal(http.StatusServiceUnavailable))
				}()

				// trigger drain
				go func() {
					defer GinkgoRecover()
					err := router.Drain(drainWait, drainTimeout)
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

	Context("OnErrOrSignal", func() {
		Context("when an error is received in the error channel", func() {
			var errChan chan error

			BeforeEach(func() {
				logcounter := schema.NewLogCounter()
				proxy := proxy.NewProxy(proxy.ProxyArgs{
					Logger:          logger,
					EndpointTimeout: config.EndpointTimeout,
					Ip:              config.Ip,
					TraceKey:        config.TraceKey,
					Registry:        registry,
					Reporter:        varz,
					AccessLogger:    &access_log.NullAccessLogger{},
				})

				errChan = make(chan error, 2)
				var err error
				router, err = NewRouter(logger, config, proxy, mbusClient, registry, varz, logcounter, errChan)
				Expect(err).ToNot(HaveOccurred())
				runRouter(router)
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
				signals, _ = runRouter(router)
			})

			It("it drains and stops the router", func() {
				testRouterDrain(config, mbusClient, registry, func() {
					signals <- syscall.SIGUSR1
				})
			})
		})

		Context("when a SIGTERM signal is sent", func() {
			It("it drains and stops the router", func() {
				signals, closeChannel := runRouter(router)
				testAndVerifyRouterStopsNoDrain(signals, closeChannel, syscall.SIGTERM)
			})
		})

		Context("when a SIGINT signal is sent", func() {
			It("it drains and stops the router", func() {
				signals, closeChannel := runRouter(router)
				testAndVerifyRouterStopsNoDrain(signals, closeChannel, syscall.SIGINT)
			})
		})

		Context("when USR1 is the first of multiple signals sent", func() {
			var (
				signals chan os.Signal
			)

			BeforeEach(func() {
				signals, _ = runRouter(router)
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
				signals, closeChannel := runRouter(router)
				testAndVerifyRouterStopsNoDrain(signals, closeChannel, syscall.SIGINT, syscall.SIGUSR1)
			})
		})

		Context("when a non handlded signal is sent", func() {
			It("it drains and stops the router", func() {
				signals, closeChannel := runRouter(router)
				testAndVerifyRouterStopsNoDrain(signals, closeChannel, syscall.SIGUSR2)
			})
		})
	})
})
