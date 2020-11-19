package router_test

import (
	"bufio"
	"bytes"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"math/big"
	"net"
	"net/http"
	"net/http/httputil"
	"os"
	"syscall"
	"time"

	"code.cloudfoundry.org/gorouter/common/health"

	"code.cloudfoundry.org/gorouter/config"

	"code.cloudfoundry.org/gorouter/accesslog"
	"code.cloudfoundry.org/gorouter/common/schema"
	cfg "code.cloudfoundry.org/gorouter/config"
	"code.cloudfoundry.org/gorouter/errorwriter"
	sharedfakes "code.cloudfoundry.org/gorouter/fakes"
	"code.cloudfoundry.org/gorouter/handlers"
	"code.cloudfoundry.org/gorouter/logger"
	"code.cloudfoundry.org/gorouter/mbus"
	"code.cloudfoundry.org/gorouter/metrics"
	fakeMetrics "code.cloudfoundry.org/gorouter/metrics/fakes"
	"code.cloudfoundry.org/gorouter/proxy"
	rregistry "code.cloudfoundry.org/gorouter/registry"
	"code.cloudfoundry.org/gorouter/route"
	. "code.cloudfoundry.org/gorouter/router"
	"code.cloudfoundry.org/gorouter/routeservice"
	"code.cloudfoundry.org/gorouter/test"
	"code.cloudfoundry.org/gorouter/test/common"
	testcommon "code.cloudfoundry.org/gorouter/test/common"
	"code.cloudfoundry.org/gorouter/test_util"
	vvarz "code.cloudfoundry.org/gorouter/varz"
	"github.com/nats-io/nats.go"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gbytes"
	"github.com/tedsuo/ifrit"
	"github.com/tedsuo/ifrit/grouper"
	"github.com/tedsuo/ifrit/sigmon"
)

