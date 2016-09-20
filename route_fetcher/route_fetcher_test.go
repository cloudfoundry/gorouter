package route_fetcher_test

import (
	"errors"
	"os"
	"time"

	"code.cloudfoundry.org/clock/fakeclock"
	"code.cloudfoundry.org/lager"
	"code.cloudfoundry.org/lager/lagertest"

	"code.cloudfoundry.org/gorouter/config"
	testRegistry "code.cloudfoundry.org/gorouter/registry/fakes"
	"code.cloudfoundry.org/gorouter/route"
	. "code.cloudfoundry.org/gorouter/route_fetcher"
	"code.cloudfoundry.org/routing-api"
	fake_routing_api "code.cloudfoundry.org/routing-api/fake_routing_api"
	"code.cloudfoundry.org/routing-api/models"
	testUaaClient "code.cloudfoundry.org/uaa-go-client/fakes"
	"code.cloudfoundry.org/uaa-go-client/schema"
	metrics_fakes "github.com/cloudfoundry/dropsonde/metric_sender/fake"
	"github.com/cloudfoundry/dropsonde/metrics"
	"github.com/tedsuo/ifrit"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gbytes"
)

var sender *metrics_fakes.FakeMetricSender

func init() {
	sender = metrics_fakes.NewFakeMetricSender()
	metrics.Initialize(sender, nil)
}

