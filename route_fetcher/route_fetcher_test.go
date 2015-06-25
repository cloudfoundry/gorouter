package route_fetcher_test

import (
	"errors"
	"time"

	"github.com/cloudfoundry-incubator/routing-api"
	"github.com/cloudfoundry-incubator/routing-api/db"
	fake_routing_api "github.com/cloudfoundry-incubator/routing-api/fake_routing_api"
	token_fetcher "github.com/cloudfoundry-incubator/uaa-token-fetcher"
	testTokenFetcher "github.com/cloudfoundry-incubator/uaa-token-fetcher/fakes"
	"github.com/cloudfoundry/gorouter/config"
	testRegistry "github.com/cloudfoundry/gorouter/registry/fakes"
	"github.com/cloudfoundry/gorouter/route"
	"github.com/cloudfoundry/gosteno"

	. "github.com/cloudfoundry/gorouter/route_fetcher"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("RouteFetcher", func() {
	var (
		cfg           *config.Config
		tokenFetcher  *testTokenFetcher.FakeTokenFetcher
		registry      *testRegistry.FakeRegistryInterface
		fetcher       *RouteFetcher
		logger        *gosteno.Logger
		sink          *gosteno.TestingSink
		client        *fake_routing_api.FakeClient
		retryInterval int

		token *token_fetcher.Token

		response []db.Route
	)

	BeforeEach(func() {
		cfg = config.DefaultConfig()

		retryInterval := 0
		tokenFetcher = &testTokenFetcher.FakeTokenFetcher{}
		registry = &testRegistry.FakeRegistryInterface{}
		sink = gosteno.NewTestingSink()

		loggerConfig := &gosteno.Config{
			Sinks: []gosteno.Sink{
				sink,
			},
		}
		gosteno.Init(loggerConfig)
		logger = gosteno.NewLogger("route_fetcher_test")

		token = &token_fetcher.Token{
			AccessToken: "access_token",
			ExpireTime:  5,
		}

		client = &fake_routing_api.FakeClient{}
		fetcher = NewRouteFetcher(logger, tokenFetcher, registry, cfg, client, retryInterval)
	})

	Describe(".FetchRoutes", func() {
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
			It("returns an error", func() {
				client.RoutesReturns(nil, errors.New("Oops!"))

				err := fetcher.FetchRoutes()
				Expect(err).To(HaveOccurred())
			})
		})

		Context("When the token fetcher returns an error", func() {
			BeforeEach(func() {
				tokenFetcher.FetchTokenReturns(nil, errors.New("token fetcher error"))
			})

			It("returns an error", func() {
				err := fetcher.FetchRoutes()
				Expect(err).To(HaveOccurred())
				Expect(registry.RegisterCallCount()).To(Equal(0))
			})
		})
	})

	Describe(".StartFetchCycle", func() {
		BeforeEach(func() {
			cfg.PruneStaleDropletsInterval = 10 * time.Millisecond
			fetcher = NewRouteFetcher(logger, tokenFetcher, registry, cfg, client, retryInterval)

			tokenFetcher.FetchTokenReturns(token, nil)

			client.RoutesReturns(response, nil)
		})

		It("periodically fetches routes", func() {
			received := make(chan struct{})

			client.RoutesStub = func() ([]db.Route, error) {
				received <- struct{}{}
				return []db.Route{}, nil
			}

			fetcher.StartFetchCycle()

			Eventually(received).Should(Receive())
			Eventually(received).Should(Receive())
		})

		It("logs the error", func() {
			tokenFetcher.FetchTokenReturns(nil, errors.New("Unauthorized"))
			fetcher.StartFetchCycle()

			Eventually(func() int {
				return len(sink.Records())
			}).Should(BeNumerically(">=", 1))

			Expect(sink.Records()).ToNot(BeNil())
			Expect(sink.Records()[0].Message).To(Equal("Unauthorized"))
		})
	})

	Describe(".StartEventCycle", func() {
		Context("when fetching the auth token fails", func() {
			It("logs the failure and tries again", func() {
				tokenFetcher.FetchTokenStub = func() (*token_fetcher.Token, error) {
					return nil, errors.New("failed to get the token")
				}
				fetcher.StartEventCycle()

				Eventually(func() string {
					if len(sink.Records()) > 0 {
						return sink.Records()[0].Message
					} else {
						return ""
					}
				}).Should(Equal("failed to get the token"))
				Eventually(tokenFetcher.FetchTokenCallCount).Should(BeNumerically(">=", 2))
			})
		})

		Context("and the event source successfully subscribes", func() {
			It("responds to events", func() {
				eventSource := fake_routing_api.FakeEventSource{}
				client.SubscribeToEventsReturns(&eventSource, nil)

				eventSource.NextStub = func() (routing_api.Event, error) {
					event := routing_api.Event{
						Action: "Delete",
						Route: db.Route{
							Route:           "z.a.k",
							Port:            63,
							IP:              "42.42.42.42",
							TTL:             1,
							LogGuid:         "Tomato",
							RouteServiceUrl: "route-service-url",
						}}
					return event, nil
				}

				tokenFetcher.FetchTokenReturns(token, nil)
				fetcher.StartEventCycle()

				Eventually(registry.UnregisterCallCount).Should(BeNumerically(">=", 1))
				Eventually(client.SubscribeToEventsCallCount).Should(Equal(1))
			})

			It("responds to errors, and retries subscribing", func() {
				eventSource := fake_routing_api.FakeEventSource{}
				client.SubscribeToEventsReturns(&eventSource, nil)

				eventSource.NextStub = func() (routing_api.Event, error) {
					return routing_api.Event{}, errors.New("beep boop im a robot")
				}

				tokenFetcher.FetchTokenReturns(token, nil)
				fetcher.StartEventCycle()

				Eventually(func() string {
					if len(sink.Records()) > 1 {
						return sink.Records()[1].Message
					} else {
						return ""
					}
				}).Should(Equal("beep boop im a robot"))
				Eventually(tokenFetcher.FetchTokenCallCount).Should(BeNumerically(">=", 2))
				Eventually(eventSource.CloseCallCount).Should(BeNumerically(">=", 2))
			})
		})

		Context("and the event source fails to subscribe", func() {
			It("logs the error and tries again", func() {
				client.SubscribeToEventsStub = func() (routing_api.EventSource, error) {
					err := errors.New("i failed to subscribe")
					return &fake_routing_api.FakeEventSource{}, err
				}

				tokenFetcher.FetchTokenReturns(token, nil)
				fetcher.StartEventCycle()

				Eventually(func() string {
					if len(sink.Records()) > 0 {
						return sink.Records()[0].Message
					} else {
						return ""
					}
				}).Should(Equal("i failed to subscribe"))
			})
		})
	})

	Describe(".HandleEvent", func() {
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
