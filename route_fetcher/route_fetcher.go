package route_fetcher

import (
	"os"
	"sync/atomic"
	"time"

	"code.cloudfoundry.org/clock"
	"code.cloudfoundry.org/gorouter/config"
	"code.cloudfoundry.org/gorouter/registry"
	"code.cloudfoundry.org/gorouter/route"
	"code.cloudfoundry.org/lager"
	"code.cloudfoundry.org/routing-api"
	"code.cloudfoundry.org/routing-api/models"
	uaa_client "code.cloudfoundry.org/uaa-go-client"
	"code.cloudfoundry.org/uaa-go-client/schema"
	"github.com/cloudfoundry/dropsonde/metrics"
)

type RouteFetcher struct {
	UaaClient                          uaa_client.Client
	RouteRegistry                      registry.RegistryInterface
	FetchRoutesInterval                time.Duration
	SubscriptionRetryIntervalInSeconds int

	logger          lager.Logger
	endpoints       []models.Route
	client          routing_api.Client
	stopEventSource int32
	eventSource     atomic.Value
	eventChannel    chan routing_api.Event

	clock clock.Clock
}

const (
	TokenFetchErrors      = "token_fetch_errors"
	SubscribeEventsErrors = "subscribe_events_errors"
	maxRetries            = 3
)

func NewRouteFetcher(logger lager.Logger, uaaClient uaa_client.Client, routeRegistry registry.RegistryInterface,
	cfg *config.Config, client routing_api.Client, subscriptionRetryInterval int, clock clock.Clock) *RouteFetcher {
	return &RouteFetcher{
		UaaClient:                          uaaClient,
		RouteRegistry:                      routeRegistry,
		FetchRoutesInterval:                cfg.PruneStaleDropletsInterval / 2,
		SubscriptionRetryIntervalInSeconds: subscriptionRetryInterval,

		client:       client,
		logger:       logger,
		eventChannel: make(chan routing_api.Event, 1024),
		clock:        clock,
	}
}

func (r *RouteFetcher) Run(signals <-chan os.Signal, ready chan<- struct{}) error {
	r.startEventCycle()

	ticker := r.clock.NewTicker(r.FetchRoutesInterval)
	r.logger.Debug("created-ticker", lager.Data{"interval": r.FetchRoutesInterval})
	r.logger.Info("syncer-started")

	close(ready)
	for {
		select {
		case <-ticker.C():
			err := r.FetchRoutes()
			if err != nil {
				r.logger.Error("Failed to fetch routes: ", err)
			}
		case e := <-r.eventChannel:
			r.HandleEvent(e)

		case <-signals:
			r.logger.Info("stopping")
			atomic.StoreInt32(&r.stopEventSource, 1)
			if es := r.eventSource.Load(); es != nil {
				err := es.(routing_api.EventSource).Close()
				if err != nil {
					r.logger.Error("Failed to close routing_api EventSource: ", err)
				}
			}
			ticker.Stop()
			return nil
		}
	}
}

func (r *RouteFetcher) startEventCycle() {
	go func() {
		forceUpdate := false
		for {
			r.logger.Debug("fetching-token")
			token, err := r.UaaClient.FetchToken(forceUpdate)
			if err != nil {
				metrics.IncrementCounter(TokenFetchErrors)
				r.logger.Error("failed-to-fetch-token", err)
			} else {
				r.logger.Debug("token-fetched-successfully")
				if atomic.LoadInt32(&r.stopEventSource) == 1 {
					return
				}
				err = r.subscribeToEvents(token)
				if err != nil && err.Error() == "unauthorized" {
					forceUpdate = true
				} else {
					forceUpdate = false
				}
				if atomic.LoadInt32(&r.stopEventSource) == 1 {
					return
				}
				time.Sleep(time.Duration(r.SubscriptionRetryIntervalInSeconds) * time.Second)
			}
		}
	}()
}

func (r *RouteFetcher) subscribeToEvents(token *schema.Token) error {
	r.client.SetToken(token.AccessToken)

	r.logger.Info("Subscribing-to-routing-api-event-stream")
	source, err := r.client.SubscribeToEventsWithMaxRetries(maxRetries)
	if err != nil {
		metrics.IncrementCounter(SubscribeEventsErrors)
		r.logger.Error("Failed-to-subscribe-to-routing-api-event-stream: ", err)
		return err
	}
	r.logger.Info("Successfully-subscribed-to-routing-api-event-stream")

	r.eventSource.Store(source)
	var event routing_api.Event

	for {
		event, err = source.Next()
		if err != nil {
			metrics.IncrementCounter(SubscribeEventsErrors)
			r.logger.Error("Failed to get next event: ", err)
			break
		}
		r.logger.Debug("received-event", lager.Data{"event": event})
		r.eventChannel <- event
	}
	return err
}

func (r *RouteFetcher) HandleEvent(e routing_api.Event) {
	eventRoute := e.Route
	uri := route.Uri(eventRoute.Route)
	endpoint := route.NewEndpoint(
		eventRoute.LogGuid,
		eventRoute.IP,
		uint16(eventRoute.Port),
		eventRoute.LogGuid,
		"",
		nil,
		eventRoute.GetTTL(),
		eventRoute.RouteServiceUrl,
		eventRoute.ModificationTag)
	switch e.Action {
	case "Delete":
		r.RouteRegistry.Unregister(uri, endpoint)
	case "Upsert":
		r.RouteRegistry.Register(uri, endpoint)
	}
}

func (r *RouteFetcher) FetchRoutes() error {
	r.logger.Debug("syncer-fetch-routes-started")

	defer r.logger.Debug("syncer-fetch-routes-completed")

	forceUpdate := false
	var err error
	var routes []models.Route
	for count := 0; count < 2; count++ {
		r.logger.Debug("syncer-fetching-token")
		token, tokenErr := r.UaaClient.FetchToken(forceUpdate)
		if tokenErr != nil {
			metrics.IncrementCounter(TokenFetchErrors)
			return tokenErr
		}
		r.client.SetToken(token.AccessToken)
		r.logger.Debug("syncer-fetching-routes")
		routes, err = r.client.Routes()
		if err != nil {
			if err.Error() == "unauthorized" {
				forceUpdate = true
			} else {
				return err
			}
		} else {
			break
		}
	}

	if err == nil {
		r.logger.Debug("syncer-refreshing-endpoints")
		r.refreshEndpoints(routes)
	}

	return err
}

func (r *RouteFetcher) refreshEndpoints(validRoutes []models.Route) {
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
				"",
				nil,
				aRoute.GetTTL(),
				aRoute.RouteServiceUrl,
				aRoute.ModificationTag,
			))
	}
}

func (r *RouteFetcher) deleteEndpoints(validRoutes []models.Route) {
	var diff []models.Route

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
				"",
				nil,
				aRoute.GetTTL(),
				aRoute.RouteServiceUrl,
				aRoute.ModificationTag,
			))
	}
}

func routeEquals(current, desired models.Route) bool {
	if current.Route == desired.Route && current.IP == desired.IP && current.Port == desired.Port {
		return true
	}

	return false
}
