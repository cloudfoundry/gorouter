package mbus_test

import (
	"encoding/json"
	"errors"
	"os"
	"sync/atomic"
	"time"

	"code.cloudfoundry.org/routing-api/models"

	"code.cloudfoundry.org/gorouter/common"
	"code.cloudfoundry.org/gorouter/config"
	"code.cloudfoundry.org/gorouter/logger"
	"code.cloudfoundry.org/gorouter/mbus"
	mbusFakes "code.cloudfoundry.org/gorouter/mbus/fakes"
	registryFakes "code.cloudfoundry.org/gorouter/registry/fakes"
	"code.cloudfoundry.org/gorouter/route"
	"code.cloudfoundry.org/gorouter/test_util"

	"github.com/nats-io/nats"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gbytes"
	"github.com/tedsuo/ifrit"
)

var _ = Describe("Subscriber", func() {
	var (
		sub     *mbus.Subscriber
		cfg     *config.Config
		process ifrit.Process

		registry *registryFakes.FakeRegistry

		natsRunner  *test_util.NATSRunner
		natsPort    uint16
		natsClient  *nats.Conn
		reconnected chan mbus.Signal

		l logger.Logger
	)

	BeforeEach(func() {
		natsPort = test_util.NextAvailPort()

		natsRunner = test_util.NewNATSRunner(int(natsPort))
		natsRunner.Start()
		natsClient = natsRunner.MessageBus

		registry = new(registryFakes.FakeRegistry)

		l = test_util.NewTestZapLogger("mbus-test")

		reconnected = make(chan mbus.Signal)
		var err error
		cfg, err = config.DefaultConfig()
		Expect(err).ToNot(HaveOccurred())
		cfg.Index = 4321
		cfg.StartResponseDelayInterval = 60 * time.Second
		cfg.DropletStaleThreshold = 120 * time.Second

		sub = mbus.NewSubscriber(natsClient, registry, cfg, reconnected, l)
	})

	AfterEach(func() {
		if natsRunner != nil {
			natsRunner.Stop()
		}
		if process != nil {
			process.Signal(os.Interrupt)
		}
		process = nil
	})

	It("exits when signaled", func() {
		process = ifrit.Invoke(sub)
		Eventually(process.Ready()).Should(BeClosed())

		process.Signal(os.Interrupt)
		var err error
		Eventually(process.Wait()).Should(Receive(&err))
		Expect(err).NotTo(HaveOccurred())
	})

	It("sends a start message", func() {
		msgChan := make(chan *nats.Msg, 1)

		_, err := natsClient.ChanSubscribe("router.start", msgChan)
		Expect(err).ToNot(HaveOccurred())

		process = ifrit.Invoke(sub)
		Eventually(process.Ready()).Should(BeClosed())

		var (
			msg      *nats.Msg
			startMsg common.RouterStart
		)
		Eventually(msgChan, 4).Should(Receive(&msg))
		Expect(msg).ToNot(BeNil())

		err = json.Unmarshal(msg.Data, &startMsg)
		Expect(err).ToNot(HaveOccurred())

		Expect(startMsg.Id).To(HavePrefix("4321-"))
		Expect(startMsg.Hosts).ToNot(BeEmpty())
		Expect(startMsg.MinimumRegisterIntervalInSeconds).To(Equal(int(cfg.StartResponseDelayInterval.Seconds())))
		Expect(startMsg.PruneThresholdInSeconds).To(Equal(int(cfg.DropletStaleThreshold.Seconds())))
	})

	It("errors when mbus client is nil", func() {
		sub = mbus.NewSubscriber(nil, registry, cfg, reconnected, l)
		process = ifrit.Invoke(sub)

		var err error
		Eventually(process.Wait()).Should(Receive(&err))
		Expect(err).To(MatchError("subscriber: nil mbus client"))
	})

	Context("Pending", func() {
		It("returns the subscription Pending value", func() {
			process = ifrit.Invoke(sub)
			Eventually(process.Ready()).Should(BeClosed())
			msgs, err := sub.Pending()
			Expect(msgs).To(BeNumerically(">=", 0))
			Expect(err).ToNot(HaveOccurred())
		})

		Context("when subscription is nil", func() {
			It("returns an error", func() {
				msgs, err := sub.Pending()
				Expect(msgs).To(Equal(-1))
				Expect(err).To(HaveOccurred())
			})
		})
	})

	Context("when publish start message fails", func() {
		var fakeClient *mbusFakes.FakeClient
		BeforeEach(func() {
			fakeClient = new(mbusFakes.FakeClient)
			fakeClient.PublishReturns(errors.New("potato"))
		})
		It("errors", func() {
			sub = mbus.NewSubscriber(fakeClient, registry, cfg, reconnected, l)
			process = ifrit.Invoke(sub)

			var err error
			Eventually(process.Wait()).Should(Receive(&err))
			Expect(err).To(MatchError("potato"))
		})
	})

	Context("when reconnecting", func() {
		BeforeEach(func() {
			process = ifrit.Invoke(sub)
			Eventually(process.Ready()).Should(BeClosed())
		})

		It("sends start message", func() {
			var atomicReconnect uint32
			msgChan := make(chan *nats.Msg, 1)
			_, err := natsClient.ChanSubscribe("router.start", msgChan)
			Expect(err).ToNot(HaveOccurred())

			reconnectedCbs := make([]func(*nats.Conn), 0)
			reconnectedCbs = append(reconnectedCbs, natsClient.Opts.ReconnectedCB)
			reconnectedCbs = append(reconnectedCbs, func(_ *nats.Conn) {
				atomic.StoreUint32(&atomicReconnect, 1)
				reconnected <- mbus.Signal{}
			})

			natsClient.Opts.ReconnectedCB = func(conn *nats.Conn) {
				for _, rcb := range reconnectedCbs {
					if rcb != nil {
						rcb(conn)
					}
				}
			}
			natsRunner.Stop()
			natsRunner.Start()

			var (
				msg      *nats.Msg
				startMsg common.RouterStart
			)
			Eventually(msgChan, 4).Should(Receive(&msg))
			Expect(msg).ToNot(BeNil())
			Expect(atomic.LoadUint32(&atomicReconnect)).To(Equal(uint32(1)))

			err = json.Unmarshal(msg.Data, &startMsg)
			Expect(err).ToNot(HaveOccurred())

			Expect(startMsg.Id).To(HavePrefix("4321-"))
			Expect(startMsg.Hosts).ToNot(BeEmpty())
			Expect(startMsg.MinimumRegisterIntervalInSeconds).To(Equal(int(cfg.StartResponseDelayInterval.Seconds())))
			Expect(startMsg.PruneThresholdInSeconds).To(Equal(int(cfg.DropletStaleThreshold.Seconds())))
		})
	})

	Context("when a greeting message is received", func() {
		BeforeEach(func() {
			process = ifrit.Invoke(sub)
			Eventually(process.Ready()).Should(BeClosed())
		})

		It("responds", func() {
			msgChan := make(chan *nats.Msg, 1)

			_, err := natsClient.ChanSubscribe("router.greet.test.response", msgChan)
			Expect(err).ToNot(HaveOccurred())

			err = natsClient.PublishRequest("router.greet", "router.greet.test.response", []byte{})
			Expect(err).ToNot(HaveOccurred())

			var msg *nats.Msg
			Eventually(msgChan).Should(Receive(&msg))
			Expect(msg).ToNot(BeNil())

			var message common.RouterStart
			err = json.Unmarshal(msg.Data, &message)
			Expect(err).ToNot(HaveOccurred())

			Expect(message.Id).To(HavePrefix("4321-"))
			Expect(message.Hosts).ToNot(BeEmpty())
			Expect(message.MinimumRegisterIntervalInSeconds).To(Equal(int(cfg.StartResponseDelayInterval.Seconds())))
			Expect(message.PruneThresholdInSeconds).To(Equal(int(cfg.DropletStaleThreshold.Seconds())))
		})
	})

	Context("when the message cannot be unmarshaled", func() {
		BeforeEach(func() {
			sub = mbus.NewSubscriber(natsClient, registry, cfg, reconnected, l)
			process = ifrit.Invoke(sub)
			Eventually(process.Ready()).Should(BeClosed())
		})
		It("does not update the registry", func() {
			err := natsClient.Publish("router.register", []byte(` `))
			Expect(err).ToNot(HaveOccurred())
			Consistently(registry.RegisterCallCount).Should(BeZero())
		})
	})

	Context("when TLS is enabled for backends", func() {
		BeforeEach(func() {
			cfg.Backends.EnableTLS = true
			sub = mbus.NewSubscriber(natsClient, registry, cfg, reconnected, l)
			process = ifrit.Invoke(sub)
			Eventually(process.Ready()).Should(BeClosed())
		})
		Context("when the message contains a tls port for route", func() {
			It("endpoint is constructed with tls port instead of http", func() {
				msg := mbus.RegistryMessage{
					Host:                 "host",
					App:                  "app",
					TLSPort:              1999,
					ServerCertDomainSAN:  "san",
					PrivateInstanceID:    "id",
					PrivateInstanceIndex: "index",
					Port:                 1111,
					StaleThresholdInSeconds: 120,
					Uris: []route.Uri{"test.example.com"},
					Tags: map[string]string{"key": "value"},
				}

				data, err := json.Marshal(msg)
				Expect(err).NotTo(HaveOccurred())

				err = natsClient.Publish("router.register", data)
				Expect(err).ToNot(HaveOccurred())

				Eventually(registry.RegisterCallCount).Should(Equal(1))
				_, originalEndpoint := registry.RegisterArgsForCall(0)
				expectedEndpoint := route.NewEndpoint("app", "host", 1999, "san", "id", "index", map[string]string{"key": "value"}, 120, "", models.ModificationTag{}, "", true)
				Expect(originalEndpoint).To(Equal(expectedEndpoint))
			})
		})
	})

	Context("when TLS is disabled for backends", func() {
		BeforeEach(func() {
			process = ifrit.Invoke(sub)
			Eventually(process.Ready()).Should(BeClosed())
		})
		Context("when the message contains a tls port and no port", func() {
			It("endpoint is not constructed and an error message is logged and unregister fails", func() {
				msg := mbus.RegistryMessage{
					Host:                    "host",
					App:                     "app",
					TLSPort:                 1999,
					ServerCertDomainSAN:     "san",
					PrivateInstanceID:       "id",
					PrivateInstanceIndex:    "index",
					StaleThresholdInSeconds: 120,
					Uris: []route.Uri{"test.example.com"},
					Tags: map[string]string{"key": "value"},
				}

				data, err := json.Marshal(msg)
				Expect(err).NotTo(HaveOccurred())

				err = natsClient.Publish("router.register", data)
				Expect(err).ToNot(HaveOccurred())

				Consistently(registry.RegisterCallCount).Should(BeZero())
				Expect(l).To(gbytes.Say("Unable to register route"))

				err = natsClient.Publish("router.unregister", data)
				Expect(err).ToNot(HaveOccurred())

				Consistently(registry.UnregisterCallCount).Should(BeZero())
				Expect(l).To(gbytes.Say("Unable to unregister route"))
			})
		})

		Context("when the message contains a regular port and a tls port", func() {
			It("endpoint is constructed with the regular port and useTls set to false and unregister succeeds with regular port", func() {
				msg := mbus.RegistryMessage{
					Host:                 "host",
					App:                  "app",
					TLSPort:              1999,
					ServerCertDomainSAN:  "san",
					PrivateInstanceID:    "id",
					PrivateInstanceIndex: "index",
					Port:                 1111,
					StaleThresholdInSeconds: 120,
					Uris: []route.Uri{"test.example.com"},
					Tags: map[string]string{"key": "value"},
				}

				data, err := json.Marshal(msg)
				Expect(err).NotTo(HaveOccurred())

				err = natsClient.Publish("router.register", data)
				Expect(err).ToNot(HaveOccurred())

				Eventually(registry.RegisterCallCount).Should(Equal(1))
				_, originalEndpoint := registry.RegisterArgsForCall(0)
				expectedEndpoint := route.NewEndpoint("app", "host", 1111, "san", "id", "index", map[string]string{"key": "value"}, 120, "", models.ModificationTag{}, "", false)
				Expect(originalEndpoint).To(Equal(expectedEndpoint))

				err = natsClient.Publish("router.unregister", data)
				Expect(err).ToNot(HaveOccurred())

				Eventually(registry.UnregisterCallCount).Should(Equal(1))
				_, originalEndpoint = registry.UnregisterArgsForCall(0)
				expectedEndpoint = route.NewEndpoint("app", "host", 1111, "san", "id", "index", map[string]string{"key": "value"}, 120, "", models.ModificationTag{}, "", false)
				Expect(originalEndpoint).To(Equal(expectedEndpoint))
			})
		})

		Context("when the message contains just a regular port", func() {
			It("endpoint is constructed with the regular port and useTls set to false, unregister succeeds", func() {
				msg := mbus.RegistryMessage{
					Host:                 "host",
					App:                  "app",
					ServerCertDomainSAN:  "san",
					PrivateInstanceID:    "id",
					PrivateInstanceIndex: "index",
					Port:                 1111,
					StaleThresholdInSeconds: 120,
					Uris: []route.Uri{"test.example.com"},
					Tags: map[string]string{"key": "value"},
				}

				data, err := json.Marshal(msg)
				Expect(err).NotTo(HaveOccurred())

				err = natsClient.Publish("router.register", data)
				Expect(err).ToNot(HaveOccurred())

				Eventually(registry.RegisterCallCount).Should(Equal(1))
				_, originalEndpoint := registry.RegisterArgsForCall(0)
				expectedEndpoint := route.NewEndpoint("app", "host", 1111, "san", "id", "index", map[string]string{"key": "value"}, 120, "", models.ModificationTag{}, "", false)
				Expect(originalEndpoint).To(Equal(expectedEndpoint))

				err = natsClient.Publish("router.unregister", data)
				Expect(err).ToNot(HaveOccurred())

				Eventually(registry.UnregisterCallCount).Should(Equal(1))
				_, originalEndpoint = registry.UnregisterArgsForCall(0)
				expectedEndpoint = route.NewEndpoint("app", "host", 1111, "san", "id", "index", map[string]string{"key": "value"}, 120, "", models.ModificationTag{}, "", false)
				Expect(originalEndpoint).To(Equal(expectedEndpoint))
			})
		})
	})

	Context("when the message contains an http url for route services", func() {
		BeforeEach(func() {
			sub = mbus.NewSubscriber(natsClient, registry, cfg, reconnected, l)
			process = ifrit.Invoke(sub)
			Eventually(process.Ready()).Should(BeClosed())
		})
		It("does not update the registry", func() {
			msg := mbus.RegistryMessage{
				Host:                 "host",
				App:                  "app",
				RouteServiceURL:      "url",
				ServerCertDomainSAN:  "san",
				PrivateInstanceID:    "id",
				PrivateInstanceIndex: "index",
				Port:                 1111,
				StaleThresholdInSeconds: 120,
				Uris: []route.Uri{"test.example.com", "test2.example.com"},
				Tags: map[string]string{"key": "value"},
			}

			data, err := json.Marshal(msg)
			Expect(err).NotTo(HaveOccurred())

			err = natsClient.Publish("router.register", data)
			Expect(err).ToNot(HaveOccurred())

			Consistently(registry.RegisterCallCount).Should(BeZero())
		})
	})

	Context("when a route is unregistered", func() {
		BeforeEach(func() {
			sub = mbus.NewSubscriber(natsClient, registry, cfg, reconnected, l)
			process = ifrit.Invoke(sub)
			Eventually(process.Ready()).Should(BeClosed())
		})

		It("does not race against registrations", func() {
			racingURI := route.Uri("test3.example.com")
			racingMsg := mbus.RegistryMessage{
				Host:                 "host",
				App:                  "app",
				RouteServiceURL:      "https://url.example.com",
				ServerCertDomainSAN:  "san",
				PrivateInstanceID:    "id",
				PrivateInstanceIndex: "index",
				Port:                 1111,
				StaleThresholdInSeconds: 120,
				Uris: []route.Uri{racingURI},
				Tags: map[string]string{"key": "value"},
			}

			racingData, err := json.Marshal(racingMsg)
			Expect(err).NotTo(HaveOccurred())

			msg := mbus.RegistryMessage{
				Host:                 "host",
				App:                  "app1",
				ServerCertDomainSAN:  "san",
				PrivateInstanceID:    "id",
				PrivateInstanceIndex: "index",
				Port:                 1112,
				StaleThresholdInSeconds: 120,
				Uris: []route.Uri{"test.example.com", "test2.example.com"},
				Tags: map[string]string{"key": "value"},
			}

			data, err := json.Marshal(msg)
			Expect(err).NotTo(HaveOccurred())

			var alreadyUnregistered uint32
			registry.RegisterStub = func(uri route.Uri, e *route.Endpoint) {
				defer GinkgoRecover()
				if uri == racingURI {
					Expect(atomic.LoadUint32(&alreadyUnregistered)).To(Equal(uint32(0)))
				}
			}
			registry.UnregisterStub = func(uri route.Uri, e *route.Endpoint) {
				if uri == racingURI {
					atomic.StoreUint32(&alreadyUnregistered, 1)
				}
			}

			for i := 0; i < 100; i++ {
				err = natsClient.Publish("router.register", data)
				Expect(err).ToNot(HaveOccurred())
			}

			err = natsClient.Publish("router.register", racingData)
			Expect(err).ToNot(HaveOccurred())
			err = natsClient.Publish("router.unregister", racingData)
			Expect(err).ToNot(HaveOccurred())

			Eventually(func() uint32 {
				return atomic.LoadUint32(&alreadyUnregistered)
			}).Should(Equal(uint32(1)))
		})

		It("unregisters the route", func() {
			msg := mbus.RegistryMessage{
				Host:                 "host",
				App:                  "app",
				RouteServiceURL:      "https://url.example.com",
				ServerCertDomainSAN:  "san",
				PrivateInstanceID:    "id",
				PrivateInstanceIndex: "index",
				Port:                 1111,
				StaleThresholdInSeconds: 120,
				Uris:             []route.Uri{"test.example.com", "test2.example.com"},
				Tags:             map[string]string{"key": "value"},
				IsolationSegment: "abc-iso-seg",
			}

			data, err := json.Marshal(msg)
			Expect(err).NotTo(HaveOccurred())

			err = natsClient.Publish("router.register", data)
			Expect(err).ToNot(HaveOccurred())

			Eventually(registry.RegisterCallCount).Should(Equal(2))

			Expect(registry.UnregisterCallCount()).To(Equal(0))
			err = natsClient.Publish("router.unregister", data)
			Expect(err).ToNot(HaveOccurred())

			Eventually(registry.UnregisterCallCount).Should(Equal(2))
			for i := 0; i < registry.UnregisterCallCount(); i++ {
				uri, endpoint := registry.UnregisterArgsForCall(i)

				Expect(msg.Uris).To(ContainElement(uri))
				Expect(endpoint.ApplicationId).To(Equal(msg.App))
				Expect(endpoint.Tags).To(Equal(msg.Tags))
				Expect(endpoint.PrivateInstanceId).To(Equal(msg.PrivateInstanceID))
				Expect(endpoint.PrivateInstanceIndex).To(Equal(msg.PrivateInstanceIndex))
				Expect(endpoint.RouteServiceUrl).To(Equal(msg.RouteServiceURL))
				Expect(endpoint.CanonicalAddr()).To(ContainSubstring(msg.Host))
				Expect(endpoint.IsolationSegment).To(Equal("abc-iso-seg"))
			}
		})
	})

})
