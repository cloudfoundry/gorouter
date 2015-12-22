package route_fetcher

import (
	"time"

	"github.com/cloudfoundry-incubator/routing-api"
	"github.com/cloudfoundry-incubator/routing-api/db"
	token_fetcher "github.com/cloudfoundry-incubator/uaa-token-fetcher"
	"github.com/cloudfoundry/gorouter/config"
	"github.com/cloudfoundry/gorouter/registry"
	"github.com/cloudfoundry/gorouter/route"
	steno "github.com/cloudfoundry/gosteno"
)

type RouteFetcher struct {
	TokenFetcher                       token_fetcher.TokenFetcher
	RouteRegistry                      registry.RegistryInterface
	FetchRoutesInterval                time.Duration
	SubscriptionRetryIntervalInSeconds int

	logger    *steno.Logger
	endpoints []db.Route
	ticker    *time.Ticker
	client    routing_api.Client
}

func NewRouteFetcher(logger *steno.Logger, tokenFetcher token_fetcher.TokenFetcher, routeRegistry registry.RegistryInterface, cfg *config.Config, client routing_api.Client, subscriptionRetryInterval int) *RouteFetcher {
	return &RouteFetcher{
		TokenFetcher:                       tokenFetcher,
		RouteRegistry:                      routeRegistry,
		FetchRoutesInterval:                cfg.PruneStaleDropletsInterval / 2,
		SubscriptionRetryIntervalInSeconds: subscriptionRetryInterval,

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

func (r *RouteFetcher) StartEventCycle() {
	go func() {
		useCachedToken := true
		for {
			token, err := r.TokenFetcher.FetchToken(useCachedToken)
			if err != nil {
				r.logger.Error(err.Error())
			} else {
				err = r.subscribeToEvents(token)
				if err != nil && err.Error() == "unauthorized" {
					useCachedToken = false
				} else {
					useCachedToken = true
				}
			}
			time.Sleep(time.Duration(r.SubscriptionRetryIntervalInSeconds) * time.Second)
		}
	}()
}

func (r *RouteFetcher) subscribeToEvents(token *token_fetcher.Token) error {
	r.client.SetToken(token.AccessToken)
	source, err := r.client.SubscribeToEvents()
	if err != nil {
		r.logger.Error(err.Error())
		return err
	}

	r.logger.Info("Successfully subscribed to event stream.")

	defer source.Close()

	for {
		event, err := source.Next()
		if err != nil {
			r.logger.Error(err.Error())
			break
		}

		r.logger.Debugf("Handling event: %v", event)
		r.HandleEvent(event)
	}
	return err
}

func (r *RouteFetcher) HandleEvent(e routing_api.Event) {
	eventRoute := e.Route
	uri := route.Uri(eventRoute.Route)
	endpoint := route.NewEndpoint(eventRoute.LogGuid, eventRoute.IP, uint16(eventRoute.Port), eventRoute.LogGuid, nil, eventRoute.TTL, eventRoute.RouteServiceUrl)
	switch e.Action {
	case "Delete":
		r.RouteRegistry.Unregister(uri, endpoint)
	case "Upsert":
		r.RouteRegistry.Register(uri, endpoint)
	}
}

func (r *RouteFetcher) FetchRoutes() error {
	useCachedToken := true
	var err error
	var routes []db.Route
	for count := 0; count < 2; count++ {
		token, tokenErr := r.TokenFetcher.FetchToken(useCachedToken)
		if tokenErr != nil {
			return tokenErr
		}
		r.client.SetToken(token.AccessToken)
		routes, err = r.client.Routes()
		if err != nil {
			if err.Error() == "unauthorized" {
				useCachedToken = false
			} else {
				return err
			}
		} else {
			break
		}
	}

	if err == nil {
		r.refreshEndpoints(routes)
	}

	return err
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
				aRoute.RouteServiceUrl,
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
				aRoute.RouteServiceUrl,
			))
	}
}

func routeEquals(current, desired db.Route) bool {
	if current.Route == desired.Route && current.IP == desired.IP && current.Port == desired.Port {
		return true
	}

	return false
}
