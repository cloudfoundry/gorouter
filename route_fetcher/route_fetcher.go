package route_fetcher

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"

	"github.com/cloudfoundry/gorouter/registry"
	"github.com/cloudfoundry/gorouter/route"
	"github.com/cloudfoundry/gorouter/token_fetcher"
)

type RouteFetcher struct {
	TokenFetcher  token_fetcher.TokenFetcher
	RouteRegistry registry.RegistryInterface
	Uri           string

	client    *http.Client
	endpoints []Route
}

type Route struct {
	Route   string `json:"route"`
	Port    int    `json:"port"`
	IP      string `json:"ip"`
	TTL     int    `json:"ttl"`
	LogGuid string `json:"log_guid"`
}

func NewRouteFetcher(tokenFetcher token_fetcher.TokenFetcher, routeRegistry registry.RegistryInterface, uri string) *RouteFetcher {
	return &RouteFetcher{
		TokenFetcher:  tokenFetcher,
		RouteRegistry: routeRegistry,
		Uri:           uri,
		client:        &http.Client{},
	}
}

func (r *RouteFetcher) FetchRoutes() error {
	token, err := r.TokenFetcher.FetchToken()
	if err != nil {
		return err
	}

	request, err := http.NewRequest("GET", r.Uri, nil)
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
		panic(err)
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
