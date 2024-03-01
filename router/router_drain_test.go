package router_test

import (
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"syscall"
	"time"

	"code.cloudfoundry.org/gorouter/common/health"

	"code.cloudfoundry.org/gorouter/accesslog"
	"code.cloudfoundry.org/gorouter/common/schema"
	cfg "code.cloudfoundry.org/gorouter/config"
	"code.cloudfoundry.org/gorouter/errorwriter"
	sharedfakes "code.cloudfoundry.org/gorouter/fakes"
	"code.cloudfoundry.org/gorouter/logger"
	"code.cloudfoundry.org/gorouter/mbus"
	"code.cloudfoundry.org/gorouter/metrics"
	fakeMetrics "code.cloudfoundry.org/gorouter/metrics/fakes"
	"code.cloudfoundry.org/gorouter/proxy"
	rregistry "code.cloudfoundry.org/gorouter/registry"
	"code.cloudfoundry.org/gorouter/route"
	"code.cloudfoundry.org/gorouter/router"
	"code.cloudfoundry.org/gorouter/routeservice"
	"code.cloudfoundry.org/gorouter/test/common"
	"code.cloudfoundry.org/gorouter/test_util"
	vvarz "code.cloudfoundry.org/gorouter/varz"
	"github.com/nats-io/nats.go"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/tedsuo/ifrit"
)