var _ = Describe("Router", func() {

	const uuid_regex = `^[[:xdigit:]]{8}(-[[:xdigit:]]{4}){3}-[[:xdigit:]]{12}$`
	const StickyCookieKey = "JSESSIONID"

	var (
		natsRunner *test_util.NATSRunner
		config     *cfg.Config

		mbusClient          *nats.Conn
		registry            *rregistry.RouteRegistry
		varz                vvarz.Varz
		router              *Router
		logger              logger.Logger
		statusPort          uint16
		natsPort            uint16
		fakeReporter        *fakeMetrics.FakeRouteRegistryReporter
		routeServicesServer *sharedfakes.RouteServicesServer
		err                 error
		backendIdleTimeout  time.Duration
		requestTimeout      time.Duration
	)

	BeforeEach(func() {
		proxyPort := test_util.NextAvailPort()
		statusPort = test_util.NextAvailPort()
		natsPort = test_util.NextAvailPort()
		config = test_util.SpecConfig(statusPort, proxyPort, natsPort)
		backendIdleTimeout = config.EndpointTimeout
		requestTimeout = config.EndpointTimeout
		config.EnableSSL = true
		config.SSLPort = test_util.NextAvailPort()
		config.DisableHTTP = false
		cert := test_util.CreateCert("default")
		config.SSLCertificates = []tls.Certificate{cert}
		config.CipherSuites = []uint16{tls.TLS_RSA_WITH_AES_256_CBC_SHA}
		config.ClientCertificateValidation = tls.NoClientCert
		config.StickySessionCookieNames = map[string]struct{}{
			StickyCookieKey: struct{}{},
			"SESSION":       struct{}{},
		}

		natsRunner = test_util.NewNATSRunner(int(natsPort))
		natsRunner.Start()

		routeServicesServer = &sharedfakes.RouteServicesServer{}

		mbusClient = natsRunner.MessageBus
		logger = test_util.NewTestZapLogger("router-test")
		fakeReporter = new(fakeMetrics.FakeRouteRegistryReporter)
		registry = rregistry.NewRouteRegistry(logger, config, fakeReporter)
		varz = vvarz.NewVarz(registry)
	})

	JustBeforeEach(func() {
		router, err = initializeRouter(config, backendIdleTimeout, requestTimeout, registry, varz, mbusClient, logger, routeServicesServer)
		Expect(err).ToNot(HaveOccurred())

		config.Index = 4321
		subscriber := mbus.NewSubscriber(mbusClient, registry, config, nil, logger.Session("subscriber"))

		members := grouper.Members{
			{Name: "subscriber", Runner: subscriber},
			{Name: "router", Runner: router},
		}
		group := grouper.NewOrdered(os.Interrupt, members)
		monitor := ifrit.Invoke(sigmon.New(group, syscall.SIGTERM, syscall.SIGINT, syscall.SIGUSR1))
		<-monitor.Ready()
	})

	AfterEach(func() {
		if natsRunner != nil {
			natsRunner.Stop()
		}

		if router != nil {
			router.Stop()
		}
	})

	Describe("Route Services Server", func() {
		It("starts the Route Services Server", func() {
			Expect(routeServicesServer.ServeCallCount()).To(Equal(1))
		})

		It("shuts down the server properly", func() {
			router.Stop()
			router = nil
			Expect(routeServicesServer.StopCallCount()).To(Equal(1))
		})

		Context("when an error occurs immediately during serve", func() {
			It("causes the router not to run", func() {
				rss := &sharedfakes.RouteServicesServer{}
				rss.ServeReturns(errors.New("serve error"))
				natsPort := test_util.NextAvailPort()
				proxyPort := test_util.NextAvailPort()
				statusPort = test_util.NextAvailPort()

				c := test_util.SpecConfig(statusPort, proxyPort, natsPort)
				c.StartResponseDelayInterval = 1 * time.Second

				rtr, err := initializeRouter(c, c.EndpointTimeout, c.EndpointTimeout, registry, varz, mbusClient, logger, rss)
				Expect(err).NotTo(HaveOccurred())

				signals := make(chan os.Signal)
				readyChan := make(chan struct{})

				err = rtr.Run(signals, readyChan)
				Expect(err).To(MatchError(errors.New("serve error")))
			})
		})

		Context("when an error occurs after some time serving", func() {
			It("causes the route to shut down", func() {
				natsPort := test_util.NextAvailPort()
				proxyPort := test_util.NextAvailPort()
				statusPort = test_util.NextAvailPort()

				c := test_util.SpecConfig(statusPort, proxyPort, natsPort)
				c.StartResponseDelayInterval = 1 * time.Second

				rss := &sharedfakes.RouteServicesServer{}
				rss.ServeStub = func(handler http.Handler, errChan chan error) error {
					errChan <- errors.New("a shutdown error")
					return nil
				}

				rtr, err := initializeRouter(c, c.EndpointTimeout, c.EndpointTimeout, registry, varz, mbusClient, logger, rss)
				Expect(err).NotTo(HaveOccurred())

				signals := make(chan os.Signal)
				readyChan := make(chan struct{})

				go rtr.Run(signals, readyChan)
				Eventually(func() int { return rss.StopCallCount() }, "3s").Should(Equal(2))
			})
		})
	})

	Context("when StartResponseDelayInterval is set", func() {
		var (
			rtr *Router
			c   *cfg.Config
			err error
		)

		It("does not immediately make the health check endpoint available", func() {
			natsPort := test_util.NextAvailPort()
			proxyPort := test_util.NextAvailPort()
			statusPort = test_util.NextAvailPort()
			c = test_util.SpecConfig(statusPort, proxyPort, natsPort)
			c.StartResponseDelayInterval = 1 * time.Second

			// Create a second router to test the health check in parallel to startup
			rtr, err = initializeRouter(c, c.EndpointTimeout, c.EndpointTimeout, registry, varz, mbusClient, logger, routeServicesServer)

			Expect(err).ToNot(HaveOccurred())
			healthCheckWithEndpointReceives := func() int {
				url := fmt.Sprintf("http://%s:%d/health", c.Ip, c.Status.Port)
				req, _ := http.NewRequest("GET", url, nil)

				client := http.Client{}
				resp, err := client.Do(req)
				Expect(err).ToNot(HaveOccurred())
				defer resp.Body.Close()
				return resp.StatusCode
			}
			signals := make(chan os.Signal)
			readyChan := make(chan struct{})
			go rtr.Run(signals, readyChan)

			Consistently(func() int {
				return healthCheckWithEndpointReceives()
			}, 500*time.Millisecond).Should(Equal(http.StatusServiceUnavailable))
			signals <- syscall.SIGUSR1
		})

		It("should log waiting delay value", func() {
			Eventually(logger).Should(gbytes.Say("Sleeping before returning success on /health endpoint to preload routing table"))
		})
	})

	It("registry contains last updated varz", func() {
		app1 := test.NewGreetApp([]route.Uri{"test1." + test_util.LocalhostDNS}, config.Port, mbusClient, nil)
		app1.RegisterAndListen()

		Eventually(func() bool {
			return appRegistered(registry, app1)
		}).Should(BeTrue())

		time.Sleep(100 * time.Millisecond)
		initialUpdateTime := fetchRecursively(readVarz(varz), "ms_since_last_registry_update").(float64)

		app2 := test.NewGreetApp([]route.Uri{"test2." + test_util.LocalhostDNS}, config.Port, mbusClient, nil)
		app2.RegisterAndListen()
		Eventually(func() bool {
			return appRegistered(registry, app2)
		}).Should(BeTrue())

		// updateTime should be after initial update time
		updateTime := fetchRecursively(readVarz(varz), "ms_since_last_registry_update").(float64)
		Expect(updateTime).To(BeNumerically("<", initialUpdateTime))
	})

	It("varz", func() {
		app := test.NewGreetApp([]route.Uri{"count." + test_util.LocalhostDNS}, config.Port, mbusClient, map[string]string{"framework": "rails"})
		app.RegisterAndListen()
		additionalRequests := 100
		go app.RegisterRepeatedly(100 * time.Millisecond)

		Eventually(func() bool {
			return appRegistered(registry, app)
		}).Should(BeTrue())

		// Send seed request
		sendRequests("count."+test_util.LocalhostDNS, config.Port, 1)
		initial_varz := readVarz(varz)

		// Send requests
		sendRequests("count."+test_util.LocalhostDNS, config.Port, additionalRequests)
		updated_varz := readVarz(varz)

		// Verify varz update
		initialRequestCount := fetchRecursively(initial_varz, "requests").(float64)
		updatedRequestCount := fetchRecursively(updated_varz, "requests").(float64)
		requestCount := int(updatedRequestCount - initialRequestCount)
		Expect(requestCount).To(Equal(additionalRequests))

		initialResponse2xxCount := fetchRecursively(initial_varz, "responses_2xx").(float64)
		updatedResponse2xxCount := fetchRecursively(updated_varz, "responses_2xx").(float64)
		response2xxCount := int(updatedResponse2xxCount - initialResponse2xxCount)
		Expect(response2xxCount).To(Equal(additionalRequests))

		app.Unregister()
	})

	It("Sticky sessions allow multiple consecutive requests to reach the same instance of an app", func() {
		apps := make([]*testcommon.TestApp, 10)
		for i := range apps {
			apps[i] = test.NewStickyApp([]route.Uri{"sticky." + test_util.LocalhostDNS}, config.Port, mbusClient, nil, StickyCookieKey)
			apps[i].RegisterAndListen()
		}

		for _, app := range apps {
			Eventually(func() bool {
				return appRegistered(registry, app)
			}).Should(BeTrue())
		}
		resp, port1 := getSessionAndAppPort("sticky."+test_util.LocalhostDNS, config.Port)
		port2 := getAppPortWithSticky("sticky."+test_util.LocalhostDNS, config.Port, resp.Cookies())

		var vcapCookie *http.Cookie
		for _, cookie := range resp.Cookies() {
			if cookie.Name == proxy.VcapCookieId {
				vcapCookie = cookie
			}
		}

		Expect(port1).To(Equal(port2))
		Expect(vcapCookie.Path).To(Equal("/"))

		for _, app := range apps {
			app.Unregister()
		}
	})

	It("Sticky sessions can be configured to use other cookie names", func() {
		apps := make([]*testcommon.TestApp, 10)
		for i := range apps {
			apps[i] = test.NewStickyApp([]route.Uri{"sticky." + test_util.LocalhostDNS}, config.Port, mbusClient, nil, "SESSION")
			apps[i].RegisterAndListen()
		}

		for _, app := range apps {
			Eventually(func() bool {
				return appRegistered(registry, app)
			}).Should(BeTrue())
		}
		resp, port1 := getSessionAndAppPort("sticky."+test_util.LocalhostDNS, config.Port)
		port2 := getAppPortWithSticky("sticky."+test_util.LocalhostDNS, config.Port, resp.Cookies())

		var vcapCookie *http.Cookie
		for _, cookie := range resp.Cookies() {
			if cookie.Name == proxy.VcapCookieId {
				vcapCookie = cookie
			}
		}

		Expect(port1).To(Equal(port2))
		Expect(vcapCookie.Path).To(Equal("/"))

		for _, app := range apps {
			app.Unregister()
		}
	})

	Context("when websocket request is bound to RouteService URL", func() {
		It("the request should respond with a 503", func() {
			app := test.NewWebSocketApp(
				[]route.Uri{"ws-app." + test_util.LocalhostDNS},
				config.Port,
				mbusClient,
				1*time.Second,
				"https://sample_rs_url.com",
			)
			app.RegisterAndListen()
			Eventually(func() bool {
				return appRegistered(registry, app)
			}).Should(BeTrue())

			conn, err := net.Dial("tcp", fmt.Sprintf("ws-app.%s:%d", test_util.LocalhostDNS, config.Port))
			Expect(err).NotTo(HaveOccurred())

			x := test_util.NewHttpConn(conn)

			req := test_util.NewRequest("GET", "ws-app."+test_util.LocalhostDNS, "/chat", nil)
			req.Header.Set("Upgrade", "websocket")
			req.Header.Set("Connection", "upgrade")

			x.WriteRequest(req)

			resp, _ := x.ReadResponse()
			Expect(resp.StatusCode).To(Equal(http.StatusServiceUnavailable))
			// verify the app handler never got invoked.
			x.Close()
		})
	})

	Context("Stop", func() {
		It("no longer proxies http", func() {
			app := testcommon.NewTestApp([]route.Uri{"greet." + test_util.LocalhostDNS}, config.Port, mbusClient, nil, "")

			app.AddHandler("/", func(w http.ResponseWriter, r *http.Request) {
				_, err := ioutil.ReadAll(r.Body)
				defer r.Body.Close()
				Expect(err).ToNot(HaveOccurred())
				w.WriteHeader(http.StatusNoContent)
			})
			app.RegisterAndListen()
			Eventually(func() bool {
				return appRegistered(registry, app)
			}).Should(BeTrue())

			req, err := http.NewRequest("GET", app.Endpoint(), nil)
			Expect(err).ToNot(HaveOccurred())

			sendAndReceive(req, http.StatusNoContent)

			router.Stop()
			router = nil

			req, err = http.NewRequest("GET", app.Endpoint(), nil)
			Expect(err).ToNot(HaveOccurred())
			client := http.Client{}
			_, err = client.Do(req)
			Expect(err).To(HaveOccurred())
		})

		It("no longer responds to component requests", func() {
			host := fmt.Sprintf("http://%s:%d/routes", config.Ip, config.Status.Port)

			req, err := http.NewRequest("GET", host, nil)
			Expect(err).ToNot(HaveOccurred())
			req.SetBasicAuth("user", "pass")

			sendAndReceive(req, http.StatusOK)

			router.Stop()
			router = nil

			Eventually(func() error {
				req, err = http.NewRequest("GET", host, nil)
				Expect(err).ToNot(HaveOccurred())

				_, err = http.DefaultClient.Do(req)
				return err
			}).Should(HaveOccurred())
		})

		It("no longer proxies https", func() {
			app := testcommon.NewTestApp([]route.Uri{"greet." + test_util.LocalhostDNS}, config.Port, mbusClient, nil, "")

			app.AddHandler("/", func(w http.ResponseWriter, r *http.Request) {
				_, err := ioutil.ReadAll(r.Body)
				defer r.Body.Close()
				Expect(err).ToNot(HaveOccurred())
				w.WriteHeader(http.StatusNoContent)
			})
			app.RegisterAndListen()
			Eventually(func() bool {
				return appRegistered(registry, app)
			}).Should(BeTrue())

			host := fmt.Sprintf("https://greet.%s:%d/", test_util.LocalhostDNS, config.SSLPort)

			req, err := http.NewRequest("GET", host, nil)
			Expect(err).ToNot(HaveOccurred())
			req.SetBasicAuth("user", "pass")

			tr := &http.Transport{
				TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
			}
			client := http.Client{Transport: tr}
			resp, err := client.Do(req)
			Expect(err).ToNot(HaveOccurred())
			Expect(resp).ToNot(BeNil())
			resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusNoContent))

			router.Stop()
			router = nil

			req, err = http.NewRequest("GET", host, nil)
			Expect(err).NotTo(HaveOccurred())
			_, err = client.Do(req)
			Expect(err).To(HaveOccurred())
		})
	})

	It("handles a PUT request", func() {
		app := testcommon.NewTestApp([]route.Uri{"greet." + test_util.LocalhostDNS}, config.Port, mbusClient, nil, "")

		var rr *http.Request
		var msg string
		app.AddHandler("/", func(w http.ResponseWriter, r *http.Request) {
			rr = r
			b, err := ioutil.ReadAll(r.Body)
			if err != nil {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			msg = string(b)
		})
		app.RegisterAndListen()
		Eventually(func() bool {
			return appRegistered(registry, app)
		}).Should(BeTrue())

		url := app.Endpoint()

		buf := bytes.NewBufferString("foobar")
		r, err := http.NewRequest("PUT", url, buf)
		Expect(err).ToNot(HaveOccurred())

		client := http.Client{}
		resp, err := client.Do(r)
		Expect(err).ToNot(HaveOccurred())
		Expect(resp.StatusCode).To(Equal(http.StatusOK))

		Expect(rr).ToNot(BeNil())
		Expect(rr.Method).To(Equal("PUT"))
		Expect(rr.Proto).To(Equal("HTTP/1.1"))
		Expect(msg).To(Equal("foobar"))
	})

	It("supports 100 Continue", func() {
		app := testcommon.NewTestApp([]route.Uri{"foo." + test_util.LocalhostDNS}, config.Port, mbusClient, nil, "")
		rCh := make(chan *http.Request)
		app.AddHandler("/", func(w http.ResponseWriter, r *http.Request) {
			_, err := ioutil.ReadAll(r.Body)
			if err != nil {
				w.WriteHeader(http.StatusBadRequest)
			}
			rCh <- r
		})

		app.RegisterAndListen()
		go app.RegisterRepeatedly(1 * time.Second)

		Eventually(func() bool {
			return appRegistered(registry, app)
		}).Should(BeTrue())

		host := fmt.Sprintf("foo.%s:%d", test_util.LocalhostDNS, config.Port)
		conn, err := net.DialTimeout("tcp", host, 10*time.Second)
		Expect(err).ToNot(HaveOccurred())
		defer conn.Close()

		fmt.Fprintf(conn, "POST / HTTP/1.1\r\n"+
			"Host: %s\r\n"+
			"Connection: close\r\n"+
			"Content-Length: 1\r\n"+
			"Expect: 100-continue\r\n"+
			"\r\n", host)

		fmt.Fprintf(conn, "a")

		buf := bufio.NewReader(conn)
		line, err := buf.ReadString('\n')
		Expect(err).ToNot(HaveOccurred())
		Expect(line).To(ContainSubstring("100 Continue"))

		var rr *http.Request
		Eventually(rCh).Should(Receive(&rr))
		Expect(rr).ToNot(BeNil())
	})

	It("X-Vcap-Request-Id header is overwritten", func() {
		done := make(chan string)
		app := testcommon.NewTestApp([]route.Uri{"foo." + test_util.LocalhostDNS}, config.Port, mbusClient, nil, "")
		app.AddHandler("/", func(w http.ResponseWriter, r *http.Request) {
			_, err := ioutil.ReadAll(r.Body)
			Expect(err).NotTo(HaveOccurred())
			w.WriteHeader(http.StatusOK)
			done <- r.Header.Get(handlers.VcapRequestIdHeader)
		})

		app.RegisterAndListen()
		go app.RegisterRepeatedly(1 * time.Second)

		Eventually(func() bool {
			return appRegistered(registry, app)
		}).Should(BeTrue())

		conn, err := net.Dial("tcp", fmt.Sprintf("%s:%d", config.Ip, config.Port))
		Expect(err).NotTo(HaveOccurred())
		defer conn.Close()

		httpConn := test_util.NewHttpConn(conn)

		req := test_util.NewRequest("GET", "foo."+test_util.LocalhostDNS, "/", nil)
		req.Header.Add(handlers.VcapRequestIdHeader, "A-BOGUS-REQUEST-ID")

		httpConn.WriteRequest(req)

		var answer string
		Eventually(done).Should(Receive(&answer))
		Expect(answer).ToNot(Equal("A-BOGUS-REQUEST-ID"))
		Expect(answer).To(MatchRegexp(uuid_regex))
		Expect(logger).To(gbytes.Say("vcap-request-id-header-set"))

		resp, _ := httpConn.ReadResponse()
		Expect(resp.StatusCode).To(Equal(http.StatusOK))
	})

	Describe("/routes endpoint", func() {
		var registerData []byte

		BeforeEach(func() {
			registerData = []byte(`{"dea":"dea1","app":"app1","uris":["test.com"],"host":"1.2.3.4","port":1234,"tags":{},"private_instance_id":"private_instance_id",
		"private_instance_index": "2"}`)
		})

		JustBeforeEach(func() {
			err := mbusClient.Publish("router.register", registerData)
			Expect(err).ToNot(HaveOccurred())
			time.Sleep(250 * time.Millisecond)
		})

		Context("when a route is registered", func() {
			It("contains the registered route", func() {
				Expect(routeExists(config, "test.com")).To(BeTrue())
			})
		})

		Context("when a route is unregistered", func() {
			JustBeforeEach(func() {
				Expect(routeExists(config, "test.com")).To(BeTrue())

				err := mbusClient.Publish("router.unregister", registerData)
				Expect(err).ToNot(HaveOccurred())
				time.Sleep(250 * time.Millisecond)
			})

			It("removes the unregistered route", func() {
				Expect(routeExists(config, "test.com")).To(BeFalse())
			})
		})
	})

	Context("when proxy proto is enabled", func() {
		BeforeEach(func() {
			config.EnablePROXY = true
		})

		It("sets the X-Forwarded-For header", func() {
			app := testcommon.NewTestApp([]route.Uri{"proxy." + test_util.LocalhostDNS}, config.Port, mbusClient, nil, "")

			rCh := make(chan string)
			app.AddHandler("/", func(w http.ResponseWriter, r *http.Request) {
				rCh <- r.Header.Get("X-Forwarded-For")
			})
			app.RegisterAndListen()
			Eventually(func() bool {
				return appRegistered(registry, app)
			}).Should(BeTrue())

			host := fmt.Sprintf("proxy.%s:%d", test_util.LocalhostDNS, config.Port)
			conn, err := net.DialTimeout("tcp", host, 10*time.Second)
			Expect(err).ToNot(HaveOccurred())
			defer conn.Close()

			fmt.Fprintf(conn, "PROXY TCP4 192.168.0.1 192.168.0.2 12345 80\r\n"+
				"GET / HTTP/1.0\r\n"+
				"Host: %s\r\n"+
				"\r\n", host)

			var rr string
			Eventually(rCh).Should(Receive(&rr))
			Expect(rr).ToNot(BeNil())
			Expect(rr).To(Equal("192.168.0.1"))
		})

		It("sets the x-Forwarded-Proto header to https", func() {
			app := test.NewGreetApp([]route.Uri{"test." + test_util.LocalhostDNS}, config.Port, mbusClient, nil)
			app.RegisterAndListen()
			Eventually(func() bool {
				return appRegistered(registry, app)
			}).Should(BeTrue())

			uri := fmt.Sprintf("https://test.%s:%d/forwardedprotoheader", test_util.LocalhostDNS, config.SSLPort)
			req, _ := http.NewRequest("GET", uri, nil)
			tr := &http.Transport{
				TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
			}
			client := http.Client{Transport: tr}

			resp, err := client.Do(req)
			Expect(err).ToNot(HaveOccurred())
			Expect(resp).ToNot(BeNil())
			Expect(resp.StatusCode).To(Equal(http.StatusOK))

			bytes, err := ioutil.ReadAll(resp.Body)
			Expect(err).ToNot(HaveOccurred())
			Expect(string(bytes)).To(Equal("https"))
			resp.Body.Close()
		})
	})

	Context("HTTP keep-alive", func() {
		It("reuses the same connection on subsequent calls", func() {
			app := test.NewGreetApp([]route.Uri{"keepalive." + test_util.LocalhostDNS}, config.Port, mbusClient, nil)
			app.RegisterAndListen()
			Eventually(func() bool {
				return appRegistered(registry, app)
			}).Should(BeTrue())

			host := fmt.Sprintf("keepalive.%s:%d", test_util.LocalhostDNS, config.Port)
			uri := fmt.Sprintf("http://%s", host)

			conn, err := net.Dial("tcp", host)
			Expect(err).ToNot(HaveOccurred())

			client := httputil.NewClientConn(conn, nil)
			req, _ := http.NewRequest("GET", uri, nil)
			Expect(req.Close).To(BeFalse())

			resp, err := client.Do(req)
			Expect(err).ToNot(HaveOccurred())
			Expect(resp).ToNot(BeNil())
			resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusOK))

			//make second request without errors
			resp, err = client.Do(req)
			Expect(err).ToNot(HaveOccurred())
			Expect(resp).ToNot(BeNil())
			resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusOK))
		})

		It("resets the idle timeout on activity", func() {
			app := test.NewGreetApp([]route.Uri{"keepalive." + test_util.LocalhostDNS}, config.Port, mbusClient, nil)
			app.RegisterAndListen()
			Eventually(func() bool {
				return appRegistered(registry, app)
			}).Should(BeTrue())

			host := fmt.Sprintf("keepalive.%s:%d", test_util.LocalhostDNS, config.Port)
			uri := fmt.Sprintf("http://%s", host)

			conn, err := net.Dial("tcp", host)
			Expect(err).ToNot(HaveOccurred())

			client := httputil.NewClientConn(conn, nil)
			req, _ := http.NewRequest("GET", uri, nil)
			Expect(req.Close).To(BeFalse())

			// initiate idle timeout
			assertServerResponse(client, req)

			// use 3/4 of the idle timeout
			time.Sleep((config.EndpointTimeout * 3) / 4)

			//make second request without errors
			resp, err := client.Do(req)
			Expect(err).ToNot(HaveOccurred())
			Expect(resp).ToNot(BeNil())
			resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusOK))

			// use another 3/4 of the idle timeout, exceeding the original timeout
			time.Sleep((config.EndpointTimeout * 3) / 4)

			// make third request without errors
			// even though initial idle timeout was exceeded because
			// it will have been reset
			resp, err = client.Do(req)
			Expect(err).ToNot(HaveOccurred())
			Expect(resp).ToNot(BeNil())
			resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusOK))
		})

		It("removes the idle timeout during an active connection", func() {
			// create an app that takes 3/4 of the deadline to respond
			// during an active connection
			app := newSlowApp(
				[]route.Uri{"keepalive." + test_util.LocalhostDNS},
				config.Port,
				mbusClient,
				(config.EndpointTimeout*3)/4,
			)
			app.RegisterAndListen()
			Eventually(func() bool {
				return appRegistered(registry, app)
			}).Should(BeTrue())

			host := fmt.Sprintf("keepalive.%s:%d", test_util.LocalhostDNS, config.Port)
			uri := fmt.Sprintf("http://%s", host)

			conn, err := net.Dial("tcp", host)
			Expect(err).ToNot(HaveOccurred())

			client := httputil.NewClientConn(conn, nil)
			req, _ := http.NewRequest("GET", uri, nil)
			Expect(req.Close).To(BeFalse())

			// initiate idle timeout
			assertServerResponse(client, req)

			// use 3/4 of the idle timeout
			time.Sleep((config.EndpointTimeout * 3) / 4)

			// because 3/4 of the idle timeout is now used
			// making a request that will last 3/4 of the timeout
			// that does not disconnect will show that the idle timeout
			// was removed during the active connection
			resp, err := client.Do(req)
			Expect(err).ToNot(HaveOccurred())
			Expect(resp).ToNot(BeNil())
			resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusOK))
		})
	})

	Describe("request timeout for long requests", func() {
		var (
			req             *http.Request
			client          http.Client
			appResponseTime time.Duration
		)

		Context("when app response time is longer than request timeout (and shorter than backend idle timeout)", func() {
			BeforeEach(func() {
				requestTimeout = 1 * time.Second
				backendIdleTimeout = 3 * time.Second
				appResponseTime = 2 * time.Second
				client = http.Client{}
			})
			JustBeforeEach(func() {
				app := newSlowApp(
					[]route.Uri{"slow-app." + test_util.LocalhostDNS},
					config.Port,
					mbusClient,
					appResponseTime,
				)

				app.RegisterAndListen()
				Eventually(func() bool {
					return appRegistered(registry, app)
				}).Should(BeTrue())
			})

			Context("when no body or header has been written", func() {
				BeforeEach(func() {
					uri := fmt.Sprintf("http://slow-app.%s:%d/", test_util.LocalhostDNS, config.Port)
					req, _ = http.NewRequest("GET", uri, nil)
				})

				It("responds with a 502/BadGateway and an error message", func() {
					resp, err := client.Do(req)
					Expect(err).ToNot(HaveOccurred())
					Expect(resp).ToNot(BeNil())
					Expect(resp.StatusCode).To(Equal(http.StatusBadGateway))
					defer resp.Body.Close()

					_, err = ioutil.ReadAll(resp.Body)
					Expect(err).ToNot(HaveOccurred())
					Expect(logger).Should(gbytes.Say("backend-request-timeout.*context deadline exceeded"))
					Expect(logger).Should(gbytes.Say("backend-endpoint-failed.*context deadline exceeded"))
				})
			})

			Context("when something has been written, but the response has not completed", func() {
				BeforeEach(func() {
					uri := fmt.Sprintf("http://slow-app.%s:%d/partialresponse", test_util.LocalhostDNS, config.Port)
					req, _ = http.NewRequest("GET", uri, nil)
				})

				It("responds with an unreadable body", func() {
					resp, err := client.Do(req)
					Expect(err).ToNot(HaveOccurred())
					Expect(resp).ToNot(BeNil())
					Expect(resp.StatusCode).To(Equal(http.StatusTeapot))
					defer resp.Body.Close()

					_, err = ioutil.ReadAll(resp.Body)
					Expect(err).To(MatchError("unexpected EOF"))
					Expect(logger).Should(gbytes.Say("backend-request-timeout.*context deadline exceeded"))
				})
			})
		})

		Context("when app response time is shorter than request timeout (and longer than the backend idle timeout)", func() {
			BeforeEach(func() {
				requestTimeout = 3 * time.Second
				backendIdleTimeout = 1 * time.Second
				appResponseTime = 2 * time.Second
				client = http.Client{}
			})
			JustBeforeEach(func() {
				app := newSlowApp(
					[]route.Uri{"slow-app." + test_util.LocalhostDNS},
					config.Port,
					mbusClient,
					appResponseTime,
				)

				app.RegisterAndListen()
				Eventually(func() bool {
					return appRegistered(registry, app)
				}).Should(BeTrue())
			})

			It("succeeds", func() {
				uri := fmt.Sprintf("http://slow-app.%s:%d/", test_util.LocalhostDNS, config.Port)
				req, _ = http.NewRequest("GET", uri, nil)
				resp, err := client.Do(req)
				Expect(err).ToNot(HaveOccurred())
				Expect(resp).ToNot(BeNil())
				Expect(resp.StatusCode).To(Equal(http.StatusOK))
				defer resp.Body.Close()

				_, err = ioutil.ReadAll(resp.Body)
				Expect(err).ToNot(HaveOccurred())
			})
		})

		It("websockets do not terminate", func() {
			app := test.NewWebSocketApp(
				[]route.Uri{"ws-app." + test_util.LocalhostDNS},
				config.Port,
				mbusClient,
				config.EndpointTimeout*2,
				"",
			)
			app.RegisterAndListen()
			Eventually(func() bool {
				return appRegistered(registry, app)
			}).Should(BeTrue())

			conn, err := net.Dial("tcp", fmt.Sprintf("ws-app.%s:%d", test_util.LocalhostDNS, config.Port))
			Expect(err).NotTo(HaveOccurred())

			x := test_util.NewHttpConn(conn)

			req := test_util.NewRequest("GET", "ws-app."+test_util.LocalhostDNS, "/chat", nil)
			req.Header.Set("Upgrade", "websocket")
			req.Header.Set("Connection", "upgrade")

			x.WriteRequest(req)

			resp, _ := x.ReadResponse()
			Expect(resp.StatusCode).To(Equal(http.StatusSwitchingProtocols))

			x.WriteLine("hello from client")
			x.CheckLine("hello from server")

			x.Close()
		})
	})

	Context("multiple open connections", func() {
		It("does not return an error handling connections", func() {
			app := testcommon.NewTestApp([]route.Uri{"app." + test_util.LocalhostDNS}, config.Port, mbusClient, nil, "")

			rCh := make(chan string)
			app.AddHandler("/", func(w http.ResponseWriter, r *http.Request) {
				rCh <- r.Header.Get("X-Forwarded-For")
			})
			app.RegisterAndListen()
			Eventually(func() bool {
				return appRegistered(registry, app)
			}).Should(BeTrue())

			host := fmt.Sprintf("app.%s:%d", test_util.LocalhostDNS, config.Port)
			existingConn, err := net.DialTimeout("tcp", host, 10*time.Second)
			Expect(err).ToNot(HaveOccurred())
			defer existingConn.Close()

			fmt.Fprintf(existingConn, "GET / HTTP/1.1\r\n"+
				"Host: %s\r\n"+
				"\r\n", host)

			newConn, err := net.DialTimeout("tcp", host, 10*time.Second)
			Expect(err).ToNot(HaveOccurred())
			defer newConn.Close()

			fmt.Fprintf(newConn, "GET / HTTP/1.1\r\n"+
				"Host: %s\r\n"+
				"\r\n", host)

			var rr string
			Eventually(rCh).Should(Receive(&rr))
			Expect(rr).ToNot(BeNil())
		})

		It("does not hang while handling new connection", func() {
			app := testcommon.NewTestApp([]route.Uri{"app." + test_util.LocalhostDNS}, config.Port, mbusClient, nil, "")

			rCh := make(chan string)
			app.AddHandler("/", func(w http.ResponseWriter, r *http.Request) {
				rCh <- r.Header.Get("X-Forwarded-For")
			})
			app.RegisterAndListen()
			Eventually(func() bool {
				return appRegistered(registry, app)
			}).Should(BeTrue())

			host := fmt.Sprintf("app.%s:%d", test_util.LocalhostDNS, config.Port)
			existingConn, err := net.DialTimeout("tcp", host, 10*time.Second)
			Expect(err).ToNot(HaveOccurred())
			defer existingConn.Close()

			fmt.Fprintf(existingConn, "")

			newConn, err := net.DialTimeout("tcp", host, 10*time.Second)
			Expect(err).ToNot(HaveOccurred())
			defer newConn.Close()

			fmt.Fprintf(newConn, "GET / HTTP/1.1\r\n"+
				"Host: %s\r\n"+
				"\r\n", host)

			var rr string
			Eventually(rCh, 1*time.Second).Should(Receive(&rr))
			Expect(rr).ToNot(BeNil())
		})
	})

	Context("when DisableHTTP is true", func() {
		BeforeEach(func() {
			config.DisableHTTP = true
		})

		It("does refuses connections to the HTTP port", func() {
			_, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", config.Port))
			Expect(err).To(MatchError(ContainSubstring("connection refused")))
		})
	})

	Describe("XFCC header behavior", func() {
		var (
			receivedReqChan chan *http.Request
			req             *http.Request
			httpClient      *http.Client
			tlsClientConfig *tls.Config
			clientCert      *tls.Certificate
		)

		doAndGetReceivedRequest := func() *http.Request {
			resp, err := httpClient.Do(req)
			Expect(err).NotTo(HaveOccurred())
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusTeapot))

			var receivedReq *http.Request
			Eventually(receivedReqChan).Should(Receive(&receivedReq))
			return receivedReq
		}

		BeforeEach(func() {
			receivedReqChan = make(chan *http.Request, 1)

			uri := fmt.Sprintf("https://test.%s:%d/record_headers", test_util.LocalhostDNS, config.SSLPort)
			req, _ = http.NewRequest("GET", uri, nil)

			certChain := test_util.CreateSignedCertWithRootCA(test_util.CertNames{CommonName: "*." + test_util.LocalhostDNS})
			config.SSLCertificates = []tls.Certificate{certChain.TLSCert()}

			clientCertTemplate, err := certTemplate("clientSSL")
			Expect(err).ToNot(HaveOccurred())
			clientCert, err = createClientCert(clientCertTemplate, certChain.CACert, certChain.CAPrivKey)
			Expect(err).ToNot(HaveOccurred())

			// creating and assigning pool because this test does not run buildClientCertPool
			rootCAs := x509.NewCertPool()
			rootCAs.AddCert(certChain.CACert)
			config.ClientCAPool = rootCAs

			tlsClientConfig = &tls.Config{
				RootCAs: rootCAs,
			}

			httpClient = &http.Client{Transport: &http.Transport{
				TLSClientConfig: tlsClientConfig,
			}}
		})

		JustBeforeEach(func() {
			app := test.NewGreetApp([]route.Uri{"test." + test_util.LocalhostDNS}, config.Port, mbusClient, nil)
			app.AddHandler("/record_headers", func(w http.ResponseWriter, r *http.Request) {
				receivedReqChan <- r
				w.WriteHeader(http.StatusTeapot)
			})
			app.RegisterAndListen()
			Eventually(func() bool {
				return appRegistered(registry, app)
			}).Should(BeTrue())
		})

		Context("when the gorouter is configured with always_forward", func() {
			BeforeEach(func() {
				config.ForwardedClientCert = "always_forward"
			})

			Context("when the xfcc header is provided by the client", func() {
				BeforeEach(func() {
					req.Header.Set("X-Forwarded-Client-Cert", "potato")
				})

				Context("when the client connects with mTLS", func() {
					BeforeEach(func() {
						tlsClientConfig.Certificates = []tls.Certificate{*clientCert}
					})

					It("does not remove the xfcc header", func() {
						receivedReq := doAndGetReceivedRequest()
						Expect(receivedReq.Header.Get("X-Forwarded-Client-Cert")).To(Equal("potato"))
					})
				})

				Context("when the client connects with regular (non-mutual) TLS", func() {
					BeforeEach(func() {
						tlsClientConfig.Certificates = nil
					})
					It("does not remove the xfcc header", func() {
						receivedReq := doAndGetReceivedRequest()
						Expect(receivedReq.Header.Get("X-Forwarded-Client-Cert")).To(Equal("potato"))
					})
				})

				Context("when the client connects with out any TLS", func() {
					BeforeEach(func() {
						uri := fmt.Sprintf("http://test.%s:%d/record_headers", test_util.LocalhostDNS, config.Port)
						req, _ = http.NewRequest("GET", uri, nil)
						req.Header.Set("X-Forwarded-Client-Cert", "potato")
					})

					It("does not remove the xfcc header", func() {
						receivedReq := doAndGetReceivedRequest()
						Expect(receivedReq.Header.Get("X-Forwarded-Client-Cert")).To(Equal("potato"))
					})
				})
			})

			Context("when the xfcc header is not provided by the client", func() {
				BeforeEach(func() {
					req.Header.Del("X-Forwarded-Client-Cert")
				})

				Context("when the client connects with mTLS", func() {
					BeforeEach(func() {
						tlsClientConfig.Certificates = []tls.Certificate{*clientCert}
					})

					It("does not add a xfcc header", func() {
						receivedReq := doAndGetReceivedRequest()
						Expect(receivedReq.Header.Get("X-Forwarded-Client-Cert")).To(BeEmpty())
					})
				})

				Context("when the client connects with regular (non-mutual) TLS", func() {
					BeforeEach(func() {
						tlsClientConfig.Certificates = nil
					})
					It("does not add a xfcc header", func() {
						receivedReq := doAndGetReceivedRequest()
						Expect(receivedReq.Header.Get("X-Forwarded-Client-Cert")).To(BeEmpty())
					})
				})
			})
		})

		Context("when the gorouter is configured with forward", func() {
			BeforeEach(func() {
				config.ForwardedClientCert = "forward"
			})

			Context("when the xfcc header is provided by the client", func() {
				BeforeEach(func() {
					req.Header.Set("X-Forwarded-Client-Cert", "potato")
				})

				Context("when the client attempts to connect with mTLS", func() {
					BeforeEach(func() {
						tlsClientConfig.Certificates = []tls.Certificate{*clientCert}
					})

					It("does remove the xfcc header (because default ClientAuth = NoClientCert, so the client cert is ignored)", func() {
						receivedReq := doAndGetReceivedRequest()
						Expect(receivedReq.Header.Get("X-Forwarded-Client-Cert")).To(BeEmpty())
					})

					Context("when gorouter is configured with ClientAuth = VerifyClientCertIfGiven", func() {
						BeforeEach(func() {
							config.ClientCertificateValidation = tls.VerifyClientCertIfGiven
						})

						It("does not remove the xfcc header", func() {
							receivedReq := doAndGetReceivedRequest()
							Expect(receivedReq.Header.Get("X-Forwarded-Client-Cert")).To(Equal("potato"))
						})
					})
				})

				Context("when the client connects with regular (non-mutual) TLS", func() {
					BeforeEach(func() {
						tlsClientConfig.Certificates = nil
					})
					It("removes the xfcc header", func() {
						receivedReq := doAndGetReceivedRequest()
						Expect(receivedReq.Header.Get("X-Forwarded-Client-Cert")).To(BeEmpty())
					})
				})

				Context("when the client connects with out any TLS", func() {
					BeforeEach(func() {
						uri := fmt.Sprintf("http://test.%s:%d/record_headers", test_util.LocalhostDNS, config.Port)
						req, _ = http.NewRequest("GET", uri, nil)
						req.Header.Set("X-Forwarded-Client-Cert", "potato")
					})

					It("removes the xfcc header", func() {
						receivedReq := doAndGetReceivedRequest()
						Expect(receivedReq.Header.Get("X-Forwarded-Client-Cert")).To(BeEmpty())
					})
				})
			})

			Context("when the xfcc header is not provided by the client", func() {
				BeforeEach(func() {
					req.Header.Del("X-Forwarded-Client-Cert")
				})

				Context("when the client connects with mTLS", func() {
					BeforeEach(func() {
						tlsClientConfig.Certificates = []tls.Certificate{*clientCert}
					})

					It("does not add a xfcc header", func() {
						receivedReq := doAndGetReceivedRequest()
						Expect(receivedReq.Header.Get("X-Forwarded-Client-Cert")).To(BeEmpty())
					})
				})

				Context("when the client connects with regular (non-mutual) TLS", func() {
					BeforeEach(func() {
						tlsClientConfig.Certificates = nil
					})
					It("does not add a xfcc header", func() {
						receivedReq := doAndGetReceivedRequest()
						Expect(receivedReq.Header.Get("X-Forwarded-Client-Cert")).To(BeEmpty())
					})
				})
			})
		})

		Context("when the gorouter is configured with sanitize_set", func() {
			BeforeEach(func() {
				config.ForwardedClientCert = "sanitize_set"
			})

			Context("when the xfcc header is provided by the client", func() {
				BeforeEach(func() {
					req.Header.Set("X-Forwarded-Client-Cert", "potato")
				})

				Context("when the client connects with mTLS", func() {
					BeforeEach(func() {
						tlsClientConfig.Certificates = []tls.Certificate{*clientCert}
					})

					Context("when gorouter is configured with ClientAuth = NoClientCert, so the client cert is ignored, behaving like non-mutual TLS", func() {
						It("removes the xfcc header", func() {
							receivedReq := doAndGetReceivedRequest()
							Expect(receivedReq.Header.Get("X-Forwarded-Client-Cert")).To(BeEmpty())
						})
					})

					Context("when gorouter is configured with ClientAuth = VerifyClientCertIfGiven", func() {
						BeforeEach(func() {
							config.ClientCertificateValidation = tls.VerifyClientCertIfGiven
						})
						It("replaces the xfcc header", func() {
							receivedReq := doAndGetReceivedRequest()
							xfccData := receivedReq.Header.Get("X-Forwarded-Client-Cert")
							Expect(base64.StdEncoding.EncodeToString(clientCert.Certificate[0])).To(Equal(xfccData))
						})
					})
				})

				Context("when the client connects with regular (non-mutual) TLS", func() {
					BeforeEach(func() {
						tlsClientConfig.Certificates = nil
					})
					It("removes the xfcc header", func() {
						receivedReq := doAndGetReceivedRequest()
						Expect(receivedReq.Header.Get("X-Forwarded-Client-Cert")).To(BeEmpty())
					})
				})

				Context("when the client connects with out any TLS", func() {
					BeforeEach(func() {
						uri := fmt.Sprintf("http://test.%s:%d/record_headers", test_util.LocalhostDNS, config.Port)
						req, _ = http.NewRequest("GET", uri, nil)
						req.Header.Set("X-Forwarded-Client-Cert", "potato")
					})

					It("removes the xfcc header", func() {
						receivedReq := doAndGetReceivedRequest()
						Expect(receivedReq.Header.Get("X-Forwarded-Client-Cert")).To(BeEmpty())
					})
				})
			})

			Context("when the xfcc header is not provided by the client", func() {
				BeforeEach(func() {
					req.Header.Del("X-Forwarded-Client-Cert")
				})

				Context("when the client connects with mTLS", func() {
					BeforeEach(func() {
						tlsClientConfig.Certificates = []tls.Certificate{*clientCert}
					})

					Context("when gorouter is configured with ClientAuth = NoClientCert, so the client cert is ignored, behaving like non-mutual TLS", func() {
						It("removes the xfcc header", func() {
							receivedReq := doAndGetReceivedRequest()
							Expect(receivedReq.Header.Get("X-Forwarded-Client-Cert")).To(BeEmpty())
						})
					})

					Context("when gorouter is configured with ClientAuth = VerifyClientCertIfGiven", func() {
						BeforeEach(func() {
							config.ClientCertificateValidation = tls.VerifyClientCertIfGiven
						})
						It("adds the xfcc header", func() {
							receivedReq := doAndGetReceivedRequest()
							xfccData := receivedReq.Header.Get("X-Forwarded-Client-Cert")
							Expect(base64.StdEncoding.EncodeToString(clientCert.Certificate[0])).To(Equal(xfccData))
						})
					})
				})

				Context("when the client connects with regular (non-mutual) TLS", func() {
					BeforeEach(func() {
						tlsClientConfig.Certificates = nil
					})
					It("does not add a xfcc header", func() {
						receivedReq := doAndGetReceivedRequest()
						Expect(receivedReq.Header.Get("X-Forwarded-Client-Cert")).To(BeEmpty())
					})
				})

				Context("when the client connects with out any TLS", func() {
					BeforeEach(func() {
						uri := fmt.Sprintf("http://test.%s:%d/record_headers", test_util.LocalhostDNS, config.Port)
						req, _ = http.NewRequest("GET", uri, nil)
						req.Header.Set("X-Forwarded-Client-Cert", "potato")
					})

					It("does not add a xfcc header", func() {
						receivedReq := doAndGetReceivedRequest()
						Expect(receivedReq.Header.Get("X-Forwarded-Client-Cert")).To(BeEmpty())
					})
				})
			})
		})

	})

	Context("serving https", func() {
		var (
			cert []byte

			client          *http.Client
			tlsClientConfig *tls.Config
		)
		BeforeEach(func() {
			certChain := test_util.CreateSignedCertWithRootCA(test_util.CertNames{CommonName: "test." + test_util.LocalhostDNS})
			config.CACerts = string(certChain.CACertPEM)
			config.SSLCertificates = append(config.SSLCertificates, certChain.TLSCert())
			cert = certChain.CertPEM

			rootCAs := x509.NewCertPool()
			rootCAs.AddCert(certChain.CACert)
			tlsClientConfig = &tls.Config{
				RootCAs: rootCAs,
			}
			client = &http.Client{Transport: &http.Transport{
				TLSClientConfig: tlsClientConfig,
			}}
		})

		It("serves ssl traffic", func() {
			app := test.NewGreetApp([]route.Uri{"test." + test_util.LocalhostDNS}, config.Port, mbusClient, nil)
			app.RegisterAndListen()
			Eventually(func() bool {
				return appRegistered(registry, app)
			}).Should(BeTrue())

			uri := fmt.Sprintf("https://test.%s:%d/", test_util.LocalhostDNS, config.SSLPort)
			req, _ := http.NewRequest("GET", uri, nil)

			resp, err := client.Do(req)
			Expect(err).ToNot(HaveOccurred())
			Expect(resp).ToNot(BeNil())

			Expect(resp.StatusCode).To(Equal(http.StatusOK))

			bytes, err := ioutil.ReadAll(resp.Body)
			Expect(err).ToNot(HaveOccurred())
			Expect(bytes).To(ContainSubstring("Hello"))
			defer resp.Body.Close()
		})

		It("fails when the client uses an unsupported cipher suite", func() {
			tlsClientConfig.MaxVersion = tls.VersionTLS12 // Can not configure cipher suites for TLS1.3
			tlsClientConfig.CipherSuites = []uint16{tls.TLS_ECDHE_RSA_WITH_3DES_EDE_CBC_SHA}

			app := test.NewGreetApp([]route.Uri{"test." + test_util.LocalhostDNS}, config.Port, mbusClient, nil)
			app.RegisterAndListen()
			Eventually(func() bool {
				return appRegistered(registry, app)
			}).Should(BeTrue())

			uri := fmt.Sprintf("https://test.%s:%d", test_util.LocalhostDNS, config.SSLPort)
			req, _ := http.NewRequest("GET", uri, nil)

			_, err := client.Do(req)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("handshake failure"))
		})

		It("sets the x-Forwarded-Proto header to https", func() {
			app := test.NewGreetApp([]route.Uri{"test." + test_util.LocalhostDNS}, config.Port, mbusClient, nil)
			app.RegisterAndListen()
			Eventually(func() bool {
				return appRegistered(registry, app)
			}).Should(BeTrue())

			uri := fmt.Sprintf("https://test.%s:%d/forwardedprotoheader", test_util.LocalhostDNS, config.SSLPort)
			req, _ := http.NewRequest("GET", uri, nil)

			resp, err := client.Do(req)
			Expect(err).ToNot(HaveOccurred())
			Expect(resp).ToNot(BeNil())
			Expect(resp.StatusCode).To(Equal(http.StatusOK))

			bytes, err := ioutil.ReadAll(resp.Body)
			Expect(err).ToNot(HaveOccurred())
			Expect(string(bytes)).To(Equal("https"))
			resp.Body.Close()
		})

		Context("when a ca cert is provided", func() {
			BeforeEach(func() {
				config.CACerts = string(cert)
			})
			It("add the ca cert to the trusted pool and returns 200", func() {
				certPool, err := x509.SystemCertPool()
				Expect(err).ToNot(HaveOccurred())
				certPool.AppendCertsFromPEM(cert)
				tlsClientConfig.RootCAs = certPool

				app := test.NewGreetApp([]route.Uri{"test." + test_util.LocalhostDNS}, config.Port, mbusClient, nil)
				app.RegisterAndListen()
				Eventually(func() bool {
					return appRegistered(registry, app)
				}).Should(BeTrue())

				uri := fmt.Sprintf("https://test.%s:%d", test_util.LocalhostDNS, config.SSLPort)
				req, _ := http.NewRequest("GET", uri, nil)

				resp, err := client.Do(req)
				Expect(err).ToNot(HaveOccurred())
				Expect(resp).ToNot(BeNil())
				Expect(resp.StatusCode).To(Equal(http.StatusOK))
			})
		})

		Context("when frontend SSL is disabled", func() {
			BeforeEach(func() {
				config.EnableSSL = false
			})

			It("refuses connections to the SSL port", func() {
				_, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", config.SSLPort))
				Expect(err.Error()).To(ContainSubstring("connection refused"))
			})
		})

		Context("when a supported server name is provided", func() {
			BeforeEach(func() {
				tlsClientConfig.ServerName = "test." + test_util.LocalhostDNS
			})
			It("return 200 Ok status", func() {
				app := test.NewGreetApp([]route.Uri{"test." + test_util.LocalhostDNS}, config.Port, mbusClient, nil)
				app.RegisterAndListen()
				Eventually(func() bool {
					return appRegistered(registry, app)
				}).Should(BeTrue())

				uri := fmt.Sprintf("https://test.%s:%d", test_util.LocalhostDNS, config.SSLPort)
				req, _ := http.NewRequest("GET", uri, nil)

				resp, err := client.Do(req)
				Expect(err).ToNot(HaveOccurred())
				Expect(resp).ToNot(BeNil())
				Expect(resp.StatusCode).To(Equal(http.StatusOK))

				bytes, err := ioutil.ReadAll(resp.Body)
				Expect(err).ToNot(HaveOccurred())
				Expect(string(bytes)).To(ContainSubstring("Hello"))
				resp.Body.Close()
			})

			It("retrieves the correct certificate for the client", func() {
				app := test.NewGreetApp([]route.Uri{"test." + test_util.LocalhostDNS}, config.Port, mbusClient, nil)

				app.RegisterAndListen()
				Eventually(func() bool {
					return appRegistered(registry, app)
				}).Should(BeTrue())

				uri := fmt.Sprintf("test.%s:%d", test_util.LocalhostDNS, config.SSLPort)

				conn, err := tls.Dial("tcp", uri, tlsClientConfig)
				Expect(err).ToNot(HaveOccurred())
				defer conn.Close()
				cstate := conn.ConnectionState()
				certs := cstate.PeerCertificates
				Expect(len(certs)).To(Equal(1))
				Expect(certs[0].Subject.CommonName).To(Equal("test." + test_util.LocalhostDNS))

			})
			Context("with certificate chain", func() {
				BeforeEach(func() {
					chainRootCaCert, chainRootCaKey, rootPEM, err := createRootCA("a." + test_util.LocalhostDNS)
					Expect(err).ToNot(HaveOccurred())
					intermediateKey, err := rsa.GenerateKey(rand.Reader, 2048)
					Expect(err).ToNot(HaveOccurred())
					intermediateTmpl, err := certTemplate("b." + test_util.LocalhostDNS)
					Expect(err).ToNot(HaveOccurred())
					intermediateCert, intermediatePEM, err := createCert(intermediateTmpl, chainRootCaCert, &intermediateKey.PublicKey, chainRootCaKey)
					Expect(err).ToNot(HaveOccurred())
					leafTmpl, err := certTemplate("c." + test_util.LocalhostDNS)
					Expect(err).ToNot(HaveOccurred())
					leafKey, err := rsa.GenerateKey(rand.Reader, 2048)
					Expect(err).ToNot(HaveOccurred())
					leafKeyPEM := pem.EncodeToMemory(&pem.Block{
						Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(leafKey),
					})
					_, leafPEM, err := createCert(leafTmpl, intermediateCert, &leafKey.PublicKey, intermediateKey)
					Expect(err).ToNot(HaveOccurred())
					chainPEM := append(leafPEM, intermediatePEM...)
					chainPEM = append(chainPEM, rootPEM...)
					chainCert, err := tls.X509KeyPair(chainPEM, leafKeyPEM)
					Expect(err).ToNot(HaveOccurred())
					config.SSLCertificates = append(config.SSLCertificates, chainCert) //[]tls.Certificate{chainCert}

				})
				It("return 200 Ok status", func() {
					app := test.NewGreetApp([]route.Uri{"c." + test_util.LocalhostDNS}, config.Port, mbusClient, nil)

					app.RegisterAndListen()
					Eventually(func() bool {
						return appRegistered(registry, app)
					}).Should(BeTrue())

					uri := fmt.Sprintf("c.%s:%d", test_util.LocalhostDNS, config.SSLPort)
					tlsConfig := &tls.Config{
						InsecureSkipVerify: true,
						ServerName:         "c." + test_util.LocalhostDNS,
					}
					conn, err := tls.Dial("tcp", uri, tlsConfig)
					Expect(err).ToNot(HaveOccurred())
					defer conn.Close()
					cstate := conn.ConnectionState()
					certs := cstate.PeerCertificates
					Expect(len(certs)).To(Equal(3))
					Expect(certs[0].Subject.CommonName).To(Equal("c." + test_util.LocalhostDNS))
					Expect(certs[1].Subject.CommonName).To(Equal("b." + test_util.LocalhostDNS))
					Expect(certs[2].Subject.CommonName).To(Equal("a." + test_util.LocalhostDNS))

				})

			})

		})
		Context("when server name does not match anything", func() {
			It("returns the default certificate", func() {
				tlsClientConfig.ServerName = "not-here.com"
				tlsClientConfig.InsecureSkipVerify = true

				app := test.NewGreetApp([]route.Uri{"test." + test_util.LocalhostDNS}, config.Port, mbusClient, nil)

				app.RegisterAndListen()
				Eventually(func() bool {
					return appRegistered(registry, app)
				}).Should(BeTrue())

				uri := fmt.Sprintf("test.%s:%d", test_util.LocalhostDNS, config.SSLPort)

				conn, err := tls.Dial("tcp", uri, tlsClientConfig)
				Expect(err).ToNot(HaveOccurred())
				defer conn.Close()
				cstate := conn.ConnectionState()
				certs := cstate.PeerCertificates
				Expect(len(certs)).To(Equal(1))
				Expect(certs[0].Subject.CommonName).To(Equal("default"))
			})
		})

		Context("when no server name header is provided", func() {
			BeforeEach(func() {
				tlsClientConfig.ServerName = ""
			})

			It("uses a cert that matches the hostname", func() {
				app := test.NewGreetApp([]route.Uri{"test." + test_util.LocalhostDNS}, config.Port, mbusClient, nil)

				app.RegisterAndListen()
				Eventually(func() bool {
					return appRegistered(registry, app)
				}).Should(BeTrue())

				uri := fmt.Sprintf("test.%s:%d", test_util.LocalhostDNS, config.SSLPort)

				conn, err := tls.Dial("tcp", uri, tlsClientConfig)
				Expect(err).ToNot(HaveOccurred())
				cstate := conn.ConnectionState()
				certs := cstate.PeerCertificates
				Expect(len(certs)).To(Equal(1))
				Expect(certs[0].Subject.CommonName).To(Equal("test." + test_util.LocalhostDNS))
			})

			It("uses the default cert when hostname does not match any cert", func() {
				tlsClientConfig.InsecureSkipVerify = true

				app := test.NewGreetApp([]route.Uri{"notexist." + test_util.LocalhostDNS}, config.Port, mbusClient, nil)

				app.RegisterAndListen()
				Eventually(func() bool {
					return appRegistered(registry, app)
				}).Should(BeTrue())

				uri := fmt.Sprintf("notexist.%s:%d", test_util.LocalhostDNS, config.SSLPort)

				conn, err := tls.Dial("tcp", uri, tlsClientConfig)
				Expect(err).ToNot(HaveOccurred())
				cstate := conn.ConnectionState()
				certs := cstate.PeerCertificates
				Expect(len(certs)).To(Equal(1))
				Expect(certs[0].Subject.CommonName).To(Equal("default"))
			})
		})

		Context("when gorouter is configured with ClientAuth=RequireAndVerifyClientCert but the client doesn't provide a certificate", func() {
			BeforeEach(func() {
				config.ClientCertificateValidation = tls.RequireAndVerifyClientCert
			})

			It("fails the connection", func() {
				app := test.NewGreetApp([]route.Uri{"test." + test_util.LocalhostDNS}, config.Port, mbusClient, nil)
				app.RegisterAndListen()
				Eventually(func() bool {
					return appRegistered(registry, app)
				}).Should(BeTrue())

				uri := fmt.Sprintf("https://test.%s:%d/", test_util.LocalhostDNS, config.SSLPort)
				req, _ := http.NewRequest("GET", uri, nil)
				tr := &http.Transport{
					TLSClientConfig: &tls.Config{
						InsecureSkipVerify: true,
						Certificates:       []tls.Certificate{ /* no client cert! */ },
					},
				}

				client := http.Client{Transport: tr}
				resp, err := client.Do(req)
				Expect(err).To(MatchError(ContainSubstring("remote error: tls: bad certificate")))
				Expect(resp).To(BeNil())
			})
		})

		Context("when a client provides a certificate", func() {
			var (
				rootCert   *x509.Certificate
				rootKey    *rsa.PrivateKey
				clientCert *tls.Certificate
			)

			BeforeEach(func() {
				var (
					err error
				)
				rootCert, rootKey, _, err = createRootCA("rootCA")
				Expect(err).ToNot(HaveOccurred())

				clientCertTemplate, err := certTemplate("clientSSL")
				Expect(err).ToNot(HaveOccurred())
				clientCert, err = createClientCert(clientCertTemplate, rootCert, rootKey)
				Expect(err).ToNot(HaveOccurred())
			})

			var expectSuccessfulConnection = func() {
				app := test.NewGreetApp([]route.Uri{"test." + test_util.LocalhostDNS}, config.Port, mbusClient, nil)
				app.RegisterAndListen()
				Eventually(func() bool {
					return appRegistered(registry, app)
				}).Should(BeTrue())

				uri := fmt.Sprintf("https://test.%s:%d/", test_util.LocalhostDNS, config.SSLPort)
				req, _ := http.NewRequest("GET", uri, nil)

				tlsClientConfig.Certificates = []tls.Certificate{*clientCert}

				resp, err := client.Do(req)
				Expect(err).ToNot(HaveOccurred())
				Expect(resp).ToNot(BeNil())

				Expect(resp.StatusCode).To(Equal(http.StatusOK))

				bytes, err := ioutil.ReadAll(resp.Body)
				Expect(err).ToNot(HaveOccurred())
				Expect(bytes).To(ContainSubstring("Hello"))
				defer resp.Body.Close()
			}

			Context("when the client cert is valid ", func() {
				Context("when the cert is not included in clientCAPool", func() {
					Context("when gorouter is not configured to verify the cert", func() {
						BeforeEach(func() {
							config.ClientCertificateValidation = tls.NoClientCert
						})
						It("successfully serves SSL traffic", func() {
							expectSuccessfulConnection()
						})
					})

					Context("when gorouter is configured to verify the cert", func() {
						BeforeEach(func() {
							config.ClientCertificateValidation = tls.VerifyClientCertIfGiven
						})
						It("unsuccessfully serves SSL traffic", func() {
							app := test.NewGreetApp([]route.Uri{"test." + test_util.LocalhostDNS}, config.Port, mbusClient, nil)
							app.RegisterAndListen()
							Eventually(func() bool {
								return appRegistered(registry, app)
							}).Should(BeTrue())

							uri := fmt.Sprintf("https://test.%s:%d/", test_util.LocalhostDNS, config.SSLPort)
							req, _ := http.NewRequest("GET", uri, nil)

							tlsClientConfig.Certificates = []tls.Certificate{*clientCert}

							_, err := client.Do(req)
							Expect(err).To(HaveOccurred())
						})
					})
				})

				Context("when the cert is included in clientCAPool", func() {
					BeforeEach(func() {
						// creating and assigning pool because this test does not run buildClientCertPool
						rootCAs := x509.NewCertPool()
						rootCAs.AddCert(rootCert)
						config.ClientCAPool = rootCAs
					})

					Context("when gorouter is not configured to verify the cert", func() {
						BeforeEach(func() {
							config.ClientCertificateValidation = tls.NoClientCert
						})
						It("successfully serves SSL traffic", func() {
							expectSuccessfulConnection()
						})
					})

					Context("when gorouter is configured to verify the cert", func() {
						BeforeEach(func() {
							config.ClientCertificateValidation = tls.VerifyClientCertIfGiven
						})
						It("unsuccessfully serves SSL traffic", func() {
							expectSuccessfulConnection()
						})
					})
				})
			})

			Context("when the client cert is invalid", func() {
				BeforeEach(func() {
					//client presents expired certificate signed by server-trusted CA
					badCertTemplate, err := badCertTemplate("invalidClientSSL")
					Expect(err).ToNot(HaveOccurred())
					clientCert, err = createClientCert(badCertTemplate, rootCert, rootKey)
					Expect(err).ToNot(HaveOccurred())
				})

				Context("when gorouter is configured with ClientAuth = NoClientCert", func() {
					BeforeEach(func() {
						config.ClientCertificateValidation = tls.NoClientCert
					})
					Specify("the connection succeeds because the client cert is ignored", func() {
						app := test.NewGreetApp([]route.Uri{"test." + test_util.LocalhostDNS}, config.Port, mbusClient, nil)
						app.RegisterAndListen()
						Eventually(func() bool {
							return appRegistered(registry, app)
						}).Should(BeTrue())

						uri := fmt.Sprintf("https://test.%s:%d/", test_util.LocalhostDNS, config.SSLPort)
						req, _ := http.NewRequest("GET", uri, nil)
						tr := &http.Transport{
							TLSClientConfig: &tls.Config{
								InsecureSkipVerify: true,
								Certificates: []tls.Certificate{
									*clientCert,
								},
							},
						}

						client := http.Client{Transport: tr}
						resp, err := client.Do(req)
						Expect(err).ToNot(HaveOccurred())
						defer resp.Body.Close()
						Expect(resp).ToNot(BeNil())

						Expect(resp.StatusCode).To(Equal(http.StatusOK))
					})
				})

				Context("when gorouter is configured with ClientAuth = VerifyClientCertIfGiven", func() {
					BeforeEach(func() {
						config.ClientCertificateValidation = tls.VerifyClientCertIfGiven
					})
					It("fails the connection", func() {
						app := test.NewGreetApp([]route.Uri{"test." + test_util.LocalhostDNS}, config.Port, mbusClient, nil)
						app.RegisterAndListen()
						Eventually(func() bool {
							return appRegistered(registry, app)
						}).Should(BeTrue())

						uri := fmt.Sprintf("https://test.%s:%d/", test_util.LocalhostDNS, config.SSLPort)
						req, _ := http.NewRequest("GET", uri, nil)
						tr := &http.Transport{
							TLSClientConfig: &tls.Config{
								InsecureSkipVerify: true,
								Certificates: []tls.Certificate{
									*clientCert,
								},
							},
						}

						client := http.Client{Transport: tr}
						resp, err := client.Do(req)
						Expect(err).To(HaveOccurred())
						Expect(resp).To(BeNil())
					})
				})
			})

		})

	})

	Describe("frontend timeouts", func() {
		Context("when the frontend connection idles for more than the configured IdleTimeout", func() {
			BeforeEach(func() {
				config.FrontendIdleTimeout = 500 * time.Millisecond
			})
			It("closes the TCP connection", func() {
				conn, err := net.Dial("tcp", fmt.Sprintf("some-app.%s:%d", test_util.LocalhostDNS, config.Port))
				Expect(err).NotTo(HaveOccurred())

				_, err = conn.Write([]byte("GET /index.html HTTP/1.1\nHost: www.example.com\n\n"))
				Expect(err).NotTo(HaveOccurred())

				buffer := make([]byte, 1024)
				_, err = conn.Read(buffer)
				Expect(err).NotTo(HaveOccurred())

				readErr := make(chan error, 1)
				go func() {
					_, err := conn.Read(buffer)
					readErr <- err
				}()
				// now connection is idle

				Consistently(readErr, "200ms").ShouldNot(Receive()) // connection stays open

				Eventually(readErr, "1s").Should(Receive(Equal(io.EOF))) // connection is closed
			})
		})
	})
})