var _ = Describe("RouteFetcher", func() {
	var (
		cfg       *config.Config
		uaaClient *testUaaClient.FakeClient
		registry  *testRegistry.FakeRegistryInterface
		fetcher   *RouteFetcher
		logger    lager.Logger
		client    *fake_routing_api.FakeClient

		token *schema.Token

		response     []models.Route
		process      ifrit.Process
		eventChannel chan routing_api.Event
		errorChannel chan error

		clock *fakeclock.FakeClock
	)

	BeforeEach(func() {
		logger = lagertest.NewTestLogger("test")
		cfg = config.DefaultConfig()
		cfg.PruneStaleDropletsInterval = 2 * time.Millisecond

		retryInterval := 0
		uaaClient = &testUaaClient.FakeClient{}
		registry = &testRegistry.FakeRegistryInterface{}

		token = &schema.Token{
			AccessToken: "access_token",
			ExpiresIn:   5,
		}
		client = &fake_routing_api.FakeClient{}

		eventChannel = make(chan routing_api.Event)
		errorChannel = make(chan error)
		eventSource := fake_routing_api.FakeEventSource{}
		client.SubscribeToEventsWithMaxRetriesReturns(&eventSource, nil)

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
		fetcher = NewRouteFetcher(logger, uaaClient, registry, cfg, client, retryInterval, clock)

	})

	AfterEach(func() {
		close(errorChannel)
		close(eventChannel)
	})

	Describe("FetchRoutes", func() {
		BeforeEach(func() {
			uaaClient.FetchTokenReturns(token, nil)

			response = []models.Route{
				models.NewRoute(
					"foo",
					1,
					"1.1.1.1",
					"guid",
					"rs",
					0,
				),
				models.NewRoute(
					"foo",
					2,
					"2.2.2.2",
					"guid",
					"route-service-url",
					0,
				),
				models.NewRoute(
					"bar",
					3,
					"3.3.3.3",
					"guid",
					"rs",
					0,
				),
			}

			*response[0].TTL = 1
			*response[1].TTL = 1
			*response[2].TTL = 1
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
						"",
						nil,
						*expectedRoute.TTL,
						expectedRoute.RouteServiceUrl,
						expectedRoute.ModificationTag,
					)))
			}
		})

		It("uses cache when fetching token from UAA", func() {
			client.RoutesReturns(response, nil)

			err := fetcher.FetchRoutes()
			Expect(err).ToNot(HaveOccurred())
			Expect(uaaClient.FetchTokenCallCount()).To(Equal(1))
			Expect(uaaClient.FetchTokenArgsForCall(0)).To(Equal(false))
		})

		Context("when a cached token is invalid", func() {
			BeforeEach(func() {
				count := 0
				client.RoutesStub = func() ([]models.Route, error) {
					if count == 0 {
						count++
						return nil, errors.New("unauthorized")
					} else {
						return response, nil
					}
				}
			})

			It("uses cache when fetching token from UAA", func() {
				client = &fake_routing_api.FakeClient{}
				err := fetcher.FetchRoutes()
				Expect(err).ToNot(HaveOccurred())
				Expect(uaaClient.FetchTokenCallCount()).To(Equal(2))
				Expect(uaaClient.FetchTokenArgsForCall(0)).To(Equal(false))
				Expect(uaaClient.FetchTokenArgsForCall(1)).To(Equal(true))
			})
		})

		It("removes unregistered routes", func() {
			secondResponse := []models.Route{
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

			expectedUnregisteredRoutes := []models.Route{
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
						"",
						nil,
						*expectedRoute.TTL,
						expectedRoute.RouteServiceUrl,
						expectedRoute.ModificationTag,
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
					Expect(uaaClient.FetchTokenCallCount()).To(Equal(1))
					Expect(uaaClient.FetchTokenArgsForCall(0)).To(BeFalse())
				})
			})

			Context("error is unauthorized error", func() {
				It("returns an error", func() {
					client.RoutesReturns(nil, errors.New("unauthorized"))

					err := fetcher.FetchRoutes()
					Expect(uaaClient.FetchTokenCallCount()).To(Equal(2))
					Expect(uaaClient.FetchTokenArgsForCall(0)).To(BeFalse())
					Expect(uaaClient.FetchTokenArgsForCall(1)).To(BeTrue())
					Expect(client.RoutesCallCount()).To(Equal(2))
					Expect(err).To(HaveOccurred())
					Expect(err.Error()).To(Equal("unauthorized"))
				})
			})
		})

		Context("When the token fetcher returns an error", func() {
			BeforeEach(func() {
				uaaClient.FetchTokenReturns(nil, errors.New("token fetcher error"))
			})

			It("returns an error", func() {
				currentTokenFetchErrors := sender.GetCounter(TokenFetchErrors)
				err := fetcher.FetchRoutes()
				Expect(err).To(HaveOccurred())
				Expect(uaaClient.FetchTokenCallCount()).To(Equal(1))
				Expect(registry.RegisterCallCount()).To(Equal(0))
				Eventually(func() uint64 {
					return sender.GetCounter(TokenFetchErrors)
				}).Should(BeNumerically(">", currentTokenFetchErrors))
			})
		})

	})

	Describe("Run", func() {
		BeforeEach(func() {
			uaaClient.FetchTokenReturns(token, nil)
			client.RoutesReturns(response, nil)
		})

		JustBeforeEach(func() {
			fetcher.FetchRoutesInterval = 10 * time.Millisecond
			process = ifrit.Invoke(fetcher)
		})

		AfterEach(func() {
			process.Signal(os.Interrupt)
			Eventually(process.Wait(), 5*time.Second).Should(Receive())
		})

		It("subscribes for events", func() {
			Eventually(client.SubscribeToEventsWithMaxRetriesCallCount).Should(Equal(1))
		})

		Context("on specified interval", func() {
			It("it fetches routes", func() {
				// to be consumed by the eventSource.NextStub to avoid starvation
				eventChannel <- routing_api.Event{}
				clock.Increment(cfg.PruneStaleDropletsInterval + 100*time.Millisecond)
				Eventually(client.RoutesCallCount, 2*time.Second, 50*time.Millisecond).Should(Equal(1))
				clock.Increment(cfg.PruneStaleDropletsInterval + 100*time.Millisecond)
				Eventually(client.RoutesCallCount, 2*time.Second, 50*time.Millisecond).Should(Equal(2))
			})

			It("uses cache when fetching token from uaa", func() {
				eventChannel <- routing_api.Event{}
				clock.Increment(cfg.PruneStaleDropletsInterval + 100*time.Millisecond)
				Eventually(client.RoutesCallCount, 2*time.Second, 50*time.Millisecond).Should(Equal(1))
				Expect(uaaClient.FetchTokenArgsForCall(0)).To(Equal(false))
			})
		})

		Context("when token fetcher returns error", func() {
			BeforeEach(func() {
				uaaClient.FetchTokenReturns(nil, errors.New("Unauthorized"))
			})

			It("logs the error", func() {
				currentTokenFetchErrors := sender.GetCounter(TokenFetchErrors)

				Eventually(logger).Should(gbytes.Say("Unauthorized"))

				Eventually(uaaClient.FetchTokenCallCount).Should(BeNumerically(">=", 2))
				Expect(client.SubscribeToEventsWithMaxRetriesCallCount()).Should(Equal(0))
				Expect(client.RoutesCallCount()).Should(Equal(0))

				Eventually(func() uint64 {
					return sender.GetCounter(TokenFetchErrors)
				}).Should(BeNumerically(">", currentTokenFetchErrors))
			})
		})

		Describe("Event cycle", func() {
			Context("and the event source successfully subscribes", func() {
				It("responds to events", func() {
					Eventually(client.SubscribeToEventsWithMaxRetriesCallCount).Should(Equal(1))
					route := models.NewRoute("z.a.k", 63, "42.42.42.42", "Tomato", "route-service-url", 1)
					eventChannel <- routing_api.Event{
						Action: "Delete",
						Route:  route,
					}
					Eventually(registry.UnregisterCallCount).Should(BeNumerically(">=", 1))
				})

				It("responds to errors, and retries subscribing", func() {
					currentSubscribeEventsErrors := sender.GetCounter(SubscribeEventsErrors)

					fetchTokenCallCount := uaaClient.FetchTokenCallCount()
					subscribeCallCount := client.SubscribeToEventsWithMaxRetriesCallCount()

					errorChannel <- errors.New("beep boop im a robot")

					Eventually(logger).Should(gbytes.Say("beep boop im a robot"))

					Eventually(uaaClient.FetchTokenCallCount).Should(BeNumerically(">", fetchTokenCallCount))
					Eventually(client.SubscribeToEventsWithMaxRetriesCallCount).Should(BeNumerically(">", subscribeCallCount))

					Eventually(func() uint64 {
						return sender.GetCounter(SubscribeEventsErrors)
					}).Should(BeNumerically(">", currentSubscribeEventsErrors))
				})
			})

			Context("and the event source fails to subscribe", func() {
				Context("with error other than unauthorized", func() {
					BeforeEach(func() {
						client.SubscribeToEventsWithMaxRetriesStub = func(uint16) (routing_api.EventSource, error) {
							err := errors.New("i failed to subscribe")
							return &fake_routing_api.FakeEventSource{}, err
						}
					})

					It("logs the error and tries again", func() {
						fetchTokenCallCount := uaaClient.FetchTokenCallCount()
						subscribeCallCount := client.SubscribeToEventsWithMaxRetriesCallCount()

						currentSubscribeEventsErrors := sender.GetCounter(SubscribeEventsErrors)

						Eventually(logger).Should(gbytes.Say("i failed to subscribe"))

						Eventually(uaaClient.FetchTokenCallCount).Should(BeNumerically(">", fetchTokenCallCount))
						Eventually(client.SubscribeToEventsWithMaxRetriesCallCount).Should(BeNumerically(">", subscribeCallCount))

						Eventually(func() uint64 {
							return sender.GetCounter(SubscribeEventsErrors)
						}).Should(BeNumerically(">", currentSubscribeEventsErrors))
					})
				})

				Context("with unauthorized error", func() {
					BeforeEach(func() {
						client.SubscribeToEventsWithMaxRetriesStub = func(uint16) (routing_api.EventSource, error) {
							err := errors.New("unauthorized")
							return &fake_routing_api.FakeEventSource{}, err
						}
					})

					It("logs the error and tries again by not using cached access token", func() {
						currentSubscribeEventsErrors := sender.GetCounter(SubscribeEventsErrors)
						Eventually(logger).Should(gbytes.Say("unauthorized"))
						Eventually(uaaClient.FetchTokenCallCount).Should(BeNumerically(">", 2))
						Expect(uaaClient.FetchTokenArgsForCall(0)).To(BeFalse())
						Expect(uaaClient.FetchTokenArgsForCall(1)).To(BeTrue())

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
				eventRoute := models.NewRoute(
					"z.a.k",
					63,
					"42.42.42.42",
					"Tomato",
					"route-service-url",
					1,
				)

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
						"",
						nil,
						*eventRoute.TTL,
						eventRoute.RouteServiceUrl,
						eventRoute.ModificationTag,
					)))
			})
		})

		Context("When the event is a DELETE", func() {
			It("unregisters the route from the registry", func() {
				eventRoute := models.NewRoute(
					"z.a.k",
					63,
					"42.42.42.42",
					"Tomato",
					"route-service-url",
					1,
				)

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
						"",
						nil,
						*eventRoute.TTL,
						eventRoute.RouteServiceUrl,
						eventRoute.ModificationTag,
					)))
			})
		})
	})
})
