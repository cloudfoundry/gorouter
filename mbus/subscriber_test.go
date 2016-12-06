package mbus_test

import (
	"encoding/json"
	"os"
	"sync/atomic"

	"code.cloudfoundry.org/gorouter/common"
	"code.cloudfoundry.org/gorouter/mbus"
	"code.cloudfoundry.org/gorouter/registry/fakes"
	"code.cloudfoundry.org/gorouter/route"
	"code.cloudfoundry.org/gorouter/test_util"
	"code.cloudfoundry.org/lager"
	"code.cloudfoundry.org/lager/lagertest"

	"github.com/nats-io/nats"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/tedsuo/ifrit"
)

var _ = Describe("Subscriber", func() {
	var (
		sub     *mbus.Subscriber
		subOpts *mbus.SubscriberOpts
		process ifrit.Process

		registry *fakes.FakeRegistryInterface

		natsRunner   *test_util.NATSRunner
		natsPort     uint16
		natsClient   *nats.Conn
		startMsgChan chan struct{}

		logger lager.Logger
	)

	BeforeEach(func() {
		natsPort = test_util.NextAvailPort()

		natsRunner = test_util.NewNATSRunner(int(natsPort))
		natsRunner.Start()
		natsClient = natsRunner.MessageBus

		registry = new(fakes.FakeRegistryInterface)

		logger = lagertest.NewTestLogger("mbus-test")

		startMsgChan = make(chan struct{})

		subOpts = &mbus.SubscriberOpts{
			ID: "Fake-Subscriber-ID",
			MinimumRegisterIntervalInSeconds: 60,
			PruneThresholdInSeconds:          120,
		}

		sub = mbus.NewSubscriber(logger, natsClient, registry, startMsgChan, subOpts)
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

		Expect(startMsg.Id).To(Equal(subOpts.ID))
		Expect(startMsg.Hosts).ToNot(BeEmpty())
		Expect(startMsg.MinimumRegisterIntervalInSeconds).To(Equal(subOpts.MinimumRegisterIntervalInSeconds))
		Expect(startMsg.PruneThresholdInSeconds).To(Equal(subOpts.PruneThresholdInSeconds))
	})

	It("errors when publish start message fails", func() {
		sub = mbus.NewSubscriber(logger, nil, registry, startMsgChan, subOpts)
		process = ifrit.Invoke(sub)

		var err error
		Eventually(process.Wait()).Should(Receive(&err))
		Expect(err).To(HaveOccurred())
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
				startMsgChan <- struct{}{}
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

			Expect(startMsg.Id).To(Equal(subOpts.ID))
			Expect(startMsg.Hosts).ToNot(BeEmpty())
			Expect(startMsg.MinimumRegisterIntervalInSeconds).To(Equal(subOpts.MinimumRegisterIntervalInSeconds))
			Expect(startMsg.PruneThresholdInSeconds).To(Equal(subOpts.PruneThresholdInSeconds))
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

			Expect(message.Id).To(Equal(subOpts.ID))
			Expect(message.Hosts).ToNot(BeEmpty())
			Expect(message.MinimumRegisterIntervalInSeconds).To(Equal(subOpts.MinimumRegisterIntervalInSeconds))
			Expect(message.PruneThresholdInSeconds).To(Equal(subOpts.PruneThresholdInSeconds))
		})
	})

	Context("when a route is registered through NATS", func() {
		BeforeEach(func() {
			process = ifrit.Invoke(sub)
			Eventually(process.Ready()).Should(BeClosed())
		})

		It("updates the route registry", func() {
			msg := mbus.RegistryMessage{
				Host:                 "host",
				App:                  "app",
				RouteServiceURL:      "https://url.example.com",
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

			Eventually(registry.RegisterCallCount).Should(Equal(2))
			for i := 0; i < registry.RegisterCallCount(); i++ {
				uri, endpoint := registry.RegisterArgsForCall(i)

				Expect(msg.Uris).To(ContainElement(uri))
				Expect(endpoint.ApplicationId).To(Equal(msg.App))
				Expect(endpoint.Tags).To(Equal(msg.Tags))
				Expect(endpoint.PrivateInstanceId).To(Equal(msg.PrivateInstanceID))
				Expect(endpoint.PrivateInstanceIndex).To(Equal(msg.PrivateInstanceIndex))
				Expect(endpoint.RouteServiceUrl).To(Equal(msg.RouteServiceURL))
				Expect(endpoint.CanonicalAddr()).To(ContainSubstring(msg.Host))
			}
		})

		Context("when the message cannot be unmarshaled", func() {
			It("does not update the registry", func() {
				err := natsClient.Publish("router.register", []byte(` `))
				Expect(err).ToNot(HaveOccurred())
				Consistently(registry.RegisterCallCount).Should(BeZero())
			})
		})

		Context("when the message contains an http url for route services", func() {
			It("does not update the registry", func() {
				msg := mbus.RegistryMessage{
					Host:                 "host",
					App:                  "app",
					RouteServiceURL:      "url",
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
	})

	Context("when a route is unregistered through NATS", func() {
		BeforeEach(func() {
			process = ifrit.Invoke(sub)
			Eventually(process.Ready()).Should(BeClosed())
		})

		It("does not race against registrations", func() {
			racingURI := route.Uri("test3.example.com")
			racingMsg := mbus.RegistryMessage{
				Host:                 "host",
				App:                  "app",
				RouteServiceURL:      "https://url.example.com",
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
			}
		})
	})
})