func createClientCert(clientCertTmpl *x509.Certificate, rootCert *x509.Certificate, rootKey *rsa.PrivateKey) (*tls.Certificate, error) {
	// generate a key pair for the client
	clientKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, err
	}

	clientCertTmpl.KeyUsage = x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature
	clientCertTmpl.ExtKeyUsage = []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}
	clientCertTmpl.IPAddresses = []net.IP{net.ParseIP("127.0.0.1")}

	// create a certificate which wraps the server's public key, sign it with the root private key
	// pretending rootCert belongs to CA
	_, clientCertPEM, err := createCert(clientCertTmpl, rootCert, &clientKey.PublicKey, rootKey)
	if err != nil {
		return nil, err
	}
	// provide the private key and the cert
	clientKeyPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(clientKey),
	})
	clientCert, err := tls.X509KeyPair(clientCertPEM, clientKeyPEM)
	if err != nil {
		return nil, err
	}
	return &clientCert, nil

}

func createRootCA(cname string) (*x509.Certificate, *rsa.PrivateKey, []byte, error) {
	rootKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, nil, nil, err
	}

	rootCertTmpl, err := certTemplate(cname)
	if err != nil {
		return nil, nil, nil, err
	}
	rootCertTmpl.IsCA = true
	rootCertTmpl.KeyUsage = x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature
	rootCertTmpl.ExtKeyUsage = []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth}
	rootCertTmpl.IPAddresses = []net.IP{net.ParseIP("127.0.0.1")}

	rootCert, rootPEM, err := createCert(rootCertTmpl, rootCertTmpl, &rootKey.PublicKey, rootKey)
	if err != nil {
		return nil, nil, nil, err
	}
	return rootCert, rootKey, rootPEM, err
}

