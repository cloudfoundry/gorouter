package route_fetcher_test

import (
	"errors"
	"net/http"

	"github.com/cloudfoundry/gorouter/config"
	testRegistry "github.com/cloudfoundry/gorouter/registry/fakes"
	"github.com/cloudfoundry/gorouter/route"
	"github.com/cloudfoundry/gorouter/token_fetcher"
	testTokenFetcher "github.com/cloudfoundry/gorouter/token_fetcher/fakes"

	. "github.com/cloudfoundry/gorouter/route_fetcher"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/ghttp"
)

var _ = Describe("RouteFetcher", func() {
	var (
		cfg          *config.Config
		tokenFetcher *testTokenFetcher.FakeTokenFetcher
		registry     *testRegistry.FakeRegistryInterface
		server       *ghttp.Server
		fetcher      *RouteFetcher

		token *token_fetcher.Token

		responseBody []Route
	)

	BeforeEach(func() {
		server = ghttp.NewServer()
		cfg = config.DefaultConfig()
		cfg.RoutingApiUri = server.URL() + "/v1/routes"
		tokenFetcher = &testTokenFetcher.FakeTokenFetcher{}
		registry = &testRegistry.FakeRegistryInterface{}

		token = &token_fetcher.Token{
			AccessToken: "access_token",
			ExpireTime:  5,
		}

		fetcher = NewRouteFetcher(tokenFetcher, registry, cfg.RoutingApiUri)
	})

	Describe(".FetchRoutes", func() {
		BeforeEach(func() {
			tokenFetcher.FetchTokenReturns(token, nil)

			responseBody = []Route{
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
			server.AppendHandlers(
				ghttp.CombineHandlers(
					ghttp.VerifyRequest("GET", "/v1/routes"),
					ghttp.VerifyHeader(http.Header{
						"Authorization": []string{"bearer " + token.AccessToken},
					}),

					ghttp.RespondWithJSONEncoded(http.StatusOK, responseBody),
				))

			err := fetcher.FetchRoutes()
			Expect(err).ToNot(HaveOccurred())
			Expect(server.ReceivedRequests()).To(HaveLen(1))

			Expect(registry.RegisterCallCount()).To(Equal(3))

			for i := 0; i < 3; i++ {
				response := responseBody[i]
				uri, endpoint := registry.RegisterArgsForCall(i)
				Expect(uri).To(Equal(route.Uri(response.Route)))
				Expect(endpoint).To(Equal(route.NewEndpoint(response.LogGuid, response.IP, uint16(response.Port), response.LogGuid, nil, response.TTL)))
			}
		})

		It("Removes unregistered routes", func() {
			secondResponseBody := []Route{
				responseBody[0],
			}

			server.AppendHandlers(
				ghttp.CombineHandlers(
					ghttp.VerifyRequest("GET", "/v1/routes"),
					ghttp.VerifyHeader(http.Header{
						"Authorization": []string{"bearer " + token.AccessToken},
					}),

					ghttp.RespondWithJSONEncoded(http.StatusOK, responseBody),
				),
				ghttp.CombineHandlers(
					ghttp.RespondWithJSONEncoded(http.StatusOK, secondResponseBody),
				),
			)

			err := fetcher.FetchRoutes()
			Expect(err).ToNot(HaveOccurred())
			Expect(server.ReceivedRequests()).To(HaveLen(1))
			Expect(registry.RegisterCallCount()).To(Equal(3))

			err = fetcher.FetchRoutes()
			Expect(err).ToNot(HaveOccurred())
			Expect(server.ReceivedRequests()).To(HaveLen(2))
			Expect(registry.RegisterCallCount()).To(Equal(4))
			Expect(registry.UnregisterCallCount()).To(Equal(2))

			for i := 0; i < 2; i++ {
				response := responseBody[i+1]
				uri, endpoint := registry.UnregisterArgsForCall(i)
				Expect(uri).To(Equal(route.Uri(response.Route)))
				Expect(endpoint).To(Equal(route.NewEndpoint(response.LogGuid, response.IP, uint16(response.Port), response.LogGuid, nil, response.TTL)))
			}
		})

		Context("when the routing api is unavailble", func() {
			It("returns an error", func() {
				fetcher := NewRouteFetcher(tokenFetcher, registry, "bogus")

				err := fetcher.FetchRoutes()
				Expect(err).To(HaveOccurred())
			})
		})

		Context("when the routing api does not return 200", func() {
			It("returns an error", func() {
				server.AppendHandlers(ghttp.RespondWith(http.StatusBadRequest, "you messed up"))

				err := fetcher.FetchRoutes()
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(Equal("status code: 400, body: you messed up"))
				Expect(server.ReceivedRequests()).Should(HaveLen(1))
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
})
