package router_test

import (
	"os"

	"github.com/cloudfoundry/dropsonde"
	"github.com/cloudfoundry/dropsonde/emitter/fake"
	"github.com/cloudfoundry/gorouter/access_log"
	"github.com/cloudfoundry/gorouter/common"
	"github.com/cloudfoundry/gorouter/common/health"
	router_http "github.com/cloudfoundry/gorouter/common/http"
	"github.com/cloudfoundry/gorouter/common/schema"
	cfg "github.com/cloudfoundry/gorouter/config"
	"github.com/cloudfoundry/gorouter/proxy"
	rregistry "github.com/cloudfoundry/gorouter/registry"
	"github.com/cloudfoundry/gorouter/route"
	. "github.com/cloudfoundry/gorouter/router"
	"github.com/cloudfoundry/gorouter/test"
	"github.com/cloudfoundry/gorouter/test_util"
	vvarz "github.com/cloudfoundry/gorouter/varz"
	"github.com/nats-io/nats"
	. "github.com/onsi/ginkgo"
	gConfig "github.com/onsi/ginkgo/config"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gbytes"

	"bufio"
	"bytes"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net"
	"net/http"
	"net/http/httputil"
	"strings"
	"time"

	"github.com/cloudfoundry/gorouter/metrics/reporter/fakes"
	"github.com/pivotal-golang/lager"
	"github.com/pivotal-golang/lager/lagertest"
)