func createCert(template, parent *x509.Certificate, pub, parentPriv interface{}) (cert *x509.Certificate, certPEM []byte, err error) {
	certDER, err := x509.CreateCertificate(rand.Reader, template, parent, pub, parentPriv)
	if err != nil {
		return
	}
	cert, err = x509.ParseCertificate(certDER)
	if err != nil {
		return
	}
	//PEM encoded cert (standard TLS encoding)
	b := pem.Block{Type: "CERTIFICATE", Bytes: certDER}
	certPEM = pem.EncodeToMemory(&b)
	return
}

// helper func to crate cert template with a serial number and other fields
func certTemplate(cname string) (*x509.Certificate, error) {
	// generate a random serial number (a real cert authority would have some logic behind this)
	serialNumberLimit := new(big.Int).Lsh(big.NewInt(1), 128)
	serialNumber, err := rand.Int(rand.Reader, serialNumberLimit)
	if err != nil {
		return nil, err
	}

	subject := pkix.Name{Organization: []string{"xyz, Inc."}}
	if cname != "" {
		subject.CommonName = cname
	}

	tmpl := x509.Certificate{
		SerialNumber:          serialNumber,
		Subject:               subject,
		SignatureAlgorithm:    x509.SHA256WithRSA,
		NotBefore:             time.Now(),
		NotAfter:              time.Now().Add(time.Hour), // valid for an hour
		BasicConstraintsValid: true,
	}
	return &tmpl, nil
}

