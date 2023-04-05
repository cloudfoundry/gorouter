package route_fetcher_test

import (
	"errors"
	"os"
	"sync"
	"time"

	"code.cloudfoundry.org/clock/fakeclock"
	"code.cloudfoundry.org/gorouter/config"
	"code.cloudfoundry.org/gorouter/logger"
	testRegistry "code.cloudfoundry.org/gorouter/registry/fakes"
	"code.cloudfoundry.org/gorouter/route"
	. "code.cloudfoundry.org/gorouter/route_fetcher"
	"code.cloudfoundry.org/gorouter/test_util"
	routing_api "code.cloudfoundry.org/routing-api"
	fake_routing_api "code.cloudfoundry.org/routing-api/fake_routing_api"
	"code.cloudfoundry.org/routing-api/models"
	test_uaa_client "code.cloudfoundry.org/routing-api/uaaclient/fakes"
	metrics_fakes "github.com/cloudfoundry/dropsonde/metric_sender/fake"
	"github.com/cloudfoundry/dropsonde/metrics"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gbytes"
	"github.com/tedsuo/ifrit"
	"golang.org/x/oauth2"
)

var sender *metrics_fakes.FakeMetricSender

func init() {
	sender = metrics_fakes.NewFakeMetricSender()
	metrics.Initialize(sender, nil)
}

