package route_fetcher_test

import (
	"errors"
	"time"

	"github.com/cloudfoundry-incubator/routing-api/db"
	"github.com/cloudfoundry-incubator/routing-api/fake_routing_api"
	"github.com/cloudfoundry/gorouter/config"
	testRegistry "github.com/cloudfoundry/gorouter/registry/fakes"
	"github.com/cloudfoundry/gorouter/route"
	"github.com/cloudfoundry/gorouter/token_fetcher"
	testTokenFetcher "github.com/cloudfoundry/gorouter/token_fetcher/fakes"
	"github.com/cloudfoundry/gosteno"

	. "github.com/cloudfoundry/gorouter/route_fetcher"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("RouteFetcher", func() {
	var (
		cfg          *config.Config
		tokenFetcher *testTokenFetcher.FakeTokenFetcher
		registry     *testRegistry.FakeRegistryInterface
		fetcher      *RouteFetcher
		logger       *gosteno.Logger
		sink         *gosteno.TestingSink
		client       *fake_routing_api.FakeClient

		token *token_fetcher.Token

		response []db.Route
	)

	BeforeEach(func() {
		cfg = config.DefaultConfig()

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
		fetcher = NewRouteFetcher(logger, tokenFetcher, registry, cfg, client)
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
					Route:   "foo",
					Port:    2,
					IP:      "2.2.2.2",
					TTL:     1,
					LogGuid: "guid",
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
				response := response[i]
				uri, endpoint := registry.RegisterArgsForCall(i)
				Expect(uri).To(Equal(route.Uri(response.Route)))
				Expect(endpoint).To(Equal(route.NewEndpoint(response.LogGuid, response.IP, uint16(response.Port), response.LogGuid, nil, response.TTL)))
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

			for i := 0; i < 2; i++ {
				response := response[i+1]
				uri, endpoint := registry.UnregisterArgsForCall(i)
				Expect(uri).To(Equal(route.Uri(response.Route)))
				Expect(endpoint).To(Equal(route.NewEndpoint(response.LogGuid, response.IP, uint16(response.Port), response.LogGuid, nil, response.TTL)))
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
			fetcher = NewRouteFetcher(logger, tokenFetcher, registry, cfg, client)

			tokenFetcher.FetchTokenReturns(token, nil)

			client.RoutesReturns(response, nil)
		})

		It("periodically fetches routes", func() {
			fetcher.StartFetchCycle()

			time.Sleep(cfg.PruneStaleDropletsInterval * 2)
			Expect(client.RoutesCallCount()).To(BeNumerically(">=", 2))
		})

		It("logs the error", func() {
			tokenFetcher.FetchTokenReturns(nil, errors.New("Unauthorized"))
			fetcher.StartFetchCycle()

			time.Sleep(cfg.PruneStaleDropletsInterval + 10*time.Millisecond)
			Expect(sink.Records()).ToNot(BeNil())
			Expect(sink.Records()[0].Message).To(Equal("Unauthorized"))
		})
	})
})