func badCertTemplate(cname string) (*x509.Certificate, error) {
	serialNumberLimit := new(big.Int).Lsh(big.NewInt(1), 128)
	serialNumber, err := rand.Int(rand.Reader, serialNumberLimit)
	if err != nil {
		return nil, err
	}

	subject := pkix.Name{Organization: []string{"xyz, Inc."}}
	if cname != "" {
		subject.CommonName = cname
	}

	tmpl := x509.Certificate{
		SerialNumber:       serialNumber,
		Subject:            subject,
		SignatureAlgorithm: x509.SHA256WithRSA,
		NotBefore:          time.Now(),
		NotAfter:           time.Now(), //cert will be expired when server verifies it
	}
	return &tmpl, nil
}

func initializeRouter(config *cfg.Config, backendIdleTimeout, requestTimeout time.Duration, registry *rregistry.RouteRegistry, varz vvarz.Varz, mbusClient *nats.Conn, logger logger.Logger, routeServicesServer *sharedfakes.RouteServicesServer) (*Router, error) {
	sender := new(fakeMetrics.MetricSender)
	batcher := new(fakeMetrics.MetricBatcher)
	metricReporter := &metrics.MetricsReporter{Sender: sender, Batcher: batcher}
	combinedReporter := &metrics.CompositeReporter{VarzReporter: varz, ProxyReporter: metricReporter}
	routeServiceConfig := routeservice.NewRouteServiceConfig(logger, true, config.RouteServicesHairpinning, config.EndpointTimeout, nil, nil, false)

	ew := errorwriter.NewPlaintextErrorWriter()

	proxyConfig := *config
	proxyConfig.EndpointTimeout = requestTimeout
	routeServicesTransport := &sharedfakes.RoundTripper{}
	p := proxy.NewProxy(logger, &accesslog.NullAccessLogger{}, ew, &proxyConfig, registry, combinedReporter,
		routeServiceConfig, &tls.Config{}, &tls.Config{}, &health.Health{}, routeServicesTransport)

	h := &health.Health{}
	logcounter := schema.NewLogCounter()
	config.EndpointTimeout = backendIdleTimeout
	router, e := NewRouter(logger, config, p, mbusClient, registry, varz, h, logcounter, nil, routeServicesServer)

	h.OnDegrade = router.DrainAndStop

	return router, e
}