var _ = Describe("Router", func() {

	const uuid_regex = `^[[:xdigit:]]{8}(-[[:xdigit:]]{4}){3}-[[:xdigit:]]{12}$`

	var (
		natsRunner *test_util.NATSRunner
		natsPort   uint16
		config     *cfg.Config

		mbusClient   *nats.Conn
		registry     *rregistry.RouteRegistry
		varz         vvarz.Varz
		router       *Router
		signals      chan os.Signal
		closeChannel chan struct{}
		readyChan    chan struct{}
		logger       lager.Logger
	)

	BeforeEach(func() {
		natsPort = test_util.NextAvailPort()
		natsRunner = test_util.NewNATSRunner(int(natsPort))
		natsRunner.Start()

		fakeEmitter := fake.NewFakeEventEmitter("fake")
		dropsonde.InitializeWithEmitter(fakeEmitter)

		proxyPort := test_util.NextAvailPort()
		statusPort := test_util.NextAvailPort()

		cert, err := tls.LoadX509KeyPair("../test/assets/public.pem", "../test/assets/private.pem")
		Expect(err).ToNot(HaveOccurred())

		config = test_util.SpecConfig(statusPort, proxyPort, natsPort)
		config.EnableSSL = true
		config.SSLPort = 4443 + uint16(gConfig.GinkgoConfig.ParallelNode)
		config.SSLCertificate = cert
		config.CipherSuites = []uint16{tls.TLS_RSA_WITH_AES_256_CBC_SHA}

		// set pid file
		f, err := ioutil.TempFile("", "gorouter-test-pidfile-")
		Expect(err).ToNot(HaveOccurred())
		config.PidFile = f.Name()

		mbusClient = natsRunner.MessageBus
		logger = lagertest.NewTestLogger("router-test")
		registry = rregistry.NewRouteRegistry(logger, config, new(fakes.FakeRouteRegistryReporter))
		varz = vvarz.NewVarz(registry)
		logcounter := schema.NewLogCounter()
		proxy := proxy.NewProxy(proxy.ProxyArgs{
			EndpointTimeout: config.EndpointTimeout,
			Logger:          logger,
			Ip:              config.Ip,
			TraceKey:        config.TraceKey,
			Registry:        registry,
			Reporter:        varz,
			AccessLogger:    &access_log.NullAccessLogger{},
		})

		router, err = NewRouter(logger, config, proxy, mbusClient, registry, varz, logcounter, nil)

		Expect(err).ToNot(HaveOccurred())

		readyChan = make(chan struct{})
		closeChannel = make(chan struct{})
		go func() {
			router.Run(signals, readyChan)
			close(closeChannel)
		}()
		select {
		case <-readyChan:
		}

	})

	AfterEach(func() {
		if natsRunner != nil {
			natsRunner.Stop()
		}

		if router != nil {
			router.Stop()

			if config.PidFile != "" {
				// remove pid file
				err := os.Remove(config.PidFile)
				Expect(err).ToNot(HaveOccurred())
			}
		}

	})

	Context("NATS", func() {
		Context("Router Greetings", func() {
			It("RouterGreets", func() {
				response := make(chan []byte)

				mbusClient.Subscribe("router.greet.test.response", func(msg *nats.Msg) {
					response <- msg.Data
				})

				mbusClient.PublishRequest("router.greet", "router.greet.test.response", []byte{})

				var msg []byte
				Eventually(response).Should(Receive(&msg))

				var message common.RouterStart
				err := json.Unmarshal(msg, &message)

				Expect(err).NotTo(HaveOccurred())
				Expect(message.MinimumRegisterIntervalInSeconds).To(Equal(5))
				Expect(message.PruneThresholdInSeconds).To(Equal(120))
			})

			It("handles a empty reply on greet", func() {
				err := mbusClient.PublishRequest("router.greet", "", []byte{})
				Expect(err).NotTo(HaveOccurred())

				Consistently(func() error {
					return mbusClient.PublishRequest("router.greet", "test", []byte{})
				}).ShouldNot(HaveOccurred())
			})
		})

		It("discovers", func() {
			// Test if router responses to discover message
			sig := make(chan health.Varz)

			// Since the form of uptime is xxd:xxh:xxm:xxs, we should make
			// sure that router has run at least for one second
			time.Sleep(time.Second)

			mbusClient.Subscribe("vcap.component.discover.test.response", func(msg *nats.Msg) {
				var varz health.Varz
				_ = json.Unmarshal(msg.Data, &varz)
				sig <- varz
			})

			mbusClient.PublishRequest(
				"vcap.component.discover",
				"vcap.component.discover.test.response",
				[]byte{},
			)

			var varz health.Varz
			Eventually(sig).Should(Receive(&varz))

			var emptyTime time.Time
			var emptyDuration schema.Duration

			Expect(varz.Type).To(Equal("Router"))
			Expect(varz.Index).To(Equal(uint(2)))
			Expect(varz.UUID).ToNot(Equal(""))
			Expect(varz.StartTime).ToNot(Equal(emptyTime))
			Expect(varz.Uptime).ToNot(Equal(emptyDuration))

			verify_var_z(varz.Host, varz.Credentials[0], varz.Credentials[1])
			verify_health_z(varz.Host, registry)
		})

		Context("Register and Unregister", func() {
			var app *test.TestApp

			assertRegisterUnregister := func() {
				app.Listen()

				Eventually(func() bool {
					return appRegistered(registry, app)
				}).Should(BeTrue())

				app.VerifyAppStatus(200)

				app.Unregister()

				Eventually(func() bool {
					return appUnregistered(registry, app)
				}).Should(BeTrue())

				app.VerifyAppStatus(404)
			}

			Describe("app with no route service", func() {
				BeforeEach(func() {
					app = test.NewGreetApp([]route.Uri{"test.vcap.me"}, config.Port, mbusClient, nil)
				})

				It("registers and unregisters", func() {
					assertRegisterUnregister()
				})
			})

			Describe("app with an http route service", func() {
				BeforeEach(func() {
					app = test.NewRouteServiceApp([]route.Uri{"test.vcap.me"}, config.Port, mbusClient, "http://my-insecure-service.me")
				})

				It("does not register", func() {
					app.Listen()

					Consistently(func() bool {
						return appRegistered(registry, app)
					}).Should(BeFalse())

					app.VerifyAppStatus(404)
				})
			})
		})
	})

	It("sends start on a nats connect", func() {
		started := make(chan bool)
		cb := make(chan bool)

		mbusClient.Subscribe("router.start", func(*nats.Msg) {
			started <- true
		})

		reconnectedCbs := make([]func(*nats.Conn), 0)
		reconnectedCbs = append(reconnectedCbs, mbusClient.Opts.ReconnectedCB)
		reconnectedCbs = append(reconnectedCbs, func(_ *nats.Conn) {
			cb <- true
		})
		mbusClient.Opts.ReconnectedCB = func(conn *nats.Conn) {
			for _, rcb := range reconnectedCbs {
				rcb(conn)
			}
		}

		natsRunner.Stop()
		natsRunner.Start()

		Eventually(started, 4).Should(Receive())
		Eventually(cb, 4).Should(Receive())
	})

	It("creates a pidfile on startup", func() {

		Eventually(func() bool {
			_, err := os.Stat(config.PidFile)
			return err == nil
		}).Should(BeTrue())
	})

	It("registry contains last updated varz", func() {
		app1 := test.NewGreetApp([]route.Uri{"test1.vcap.me"}, config.Port, mbusClient, nil)
		app1.Listen()

		Eventually(func() bool {
			return appRegistered(registry, app1)
		}).Should(BeTrue())

		time.Sleep(100 * time.Millisecond)
		initialUpdateTime := fetchRecursively(readVarz(varz), "ms_since_last_registry_update").(float64)

		app2 := test.NewGreetApp([]route.Uri{"test2.vcap.me"}, config.Port, mbusClient, nil)
		app2.Listen()
		Eventually(func() bool {
			return appRegistered(registry, app2)
		}).Should(BeTrue())

		// updateTime should be after initial update time
		updateTime := fetchRecursively(readVarz(varz), "ms_since_last_registry_update").(float64)
		Expect(updateTime).To(BeNumerically("<", initialUpdateTime))
	})

	It("varz", func() {
		app := test.NewGreetApp([]route.Uri{"count.vcap.me"}, config.Port, mbusClient, map[string]string{"framework": "rails"})
		app.Listen()
		additionalRequests := 100
		go app.RegisterRepeatedly(100 * time.Millisecond)

		Eventually(func() bool {
			return appRegistered(registry, app)
		}).Should(BeTrue())

		// Send seed request
		sendRequests("count.vcap.me", config.Port, 1)
		initial_varz := readVarz(varz)

		// Send requests
		sendRequests("count.vcap.me", config.Port, additionalRequests)
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

	It("sticky session", func() {
		apps := make([]*test.TestApp, 10)
		for i := range apps {
			apps[i] = test.NewStickyApp([]route.Uri{"sticky.vcap.me"}, config.Port, mbusClient, nil)
			apps[i].Listen()
		}

		for _, app := range apps {
			Eventually(func() bool {
				return appRegistered(registry, app)
			}).Should(BeTrue())
		}
		sessionCookie, vcapCookie, port1 := getSessionAndAppPort("sticky.vcap.me", config.Port)
		port2 := getAppPortWithSticky("sticky.vcap.me", config.Port, sessionCookie, vcapCookie)

		Expect(port1).To(Equal(port2))
		Expect(vcapCookie.Path).To(Equal("/"))

		for _, app := range apps {
			app.Unregister()
		}
	})

	Context("Stop", func() {
		It("no longer proxies http", func() {
			app := test.NewTestApp([]route.Uri{"greet.vcap.me"}, config.Port, mbusClient, nil, "")

			app.AddHandler("/", func(w http.ResponseWriter, r *http.Request) {
				_, err := ioutil.ReadAll(r.Body)
				defer r.Body.Close()
				Expect(err).ToNot(HaveOccurred())
				w.WriteHeader(http.StatusNoContent)
			})
			app.Listen()
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
			app := test.NewTestApp([]route.Uri{"greet.vcap.me"}, config.Port, mbusClient, nil, "")

			app.AddHandler("/", func(w http.ResponseWriter, r *http.Request) {
				_, err := ioutil.ReadAll(r.Body)
				defer r.Body.Close()
				Expect(err).ToNot(HaveOccurred())
				w.WriteHeader(http.StatusNoContent)
			})
			app.Listen()
			Eventually(func() bool {
				return appRegistered(registry, app)
			}).Should(BeTrue())

			host := fmt.Sprintf("https://greet.vcap.me:%d/", config.SSLPort)

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
			_, err = client.Do(req)
			Expect(err).To(HaveOccurred())
		})
	})

	It("handles a PUT request", func() {
		app := test.NewTestApp([]route.Uri{"greet.vcap.me"}, config.Port, mbusClient, nil, "")

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
		app.Listen()
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
		app := test.NewTestApp([]route.Uri{"foo.vcap.me"}, config.Port, mbusClient, nil, "")
		rCh := make(chan *http.Request)
		app.AddHandler("/", func(w http.ResponseWriter, r *http.Request) {
			_, err := ioutil.ReadAll(r.Body)
			if err != nil {
				w.WriteHeader(http.StatusBadRequest)
			}
			rCh <- r
		})

		app.Listen()
		go app.RegisterRepeatedly(1 * time.Second)

		Eventually(func() bool {
			return appRegistered(registry, app)
		}).Should(BeTrue())

		host := fmt.Sprintf("foo.vcap.me:%d", config.Port)
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
		Expect(strings.Contains(line, "100 Continue")).To(BeTrue())

		var rr *http.Request
		Eventually(rCh).Should(Receive(&rr))
		Expect(rr).ToNot(BeNil())
		Expect(rr.Header.Get("Expect")).To(Equal(""))
	})

	It("X-Vcap-Request-Id header is overwritten", func() {
		done := make(chan string)
		app := test.NewTestApp([]route.Uri{"foo.vcap.me"}, config.Port, mbusClient, nil, "")
		app.AddHandler("/", func(w http.ResponseWriter, r *http.Request) {
			_, err := ioutil.ReadAll(r.Body)
			Expect(err).NotTo(HaveOccurred())
			w.WriteHeader(http.StatusOK)
			done <- r.Header.Get(router_http.VcapRequestIdHeader)
		})

		app.Listen()
		go app.RegisterRepeatedly(1 * time.Second)

		Eventually(func() bool {
			return appRegistered(registry, app)
		}).Should(BeTrue())

		conn, err := net.Dial("tcp", fmt.Sprintf("%s:%d", config.Ip, config.Port))
		Expect(err).NotTo(HaveOccurred())
		defer conn.Close()

		httpConn := test_util.NewHttpConn(conn)

		req := test_util.NewRequest("GET", "foo.vcap.me", "/", nil)
		req.Header.Add(router_http.VcapRequestIdHeader, "A-BOGUS-REQUEST-ID")

		httpConn.WriteRequest(req)

		var answer string
		Eventually(done).Should(Receive(&answer))
		Expect(answer).ToNot(Equal("A-BOGUS-REQUEST-ID"))
		Expect(answer).To(MatchRegexp(uuid_regex))
		Expect(logger).To(gbytes.Say("vcap-request-id-header-set"))

		resp, _ := httpConn.ReadResponse()
		Expect(resp.StatusCode).To(Equal(http.StatusOK))
	})

	It("handles a /routes request", func() {
		var client http.Client
		var req *http.Request
		var resp *http.Response
		var err error

		mbusClient.Publish("router.register", []byte(`{"dea":"dea1","app":"app1","uris":["test.com"],"host":"1.2.3.4","port":1234,"tags":{},"private_instance_id":"private_instance_id"}`))
		time.Sleep(250 * time.Millisecond)

		host := fmt.Sprintf("http://%s:%d/routes", config.Ip, config.Status.Port)

		req, err = http.NewRequest("GET", host, nil)
		req.SetBasicAuth("user", "pass")

		resp, err = client.Do(req)
		Expect(err).ToNot(HaveOccurred())
		Expect(resp).ToNot(BeNil())
		Expect(resp.StatusCode).To(Equal(200))

		body, err := ioutil.ReadAll(resp.Body)
		defer resp.Body.Close()
		Expect(err).ToNot(HaveOccurred())
		Expect(string(body)).To(MatchRegexp(".*1\\.2\\.3\\.4:1234.*\n"))
	})

	Context("HTTP keep-alive", func() {
		It("reuses the same connection on subsequent calls", func() {
			app := test.NewGreetApp([]route.Uri{"keepalive.vcap.me"}, config.Port, mbusClient, nil)
			app.Listen()
			Eventually(func() bool {
				return appRegistered(registry, app)
			}).Should(BeTrue())

			host := fmt.Sprintf("keepalive.vcap.me:%d", config.Port)
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
			app := test.NewGreetApp([]route.Uri{"keepalive.vcap.me"}, config.Port, mbusClient, nil)
			app.Listen()
			Eventually(func() bool {
				return appRegistered(registry, app)
			}).Should(BeTrue())

			host := fmt.Sprintf("keepalive.vcap.me:%d", config.Port)
			uri := fmt.Sprintf("http://%s", host)

			conn, err := net.Dial("tcp", host)
			Expect(err).ToNot(HaveOccurred())

			client := httputil.NewClientConn(conn, nil)
			req, _ := http.NewRequest("GET", uri, nil)
			Expect(req.Close).To(BeFalse())

			// initiate idle timeout
			assertServerResponse(client, req)

			// use 3/4 of the idle timeout
			time.Sleep(config.EndpointTimeout / 4 * 3)

			//make second request without errors
			resp, err := client.Do(req)
			Expect(err).ToNot(HaveOccurred())
			Expect(resp).ToNot(BeNil())
			resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusOK))

			// use another 3/4 of the idle timeout, exceeding the original timeout
			time.Sleep(config.EndpointTimeout / 4 * 3)

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
			app := test.NewSlowApp(
				[]route.Uri{"keepalive.vcap.me"},
				config.Port,
				mbusClient,
				config.EndpointTimeout/4*3,
			)
			app.Listen()
			Eventually(func() bool {
				return appRegistered(registry, app)
			}).Should(BeTrue())

			host := fmt.Sprintf("keepalive.vcap.me:%d", config.Port)
			uri := fmt.Sprintf("http://%s", host)

			conn, err := net.Dial("tcp", host)
			Expect(err).ToNot(HaveOccurred())

			client := httputil.NewClientConn(conn, nil)
			req, _ := http.NewRequest("GET", uri, nil)
			Expect(req.Close).To(BeFalse())

			// initiate idle timeout
			assertServerResponse(client, req)

			// use 3/4 of the idle timeout
			time.Sleep(config.EndpointTimeout / 4 * 3)

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

	Context("long requests", func() {
		Context("http", func() {
			BeforeEach(func() {
				app := test.NewSlowApp(
					[]route.Uri{"slow-app.vcap.me"},
					config.Port,
					mbusClient,
					1*time.Second,
				)

				app.Listen()
				Eventually(func() bool {
					return appRegistered(registry, app)
				}).Should(BeTrue())
			})

			It("terminates before receiving headers", func() {
				uri := fmt.Sprintf("http://slow-app.vcap.me:%d", config.Port)
				req, _ := http.NewRequest("GET", uri, nil)
				client := http.Client{}
				resp, err := client.Do(req)
				Expect(err).ToNot(HaveOccurred())
				Expect(resp).ToNot(BeNil())
				Expect(resp.StatusCode).To(Equal(http.StatusBadGateway))
				defer resp.Body.Close()

				_, err = ioutil.ReadAll(resp.Body)
				Expect(err).ToNot(HaveOccurred())
			})

			It("terminates before receiving the body", func() {
				uri := fmt.Sprintf("http://slow-app.vcap.me:%d/hello", config.Port)
				req, _ := http.NewRequest("GET", uri, nil)
				client := http.Client{}
				resp, err := client.Do(req)
				Expect(err).ToNot(HaveOccurred())
				Expect(resp).ToNot(BeNil())
				Expect(resp.StatusCode).To(Equal(http.StatusOK))
				defer resp.Body.Close()

				body, err := ioutil.ReadAll(resp.Body)
				Expect(err).ToNot(HaveOccurred())
				Expect(body).To(HaveLen(0))
			})
		})

		It("websockets do not terminate", func() {
			app := test.NewWebSocketApp(
				[]route.Uri{"ws-app.vcap.me"},
				config.Port,
				mbusClient,
				1*time.Second,
			)
			app.Listen()
			Eventually(func() bool {
				return appRegistered(registry, app)
			}).Should(BeTrue())

			conn, err := net.Dial("tcp", fmt.Sprintf("ws-app.vcap.me:%d", config.Port))
			Expect(err).NotTo(HaveOccurred())

			x := test_util.NewHttpConn(conn)

			req := test_util.NewRequest("GET", "ws-app.vcap.me", "/chat", nil)
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

	Context("serving https", func() {
		It("serves ssl traffic", func() {
			app := test.NewGreetApp([]route.Uri{"test.vcap.me"}, config.Port, mbusClient, nil)
			app.Listen()
			Eventually(func() bool {
				return appRegistered(registry, app)
			}).Should(BeTrue())

			uri := fmt.Sprintf("https://test.vcap.me:%d", config.SSLPort)
			req, _ := http.NewRequest("GET", uri, nil)
			tr := &http.Transport{
				TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
			}
			client := http.Client{Transport: tr}
			resp, err := client.Do(req)
			Expect(err).ToNot(HaveOccurred())
			Expect(resp).ToNot(BeNil())
			resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusOK))
		})

		It("fails when the client uses an unsupported cipher suite", func() {
			app := test.NewGreetApp([]route.Uri{"test.vcap.me"}, config.Port, mbusClient, nil)
			app.Listen()
			Eventually(func() bool {
				return appRegistered(registry, app)
			}).Should(BeTrue())

			uri := fmt.Sprintf("https://test.vcap.me:%d", config.SSLPort)
			req, _ := http.NewRequest("GET", uri, nil)
			tr := &http.Transport{
				TLSClientConfig: &tls.Config{
					InsecureSkipVerify: true,
					CipherSuites:       []uint16{tls.TLS_ECDHE_RSA_WITH_3DES_EDE_CBC_SHA},
				},
			}
			client := http.Client{Transport: tr}
			_, err := client.Do(req)
			Expect(err).To(HaveOccurred())
		})
	})

	Describe("SubscribeRegister", func() {
		Context("when the register message JSON fails to unmarshall", func() {
			BeforeEach(func() {
				// the port is too high
				mbusClient.Publish("router.register", []byte(`
{
  "dea": "dea1",
  "app": "app1",
  "uris": [
    "test.com"
  ],
  "host": "1.2.3.4",
  "port": 65536,
  "private_instance_id": "private_instance_id"
}
`))
			})

			It("does not add the route to the route table", func() {
				// Pool.IsEmpty() is better but the pool is not intialized yet
				Consistently(func() *route.Pool { return registry.Lookup("test.com") }).Should(BeZero())
			})
		})
	})
})

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

func verify_health_z(host string, r *rregistry.RouteRegistry) {
	var req *http.Request
	path := "/healthz"

	req, _ = http.NewRequest("GET", "http://"+host+path, nil)
	bytes := verify_success(req)
	Expect(string(bytes)).To(Equal("ok"))
}

func verify_var_z(host, user, pass string) {
	var client http.Client
	var req *http.Request
	var resp *http.Response
	var err error
	path := "/varz"

	// Request without username:password should be rejected
	req, _ = http.NewRequest("GET", "http://"+host+path, nil)
	resp, err = client.Do(req)
	Expect(err).ToNot(HaveOccurred())
	Expect(resp).ToNot(BeNil())
	Expect(resp.StatusCode).To(Equal(401))

	// varz Basic auth
	req.SetBasicAuth(user, pass)
	bytes := verify_success(req)
	varz := make(map[string]interface{})
	json.Unmarshal(bytes, &varz)

	Expect(varz["num_cores"]).ToNot(Equal(0))
	Expect(varz["type"]).To(Equal("Router"))
	Expect(varz["uuid"]).ToNot(Equal(""))
}

func verify_success(req *http.Request) []byte {
	return sendAndReceive(req, http.StatusOK)
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

func getSessionAndAppPort(url string, rPort uint16) (*http.Cookie, *http.Cookie, string) {
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

	var sessionCookie, vcapCookie *http.Cookie
	for _, cookie := range resp.Cookies() {
		if cookie.Name == proxy.StickyCookieKey {
			sessionCookie = cookie
		} else if cookie.Name == proxy.VcapCookieId {
			vcapCookie = cookie
		}
	}

	return sessionCookie, vcapCookie, string(port)
}

func getAppPortWithSticky(url string, rPort uint16, sessionCookie, vcapCookie *http.Cookie) string {
	var client http.Client
	var req *http.Request
	var resp *http.Response
	var err error
	var port []byte

	uri := fmt.Sprintf("http://%s:%d/sticky", url, rPort)

	req, err = http.NewRequest("GET", uri, nil)
	Expect(err).ToNot(HaveOccurred())

	req.AddCookie(sessionCookie)
	req.AddCookie(vcapCookie)

	resp, err = client.Do(req)
	Expect(err).ToNot(HaveOccurred())

	port, err = ioutil.ReadAll(resp.Body)

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
