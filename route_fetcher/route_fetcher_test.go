package route_fetcher_test

import (
	"errors"
	"os"
	"time"

	"github.com/pivotal-golang/clock/fakeclock"
	"github.com/pivotal-golang/lager"

	"github.com/cloudfoundry-incubator/routing-api"
	"github.com/cloudfoundry-incubator/routing-api/db"
	fake_routing_api "github.com/cloudfoundry-incubator/routing-api/fake_routing_api"
	token_fetcher "github.com/cloudfoundry-incubator/uaa-token-fetcher"
	testTokenFetcher "github.com/cloudfoundry-incubator/uaa-token-fetcher/fakes"
	metrics_fakes "github.com/cloudfoundry/dropsonde/metric_sender/fake"
	"github.com/cloudfoundry/dropsonde/metrics"
	"github.com/cloudfoundry/gorouter/config"
	testRegistry "github.com/cloudfoundry/gorouter/registry/fakes"
	"github.com/cloudfoundry/gorouter/route"
	. "github.com/cloudfoundry/gorouter/route_fetcher"
	"github.com/tedsuo/ifrit"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var sender *metrics_fakes.FakeMetricSender

func init() {
	sender = metrics_fakes.NewFakeMetricSender()
	metrics.Initialize(sender, nil)
}

var _ = Describe("RouteFetcher", func() {
	var (
		cfg          *config.Config
		tokenFetcher *testTokenFetcher.FakeTokenFetcher
		registry     *testRegistry.FakeRegistryInterface
		fetcher      *RouteFetcher
		logger       lager.Logger
		sink         *lager.ReconfigurableSink
		client       *fake_routing_api.FakeClient

		token *token_fetcher.Token

		response     []db.Route
		process      ifrit.Process
		eventChannel chan routing_api.Event
		errorChannel chan error

		clock *fakeclock.FakeClock
	)

	BeforeEach(func() {
		cfg = config.DefaultConfig()
		cfg.PruneStaleDropletsInterval = 2 * time.Second

		retryInterval := 0
		tokenFetcher = &testTokenFetcher.FakeTokenFetcher{}
		registry = &testRegistry.FakeRegistryInterface{}

		// loggerConfig := &gosteno.Config{
		// 	Sinks: []gosteno.Sink{
		// 		sink,
		// 	},
		// }
		// gosteno.Init(loggerConfig)
		cf_lager.AddFlags(flag.CommandLine)
		logger, sink = cf_lager.New("route_fetcher_test")

		token = &token_fetcher.Token{
			AccessToken: "access_token",
			ExpireTime:  5,
		}
		client = &fake_routing_api.FakeClient{}

		eventChannel = make(chan routing_api.Event)
		errorChannel = make(chan error)
		eventSource := fake_routing_api.FakeEventSource{}
		client.SubscribeToEventsReturns(&eventSource, nil)

		localEventChannel := eventChannel
		localErrorChannel := errorChannel

		eventSource.NextStub = func() (routing_api.Event, error) {
			select {
			case e := <-localErrorChannel:
				return routing_api.Event{}, e
			case event := <-localEventChannel:
				return event, nil
			}
		}

		clock = fakeclock.NewFakeClock(time.Now())
		fetcher = NewRouteFetcher(logger, tokenFetcher, registry, cfg, client, retryInterval, clock)

	})

	AfterEach(func() {
		close(errorChannel)
		close(eventChannel)
	})

	Describe("FetchRoutes", func() {
		BeforeEach(func() {
			tokenFetcher.FetchTokenReturns(token, nil)

			response = []db.Route{
				{
					Route:   "foo",
					Port:    1,
					IP:      "1.1.1.1",
					TTL:     1,
					LogGuid: "guid",
				},
				{
					Route:           "foo",
					Port:            2,
					IP:              "2.2.2.2",
					TTL:             1,
					LogGuid:         "guid",
					RouteServiceUrl: "route-service-url",
				},
				{
					Route:   "bar",
					Port:    3,
					IP:      "3.3.3.3",
					TTL:     1,
					LogGuid: "guid",
				},
			}

		})

		It("updates the route registry", func() {
			client.RoutesReturns(response, nil)

			err := fetcher.FetchRoutes()
			Expect(err).ToNot(HaveOccurred())

			Expect(registry.RegisterCallCount()).To(Equal(3))

			for i := 0; i < 3; i++ {
				expectedRoute := response[i]
				uri, endpoint := registry.RegisterArgsForCall(i)
				Expect(uri).To(Equal(route.Uri(expectedRoute.Route)))
				Expect(endpoint).To(Equal(
					route.NewEndpoint(expectedRoute.LogGuid,
						expectedRoute.IP, uint16(expectedRoute.Port),
						expectedRoute.LogGuid,
						nil,
						expectedRoute.TTL,
						expectedRoute.RouteServiceUrl,
					)))
			}
		})

		It("removes unregistered routes", func() {
			secondResponse := []db.Route{
				response[0],
			}

			client.RoutesReturns(response, nil)

			err := fetcher.FetchRoutes()
			Expect(err).ToNot(HaveOccurred())
			Expect(registry.RegisterCallCount()).To(Equal(3))

			client.RoutesReturns(secondResponse, nil)

			err = fetcher.FetchRoutes()
			Expect(err).ToNot(HaveOccurred())
			Expect(registry.RegisterCallCount()).To(Equal(4))
			Expect(registry.UnregisterCallCount()).To(Equal(2))

			expectedUnregisteredRoutes := []db.Route{
				response[1],
				response[2],
			}

			for i := 0; i < 2; i++ {
				expectedRoute := expectedUnregisteredRoutes[i]
				uri, endpoint := registry.UnregisterArgsForCall(i)
				Expect(uri).To(Equal(route.Uri(expectedRoute.Route)))
				Expect(endpoint).To(Equal(
					route.NewEndpoint(expectedRoute.LogGuid,
						expectedRoute.IP,
						uint16(expectedRoute.Port),
						expectedRoute.LogGuid,
						nil,
						expectedRoute.TTL,
						expectedRoute.RouteServiceUrl,
					)))
			}
		})

		Context("when the routing api returns an error", func() {
			Context("error is not unauthorized error", func() {
				It("returns an error", func() {
					client.RoutesReturns(nil, errors.New("Oops!"))

					err := fetcher.FetchRoutes()
					Expect(err).To(HaveOccurred())
					Expect(err.Error()).To(Equal("Oops!"))
					Expect(tokenFetcher.FetchTokenCallCount()).To(Equal(1))
					Expect(tokenFetcher.FetchTokenArgsForCall(0)).To(BeTrue())
				})
			})

			Context("error is unauthorized error", func() {
				It("returns an error", func() {
					client.RoutesReturns(nil, errors.New("unauthorized"))

					err := fetcher.FetchRoutes()
					Expect(tokenFetcher.FetchTokenCallCount()).To(Equal(2))
					Expect(tokenFetcher.FetchTokenArgsForCall(0)).To(BeTrue())
					Expect(tokenFetcher.FetchTokenArgsForCall(1)).To(BeFalse())
					Expect(client.RoutesCallCount()).To(Equal(2))
					Expect(err).To(HaveOccurred())
					Expect(err.Error()).To(Equal("unauthorized"))
				})
			})
		})

		Context("When the token fetcher returns an error", func() {
			BeforeEach(func() {
				tokenFetcher.FetchTokenReturns(nil, errors.New("token fetcher error"))
			})

			It("returns an error", func() {
				currentTokenFetchErrors := sender.GetCounter(TokenFetchErrors)
				err := fetcher.FetchRoutes()
				Expect(err).To(HaveOccurred())
				Expect(tokenFetcher.FetchTokenCallCount()).To(Equal(1))
				Expect(registry.RegisterCallCount()).To(Equal(0))
				Eventually(func() uint64 {
					return sender.GetCounter(TokenFetchErrors)
				}).Should(BeNumerically(">", currentTokenFetchErrors))
			})
		})
	})

	Describe("Run", func() {
		BeforeEach(func() {
			tokenFetcher.FetchTokenReturns(token, nil)
			client.RoutesReturns(response, nil)
		})

		JustBeforeEach(func() {
			process = ifrit.Invoke(fetcher)
		})

		AfterEach(func() {
			process.Signal(os.Interrupt)
			Eventually(process.Wait(), 5*time.Second).Should(Receive())
		})

		It("subscribes for events", func() {
			Eventually(client.SubscribeToEventsCallCount).Should(Equal(1))
		})

		Context("on specified interval", func() {
			It("it fetches routes", func() {
				clock.Increment(cfg.PruneStaleDropletsInterval + 100*time.Millisecond)
				Eventually(client.RoutesCallCount, "2s").Should(Equal(1))
				clock.Increment(cfg.PruneStaleDropletsInterval + 100*time.Millisecond)
				Eventually(client.RoutesCallCount, "2s").Should(Equal(2))
			})
		})

		Context("when token fetcher returns error", func() {
			BeforeEach(func() {
				tokenFetcher.FetchTokenReturns(nil, errors.New("Unauthorized"))
			})

			It("logs the error", func() {
				currentTokenFetchErrors := sender.GetCounter(TokenFetchErrors)

				Eventually(func() int {
					return len(sink.Records())
				}).Should(BeNumerically(">=", 1))

				Expect(sink.Records()).ToNot(BeNil())
				Expect(sink.Records()[0].Message).To(Equal("Unauthorized"))

				Eventually(tokenFetcher.FetchTokenCallCount).Should(BeNumerically(">=", 2))
				Expect(client.SubscribeToEventsCallCount()).Should(Equal(0))
				Expect(client.RoutesCallCount()).Should(Equal(0))

				Eventually(func() uint64 {
					return sender.GetCounter(TokenFetchErrors)
				}).Should(BeNumerically(">", currentTokenFetchErrors))
			})
		})

		Describe("Event cycle", func() {
			Context("and the event source successfully subscribes", func() {
				It("responds to events", func() {
					Eventually(client.SubscribeToEventsCallCount).Should(Equal(1))
					eventChannel <- routing_api.Event{
						Action: "Delete",
						Route: db.Route{
							Route:           "z.a.k",
							Port:            63,
							IP:              "42.42.42.42",
							TTL:             1,
							LogGuid:         "Tomato",
							RouteServiceUrl: "route-service-url",
						}}
					Eventually(registry.UnregisterCallCount).Should(BeNumerically(">=", 1))
				})

				It("responds to errors, and retries subscribing", func() {
					currentSubscribeEventsErrors := sender.GetCounter(SubscribeEventsErrors)

					fetchTokenCallCount := tokenFetcher.FetchTokenCallCount()
					subscribeCallCount := client.SubscribeToEventsCallCount()

					errorChannel <- errors.New("beep boop im a robot")

					Eventually(func() string {
						if len(sink.Records()) > 1 {
							return sink.Records()[1].Message
						} else {
							return ""
						}
					}).Should(Equal("beep boop im a robot"))

					Eventually(tokenFetcher.FetchTokenCallCount).Should(BeNumerically(">", fetchTokenCallCount))
					Eventually(client.SubscribeToEventsCallCount).Should(BeNumerically(">", subscribeCallCount))

					Eventually(func() uint64 {
						return sender.GetCounter(SubscribeEventsErrors)
					}).Should(BeNumerically(">", currentSubscribeEventsErrors))
				})
			})

			Context("and the event source fails to subscribe", func() {
				Context("with error other than unauthorized", func() {
					BeforeEach(func() {
						client.SubscribeToEventsStub = func() (routing_api.EventSource, error) {
							err := errors.New("i failed to subscribe")
							return &fake_routing_api.FakeEventSource{}, err
						}
					})

					It("logs the error and tries again", func() {
						fetchTokenCallCount := tokenFetcher.FetchTokenCallCount()
						subscribeCallCount := client.SubscribeToEventsCallCount()

						currentSubscribeEventsErrors := sender.GetCounter(SubscribeEventsErrors)

						Eventually(func() string {
							if len(sink.Records()) > 0 {
								return sink.Records()[0].Message
							} else {
								return ""
							}
						}).Should(Equal("i failed to subscribe"))

						Eventually(tokenFetcher.FetchTokenCallCount).Should(BeNumerically(">", fetchTokenCallCount))
						Eventually(client.SubscribeToEventsCallCount).Should(BeNumerically(">", subscribeCallCount))

						Eventually(func() uint64 {
							return sender.GetCounter(SubscribeEventsErrors)
						}).Should(BeNumerically(">", currentSubscribeEventsErrors))
					})
				})

				Context("with unauthorized error", func() {
					BeforeEach(func() {
						client.SubscribeToEventsStub = func() (routing_api.EventSource, error) {
							err := errors.New("unauthorized")
							return &fake_routing_api.FakeEventSource{}, err
						}
					})

					It("logs the error and tries again by not using cached access token", func() {
						currentSubscribeEventsErrors := sender.GetCounter(SubscribeEventsErrors)

						Eventually(func() string {
							if len(sink.Records()) > 0 {
								return sink.Records()[0].Message
							} else {
								return ""
							}
						}).Should(Equal("unauthorized"))
						Eventually(tokenFetcher.FetchTokenCallCount()).Should(BeNumerically(">", 2))
						Expect(tokenFetcher.FetchTokenArgsForCall(0)).To(BeTrue())
						Expect(tokenFetcher.FetchTokenArgsForCall(1)).To(BeFalse())

						Eventually(func() uint64 {
							return sender.GetCounter(SubscribeEventsErrors)
						}).Should(BeNumerically(">", currentSubscribeEventsErrors))
					})
				})
			})
		})
	})

	Describe("HandleEvent", func() {
		Context("When the event is an Upsert", func() {
			It("registers the route from the registry", func() {
				eventRoute := db.Route{
					Route:           "z.a.k",
					Port:            63,
					IP:              "42.42.42.42",
					TTL:             1,
					LogGuid:         "Tomato",
					RouteServiceUrl: "route-service-url",
				}

				event := routing_api.Event{
					Action: "Upsert",
					Route:  eventRoute,
				}

				fetcher.HandleEvent(event)
				Expect(registry.RegisterCallCount()).To(Equal(1))
				uri, endpoint := registry.RegisterArgsForCall(0)
				Expect(uri).To(Equal(route.Uri(eventRoute.Route)))
				Expect(endpoint).To(Equal(
					route.NewEndpoint(
						eventRoute.LogGuid,
						eventRoute.IP,
						uint16(eventRoute.Port),
						eventRoute.LogGuid,
						nil,
						eventRoute.TTL,
						eventRoute.RouteServiceUrl,
					)))
			})
		})

		Context("When the event is a DELETE", func() {
			It("unregisters the route from the registry", func() {
				eventRoute := db.Route{
					Route:           "z.a.k",
					Port:            63,
					IP:              "42.42.42.42",
					TTL:             1,
					LogGuid:         "Tomato",
					RouteServiceUrl: "route-service-url",
				}

				event := routing_api.Event{
					Action: "Delete",
					Route:  eventRoute,
				}

				fetcher.HandleEvent(event)
				Expect(registry.UnregisterCallCount()).To(Equal(1))
				uri, endpoint := registry.UnregisterArgsForCall(0)
				Expect(uri).To(Equal(route.Uri(eventRoute.Route)))
				Expect(endpoint).To(Equal(
					route.NewEndpoint(
						eventRoute.LogGuid,
						eventRoute.IP,
						uint16(eventRoute.Port),
						eventRoute.LogGuid,
						nil,
						eventRoute.TTL,
						eventRoute.RouteServiceUrl,
					)))
			})
		})
	})
})