func readVarz(v vvarz.Varz) map[string]interface{} {
	varz_byte, err := v.MarshalJSON()
	Expect(err).ToNot(HaveOccurred())

	varz_data := make(map[string]interface{})
	err = json.Unmarshal(varz_byte, &varz_data)
	Expect(err).ToNot(HaveOccurred())

	return varz_data
}

func fetchRecursively(x interface{}, s ...string) interface{} {
	var ok bool

	for _, y := range s {
		z := x.(map[string]interface{})
		x, ok = z[y]
		Expect(ok).To(BeTrue(), fmt.Sprintf("no key: %s", s))
	}

	return x
}

func sendAndReceive(req *http.Request, statusCode int) []byte {
	var client http.Client
	resp, err := client.Do(req)
	Expect(err).ToNot(HaveOccurred())
	Expect(resp).ToNot(BeNil())
	Expect(resp.StatusCode).To(Equal(statusCode))
	defer resp.Body.Close()

	bytes, err := ioutil.ReadAll(resp.Body)
	Expect(err).ToNot(HaveOccurred())

	return bytes
}

func sendRequests(url string, rPort uint16, times int) {
	uri := fmt.Sprintf("http://%s:%d", url, rPort)

	for i := 0; i < times; i++ {
		r, err := http.Get(uri)
		Expect(err).ToNot(HaveOccurred())

		Expect(r.StatusCode).To(Equal(http.StatusOK))
		// Close the body to avoid open files limit error
		r.Body.Close()
	}
}

