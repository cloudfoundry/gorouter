package route_fetcher

import (
	"time"

	"github.com/cloudfoundry-incubator/routing-api"
	"github.com/cloudfoundry-incubator/routing-api/db"
	"github.com/cloudfoundry/gorouter/config"
	"github.com/cloudfoundry/gorouter/registry"
	"github.com/cloudfoundry/gorouter/route"
	"github.com/cloudfoundry/gorouter/token_fetcher"
	steno "github.com/cloudfoundry/gosteno"
)

type RouteFetcher struct {
	TokenFetcher        token_fetcher.TokenFetcher
	RouteRegistry       registry.RegistryInterface
	FetchRoutesInterval time.Duration

	logger    *steno.Logger
	endpoints []db.Route
	ticker    *time.Ticker
	client    routing_api.Client
}

func NewRouteFetcher(logger *steno.Logger, tokenFetcher token_fetcher.TokenFetcher, routeRegistry registry.RegistryInterface, cfg *config.Config, client routing_api.Client) *RouteFetcher {
	return &RouteFetcher{
		TokenFetcher:        tokenFetcher,
		RouteRegistry:       routeRegistry,
		FetchRoutesInterval: cfg.PruneStaleDropletsInterval / 2,

		client: client,
		logger: logger,
	}
}

func (r *RouteFetcher) StartFetchCycle() {
	if r.FetchRoutesInterval > 0 {
		r.ticker = time.NewTicker(r.FetchRoutesInterval)

		go func() {
			for {
				select {
				case <-r.ticker.C:
					err := r.FetchRoutes()
					if err != nil {
						r.logger.Error(err.Error())
					}
				}
			}
		}()
	}
}

func (r *RouteFetcher) FetchRoutes() error {
	token, err := r.TokenFetcher.FetchToken()
	if err != nil {
		return err
	}
	r.client.SetToken(token.AccessToken)

	routes, err := r.client.Routes()
	if err != nil {
		return err
	}

	r.refreshEndpoints(routes)

	return nil
}

func (r *RouteFetcher) refreshEndpoints(validRoutes []db.Route) {
	r.deleteEndpoints(validRoutes)

	r.endpoints = validRoutes

	for _, aRoute := range r.endpoints {
		r.RouteRegistry.Register(
			route.Uri(aRoute.Route),
			route.NewEndpoint(
				aRoute.LogGuid,
				aRoute.IP,
				uint16(aRoute.Port),
				aRoute.LogGuid,
				nil,
				aRoute.TTL,
			))
	}
}

func (r *RouteFetcher) deleteEndpoints(validRoutes []db.Route) {
	var diff []db.Route

	for _, curRoute := range r.endpoints {
		routeFound := false

		for _, validRoute := range validRoutes {
			if routeEquals(curRoute, validRoute) {
				routeFound = true
				break
			}
		}

		if !routeFound {
			diff = append(diff, curRoute)
			r.endpoints = r.endpoints
		}
	}

	for _, aRoute := range diff {
		r.RouteRegistry.Unregister(
			route.Uri(aRoute.Route),
			route.NewEndpoint(
				aRoute.LogGuid,
				aRoute.IP,
				uint16(aRoute.Port),
				aRoute.LogGuid,
				nil,
				aRoute.TTL,
			))
	}
}

func routeEquals(current, desired db.Route) bool {
	if current.Route == desired.Route && current.IP == desired.IP && current.Port == desired.Port {
		return true
	}

	return false
}
