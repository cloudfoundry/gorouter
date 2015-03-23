package route_fetcher

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
	"time"

	"github.com/cloudfoundry/gorouter/config"
	"github.com/cloudfoundry/gorouter/registry"
	"github.com/cloudfoundry/gorouter/route"
	"github.com/cloudfoundry/gorouter/token_fetcher"
	steno "github.com/cloudfoundry/gosteno"
)

type RouteFetcher struct {
	TokenFetcher        token_fetcher.TokenFetcher
	RouteRegistry       registry.RegistryInterface
	RoutingApi          config.RoutingApiConfig
	FetchRoutesInterval time.Duration

	logger    *steno.Logger
	client    *http.Client
	endpoints []Route
	ticker    *time.Ticker
}

type Route struct {
	Route   string `json:"route"`
	Port    int    `json:"port"`
	IP      string `json:"ip"`
	TTL     int    `json:"ttl"`
	LogGuid string `json:"log_guid"`
}

func NewRouteFetcher(logger *steno.Logger, tokenFetcher token_fetcher.TokenFetcher, routeRegistry registry.RegistryInterface, cfg *config.Config) *RouteFetcher {
	return &RouteFetcher{
		TokenFetcher:        tokenFetcher,
		RouteRegistry:       routeRegistry,
		RoutingApi:          cfg.RoutingApi,
		FetchRoutesInterval: cfg.PruneStaleDropletsInterval / 2,
		client:              &http.Client{},
		logger:              logger,
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

	routingApiUri := fmt.Sprintf("%s:%d/v1/routes", r.RoutingApi.Uri, r.RoutingApi.Port)
	request, err := http.NewRequest("GET", routingApiUri, nil)
	if err != nil {
		return err
	}
	request.Header.Add("Authorization", "bearer "+token.AccessToken)

	resp, err := r.client.Do(request)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	if resp.StatusCode != http.StatusOK {
		return errors.New(fmt.Sprintf("status code: %d, body: %s", resp.StatusCode, body))
	}

	var routes []Route
	err = json.Unmarshal(body, &routes)
	if err != nil {
		return err
	}

	r.refreshEndpoints(routes)

	return nil
}

func (r *RouteFetcher) refreshEndpoints(validRoutes []Route) {
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

func (r *RouteFetcher) deleteEndpoints(validRoutes []Route) {
	var diff []Route

	for _, curRoute := range r.endpoints {
		routeFound := false

		for _, validRoute := range validRoutes {
			if curRoute.equals(validRoute) {
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

func (current Route) equals(desired Route) bool {
	if current.Route == desired.Route && current.IP == desired.IP && current.Port == desired.Port {
		return true
	}

	return false
}