func getSessionAndAppPort(url string, rPort uint16) (*http.Response, string) {
	var client http.Client
	var req *http.Request
	var resp *http.Response
	var err error
	var port []byte

	uri := fmt.Sprintf("http://%s:%d/sticky", url, rPort)

	req, err = http.NewRequest("GET", uri, nil)
	Expect(err).ToNot(HaveOccurred())

	resp, err = client.Do(req)
	Expect(err).ToNot(HaveOccurred())

	port, err = ioutil.ReadAll(resp.Body)
	Expect(err).ToNot(HaveOccurred())

	return resp, string(port)
}

func getAppPortWithSticky(url string, rPort uint16, respCookies []*http.Cookie) string {
	var client http.Client
	var req *http.Request
	var resp *http.Response
	var err error
	var port []byte

	uri := fmt.Sprintf("http://%s:%d/sticky", url, rPort)

	req, err = http.NewRequest("GET", uri, nil)
	Expect(err).ToNot(HaveOccurred())

	for _, cookie := range respCookies {
		req.AddCookie(cookie)
	}

	resp, err = client.Do(req)
	Expect(err).ToNot(HaveOccurred())

	port, err = ioutil.ReadAll(resp.Body)
	Expect(err).ToNot(HaveOccurred())

	return string(port)
}