var _ = Describe("Router", func() {
	var (
		logger     logger.Logger
		natsRunner *test_util.NATSRunner
		config     *cfg.Config
		p          http.Handler

		combinedReporter metrics.ProxyReporter
		mbusClient       *nats.Conn
		registry         *rregistry.RouteRegistry
		varz             vvarz.Varz
		rtr              *router.Router
		subscriber       ifrit.Process
		natsPort         uint16
		healthStatus     *health.Health

		ew = errorwriter.NewPlaintextErrorWriter()
	)

	testAndVerifyRouterStopsNoDrain := func(signals chan os.Signal, closeChannel chan struct{}, sigs ...os.Signal) {
		app := common.NewTestApp([]route.Uri{"drain." + test_util.LocalhostDNS}, config.Port, mbusClient, nil, "")
		blocker := make(chan bool)
		resultCh := make(chan bool, 2)
		app.AddHandler("/", func(w http.ResponseWriter, r *http.Request) {
			blocker <- true

			_, err := io.ReadAll(r.Body)
			defer r.Body.Close()
			Expect(err).ToNot(HaveOccurred())

			<-blocker

			w.WriteHeader(http.StatusNoContent)
		})

		app.RegisterAndListen()

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
		Eventually(resultCh, 5*time.Second).Should(Receive(&result))
		Expect(result).To(BeFalse())

		blocker <- false
	}

	runRouter := func(r *router.Router) (chan os.Signal, chan struct{}) {
		signals := make(chan os.Signal, 1)
		readyChan := make(chan struct{}, 1)
		closeChannel := make(chan struct{}, 1)
		errChannel := make(chan error, 1)
		go func() {
			defer close(closeChannel)
			defer close(errChannel)
			errChannel <- r.Run(signals, readyChan)
		}()

		select {
		case <-readyChan:
		case err := <-errChannel:
			Expect(err).ToNot(HaveOccurred())
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

	testUnhealthy := func(h *health.Health, interrupt func()) {
		Expect(h.Health()).To(Equal(health.Healthy))

		go interrupt()

		Eventually(func() health.Status {
			return h.Health()
		}).Should(Equal(health.Degraded))

	}

	BeforeEach(func() {
		logger = test_util.NewTestZapLogger("test")
		natsPort = test_util.NextAvailPort()
		natsRunner = test_util.NewNATSRunner(int(natsPort))
		natsRunner.Start()

		proxyPort := test_util.NextAvailPort()
		statusPort := test_util.NextAvailPort()
		statusTlsPort := test_util.NextAvailPort()
		statusRoutesPort := test_util.NextAvailPort()

		sslPort := test_util.NextAvailPort()

		defaultCert := test_util.CreateCert("default")
		cert2 := test_util.CreateCert("default")

		config = test_util.SpecConfig(statusPort, statusTlsPort, statusRoutesPort, proxyPort, natsPort)
		config.EnableSSL = true
		config.SSLPort = sslPort
		config.SSLCertificates = []tls.Certificate{defaultCert, cert2}
		config.CipherSuites = []uint16{tls.TLS_RSA_WITH_AES_256_CBC_SHA}
		config.EndpointTimeout = 1 * time.Second

		mbusClient = natsRunner.MessageBus
		registry = rregistry.NewRouteRegistry(logger, config, new(fakeMetrics.FakeRouteRegistryReporter))
		logcounter := schema.NewLogCounter()
		healthStatus = &health.Health{}
		healthStatus.SetHealth(health.Healthy)

		varz = vvarz.NewVarz(registry)
		sender := new(fakeMetrics.MetricSender)
		batcher := new(fakeMetrics.MetricBatcher)
		metricReporter := &metrics.MetricsReporter{Sender: sender, Batcher: batcher}
		combinedReporter = &metrics.CompositeReporter{VarzReporter: varz, ProxyReporter: metricReporter}
		config.HealthCheckUserAgent = "HTTP-Monitor/1.1"

		rt := &sharedfakes.RoundTripper{}
		p = proxy.NewProxy(logger, &accesslog.NullAccessLogger{}, nil, ew, config, registry, combinedReporter,
			&routeservice.RouteServiceConfig{}, &tls.Config{}, &tls.Config{}, healthStatus, rt)

		errChan := make(chan error, 2)
		var err error
		rss := &sharedfakes.RouteServicesServer{}
		rtr, err = router.NewRouter(logger, config, p, mbusClient, registry, varz, healthStatus, logcounter, errChan, rss)
		Expect(err).ToNot(HaveOccurred())

		config.Index = 4321
		subscriber = ifrit.Background(mbus.NewSubscriber(mbusClient, registry, config, nil, logger.Session("subscriber")))
		<-subscriber.Ready()

	})

	AfterEach(func() {
		if natsRunner != nil {
			natsRunner.Stop()
		}
		if subscriber != nil {
			subscriber.Signal(os.Interrupt)
			<-subscriber.Wait()
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
			app := common.NewTestApp([]route.Uri{"drain." + test_util.LocalhostDNS}, config.Port, mbusClient, nil, "")
			blocker := make(chan bool)
			drainDone := make(chan struct{})
			clientDone := make(chan struct{})

			app.AddHandler("/", func(w http.ResponseWriter, r *http.Request) {
				blocker <- true

				_, err := io.ReadAll(r.Body)
				defer r.Body.Close()
				Expect(err).ToNot(HaveOccurred())

				<-blocker

				w.WriteHeader(http.StatusNoContent)
			})

			app.RegisterAndListen()

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
				_, err = io.ReadAll(resp.Body)
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

		It("times out if it requests are not completed before timeout", func() {
			app := common.NewTestApp([]route.Uri{"draintimeout." + test_util.LocalhostDNS}, config.Port, mbusClient, nil, "")

			appRequestReceived := make(chan struct{})
			appRequestComplete := make(chan struct{})
			drainResult := make(chan error, 2)

			app.AddHandler("/", func(w http.ResponseWriter, r *http.Request) {
				appRequestReceived <- struct{}{}

				_, err := io.ReadAll(r.Body)
				defer r.Body.Close()
				Expect(err).ToNot(HaveOccurred())

				appRequestComplete <- struct{}{}
			})
			app.RegisterAndListen()

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

			<-appRequestReceived

			go func() {
				defer GinkgoRecover()
				err := rtr.Drain(0, 500*time.Millisecond)
				drainResult <- err
			}()

			var result error
			Eventually(drainResult).Should(Receive(&result))
			Expect(result).To(Equal(router.DrainTimeout))
			<-appRequestComplete
		})

		Context("with http and https servers", func() {
			It("it drains and stops the router", func() {
				app := common.NewTestApp([]route.Uri{"drain." + test_util.LocalhostDNS}, config.Port, mbusClient, nil, "")
				appRequestReceived := make(chan struct{})
				appRequestComplete := make(chan struct{})
				drainDone := make(chan struct{})
				clientDone := make(chan struct{})

				app.AddHandler("/", func(w http.ResponseWriter, r *http.Request) {
					appRequestReceived <- struct{}{}

					_, err := io.ReadAll(r.Body)
					defer r.Body.Close()
					Expect(err).ToNot(HaveOccurred())

					w.WriteHeader(http.StatusNoContent)
					appRequestComplete <- struct{}{}
				})

				app.RegisterAndListen()

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
					_, err = io.ReadAll(resp.Body)
					Expect(err).ToNot(HaveOccurred())
					close(clientDone)
				}()

				<-appRequestReceived

				// check that we can still connect within drainWait time
				go func() {
					defer GinkgoRecover()
					Consistently(func() int {
						return healthCheckWithEndpointReceives()
					}, 500*time.Millisecond).ShouldNot(Equal(http.StatusBadGateway))
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
				<-appRequestComplete

				Eventually(drainDone, drainWait+time.Duration(float64(drainTimeout)*0.9)).Should(BeClosed())
				Eventually(clientDone).Should(BeClosed())
			})
		})
	})

	Context("OnErrOrSignal", func() {
		Context("when an error is received in the error channel", func() {
			var errChan chan error
			var h *health.Health
			var rtr2 *router.Router

			BeforeEach(func() {
				logcounter := schema.NewLogCounter()
				h = &health.Health{}
				h.SetHealth(health.Healthy)
				config.HealthCheckUserAgent = "HTTP-Monitor/1.1"
				config.Status.Port = test_util.NextAvailPort()
				config.Status.TLS.Port = test_util.NextAvailPort()
				config.Status.Routes.Port = test_util.NextAvailPort()
				rt := &sharedfakes.RoundTripper{}
				p := proxy.NewProxy(logger, &accesslog.NullAccessLogger{}, nil, ew, config, registry, combinedReporter,
					&routeservice.RouteServiceConfig{}, &tls.Config{}, &tls.Config{}, h, rt)

				errChan = make(chan error, 2)
				var err error
				rss := &sharedfakes.RouteServicesServer{}
				rtr2, err = router.NewRouter(logger, config, p, mbusClient, registry, varz, h, logcounter, errChan, rss)
				Expect(err).ToNot(HaveOccurred())
				runRouter(rtr2)
			})

			It("degrades router health", func() {
				testUnhealthy(h, func() {
					errChan <- errors.New("initiate unhealthy error")
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

			It("degrades router health", func() {
				testUnhealthy(healthStatus, func() {
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

			It("degrades router health", func() {
				testUnhealthy(healthStatus, func() {
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