var _ = Describe("RouteFetcher", func() {
	var (
		cfg          *config.Config
		tokenFetcher *test_uaa_client.FakeTokenFetcher
		registry     *testRegistry.FakeRegistry
		fetcher      *RouteFetcher
		logger       logger.Logger
		client       *fake_routing_api.FakeClient
		eventSource  *fake_routing_api.FakeEventSource

		token *oauth2.Token

		response     []models.Route
		process      ifrit.Process
		eventChannel chan routing_api.Event
		errorChannel chan error

		clock *fakeclock.FakeClock
	)

	BeforeEach(func() {
		logger = test_util.NewTestZapLogger("test")
		var err error
		cfg, err = config.DefaultConfig()
		Expect(err).ToNot(HaveOccurred())
		cfg.PruneStaleDropletsInterval = 2 * time.Millisecond

		retryInterval := 0 * time.Second
		tokenFetcher = &test_uaa_client.FakeTokenFetcher{}
		registry = &testRegistry.FakeRegistry{}

		token = &oauth2.Token{
			AccessToken: "access_token",
			Expiry:      time.Now().Add(5 * time.Second),
		}
		client = &fake_routing_api.FakeClient{}

		eventChannel = make(chan routing_api.Event)
		errorChannel = make(chan error)
		eventSource = &fake_routing_api.FakeEventSource{}
		client.SubscribeToEventsWithMaxRetriesReturns(eventSource, nil)

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
					route.NewEndpoint(&route.EndpointOpts{
						AppId:                   expectedRoute.LogGuid,
						Host:                    expectedRoute.IP,
						Port:                    uint16(expectedRoute.Port),
						ServerCertDomainSAN:     expectedRoute.LogGuid,
						StaleThresholdInSeconds: *expectedRoute.TTL,
						RouteServiceUrl:         expectedRoute.RouteServiceUrl,
						ModificationTag:         expectedRoute.ModificationTag,
					})))
			}
		})

		It("can be called in parallel", func() {
			// test with -race to validate
			client.RoutesReturns(response, nil)

			var wg sync.WaitGroup
			for i := 0; i < 10; i++ {
				wg.Add(1)
				go func(wg *sync.WaitGroup) {
					defer wg.Done()
					err := fetcher.FetchRoutes()
					Expect(err).ToNot(HaveOccurred())
				}(&wg)
			}
			wg.Wait()
		})

		It("uses cache when fetching token from UAA", func() {
			client.RoutesReturns(response, nil)

			err := fetcher.FetchRoutes()
			Expect(err).ToNot(HaveOccurred())
			Expect(tokenFetcher.FetchTokenCallCount()).To(Equal(1))
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
				Expect(tokenFetcher.FetchTokenCallCount()).To(Equal(2))
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
					route.NewEndpoint(&route.EndpointOpts{
						AppId:                   expectedRoute.LogGuid,
						Host:                    expectedRoute.IP,
						Port:                    uint16(expectedRoute.Port),
						ServerCertDomainSAN:     expectedRoute.LogGuid,
						StaleThresholdInSeconds: *expectedRoute.TTL,
						RouteServiceUrl:         expectedRoute.RouteServiceUrl,
						ModificationTag:         expectedRoute.ModificationTag,
					})))
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
				})
			})

			Context("error is unauthorized error", func() {
				It("returns an error", func() {
					client.RoutesReturns(nil, errors.New("unauthorized"))

					err := fetcher.FetchRoutes()
					Expect(tokenFetcher.FetchTokenCallCount()).To(Equal(2))
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
			fetcher.FetchRoutesInterval = 10 * time.Millisecond
		})

		JustBeforeEach(func() {
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
			BeforeEach(func() {
				client.SubscribeToEventsWithMaxRetriesReturns(&fake_routing_api.FakeEventSource{}, errors.New("not used"))
			})

			It("fetches routes", func() {
				clock.Increment(cfg.PruneStaleDropletsInterval + 100*time.Millisecond)
				Eventually(client.RoutesCallCount, 2*time.Second, 50*time.Millisecond).Should(Equal(1))
				clock.Increment(cfg.PruneStaleDropletsInterval + 100*time.Millisecond)
				Eventually(client.RoutesCallCount, 2*time.Second, 50*time.Millisecond).Should(Equal(2))
			})

			It("uses cache when fetching token from uaa", func() {
				clock.Increment(cfg.PruneStaleDropletsInterval + 100*time.Millisecond)
				Eventually(client.RoutesCallCount, 2*time.Second, 50*time.Millisecond).Should(Equal(1))
			})
		})

		Context("when token fetcher returns error", func() {
			BeforeEach(func() {
				tokenFetcher.FetchTokenReturns(nil, errors.New("Unauthorized"))
			})

			It("logs the error", func() {
				currentTokenFetchErrors := sender.GetCounter(TokenFetchErrors)

				Eventually(logger).Should(gbytes.Say("Unauthorized"))

				Eventually(tokenFetcher.FetchTokenCallCount).Should(BeNumerically(">=", 2))
				Expect(client.SubscribeToEventsWithMaxRetriesCallCount()).Should(Equal(0))
				Expect(client.RoutesCallCount()).Should(Equal(0))

				Eventually(func() uint64 {
					return sender.GetCounter(TokenFetchErrors)
				}).Should(BeNumerically(">", currentTokenFetchErrors))
			})
		})

		Describe("Event cycle", func() {
			BeforeEach(func() {
				fetcher.FetchRoutesInterval = 5 * time.Minute // Ignore syncing cycle
			})

			Context("and the event source successfully subscribes", func() {
				It("responds to events", func() {
					Eventually(client.SubscribeToEventsWithMaxRetriesCallCount).Should(Equal(1))
					eventChannel <- routing_api.Event{
						Action: "Delete",
						Route:  models.NewRoute("z.a.k", 63, "42.42.42.42", "Tomato", "route-service-url", 1),
					}
					Eventually(registry.UnregisterCallCount).Should(BeNumerically(">=", 1))
				})

				It("refreshes all routes", func() {
					Eventually(client.RoutesCallCount).Should(Equal(1))
				})

				It("responds to errors and closes the old subscription", func() {
					errorChannel <- errors.New("beep boop im a robot")
					Eventually(eventSource.CloseCallCount).Should(Equal(1))
				})

				It("responds to errors, and retries subscribing", func() {
					currentSubscribeEventsErrors := sender.GetCounter(SubscribeEventsErrors)

					fetchTokenCallCount := tokenFetcher.FetchTokenCallCount()
					subscribeCallCount := client.SubscribeToEventsWithMaxRetriesCallCount()

					errorChannel <- errors.New("beep boop im a robot")
					errorChannel <- errors.New("beep boop im a robot")
					errorChannel <- errors.New("beep boop im a robot")

					Eventually(logger).Should(gbytes.Say("beep boop im a robot"))

					Eventually(tokenFetcher.FetchTokenCallCount).Should(BeNumerically(">", fetchTokenCallCount))
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
						fetchTokenCallCount := tokenFetcher.FetchTokenCallCount()
						subscribeCallCount := client.SubscribeToEventsWithMaxRetriesCallCount()

						currentSubscribeEventsErrors := sender.GetCounter(SubscribeEventsErrors)

						Eventually(logger).Should(gbytes.Say("i failed to subscribe"))

						Eventually(tokenFetcher.FetchTokenCallCount).Should(BeNumerically(">", fetchTokenCallCount))
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
						Eventually(tokenFetcher.FetchTokenCallCount).Should(BeNumerically(">", 2))

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
					route.NewEndpoint(&route.EndpointOpts{
						AppId:                   eventRoute.LogGuid,
						Host:                    eventRoute.IP,
						Port:                    uint16(eventRoute.Port),
						ServerCertDomainSAN:     eventRoute.LogGuid,
						StaleThresholdInSeconds: *eventRoute.TTL,
						RouteServiceUrl:         eventRoute.RouteServiceUrl,
						ModificationTag:         eventRoute.ModificationTag,
					})))

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
					route.NewEndpoint(&route.EndpointOpts{
						AppId:                   eventRoute.LogGuid,
						Host:                    eventRoute.IP,
						Port:                    uint16(eventRoute.Port),
						ServerCertDomainSAN:     eventRoute.LogGuid,
						StaleThresholdInSeconds: *eventRoute.TTL,
						RouteServiceUrl:         eventRoute.RouteServiceUrl,
						ModificationTag:         eventRoute.ModificationTag,
					})))

			})
		})
	})
})