func assertServerResponse(client *httputil.ClientConn, req *http.Request) {
	var resp *http.Response
	var err error

	for i := 0; i < 3; i++ {
		resp, err = client.Do(req)
		Expect(err).ToNot(HaveOccurred())
		Expect(resp).ToNot(BeNil())
		resp.Body.Close()
		if resp.StatusCode == http.StatusOK {
			break
		}

		time.Sleep(10 * time.Millisecond)
	}

	Expect(resp.StatusCode).To(Equal(http.StatusOK))
}

func routeExists(config *config.Config, routeName string) (bool, error) {
	host := fmt.Sprintf("http://%s:%d/routes", config.Ip, config.Status.Port)
	req, err := http.NewRequest("GET", host, nil)
	Expect(err).ToNot(HaveOccurred())
	req.SetBasicAuth("user", "pass")

	var client http.Client
	resp, err := client.Do(req)
	Expect(err).ToNot(HaveOccurred())
	Expect(resp).ToNot(BeNil())

	switch resp.StatusCode {
	case http.StatusOK:
		bytes, err := ioutil.ReadAll(resp.Body)
		resp.Body.Close()
		Expect(err).ToNot(HaveOccurred())
		routes := make(map[string]interface{})
		err = json.Unmarshal(bytes, &routes)
		Expect(err).ToNot(HaveOccurred())

		_, found := routes[routeName]
		return found, nil

	default:
		return false, errors.New("Didn't get an OK response")
	}
}

func newSlowApp(urls []route.Uri, rPort uint16, mbusClient *nats.Conn, delay time.Duration) *common.TestApp {
	app := common.NewTestApp(urls, rPort, mbusClient, nil, "")

	app.AddHandler("/", func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(delay)
	})

	app.AddHandler("/partialresponse", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTeapot)
		io.WriteString(w, "hello, world")
		w.(http.Flusher).Flush()
		time.Sleep(delay)
	})

	return app
}
