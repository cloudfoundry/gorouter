package route_fetcher

import (
	"context"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"code.cloudfoundry.org/clock"
	"code.cloudfoundry.org/gorouter/config"
	"code.cloudfoundry.org/gorouter/logger"
	"code.cloudfoundry.org/gorouter/registry"
	"code.cloudfoundry.org/gorouter/route"
	routing_api "code.cloudfoundry.org/routing-api"
	"code.cloudfoundry.org/routing-api/models"
	"code.cloudfoundry.org/routing-api/uaaclient"
	"github.com/cloudfoundry/dropsonde/metrics"
	"github.com/uber-go/zap"
	"golang.org/x/oauth2"
)

type RouteFetcher struct {
	UaaTokenFetcher           uaaclient.TokenFetcher
	RouteRegistry             registry.Registry
	FetchRoutesInterval       time.Duration
	SubscriptionRetryInterval time.Duration

	logger          logger.Logger
	endpoints       []models.Route
	endpointsMutex  sync.Mutex
	uaaToken        *oauth2.Token
	uaaTokenMutex   sync.Mutex
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

func NewRouteFetcher(
	logger logger.Logger,
	uaaTokenFetcher uaaclient.TokenFetcher,
	routeRegistry registry.Registry,
	cfg *config.Config,
	client routing_api.Client,
	subscriptionRetryInterval time.Duration,
	clock clock.Clock,
) *RouteFetcher {
	return &RouteFetcher{
		UaaTokenFetcher:           uaaTokenFetcher,
		RouteRegistry:             routeRegistry,
		FetchRoutesInterval:       cfg.PruneStaleDropletsInterval / 2,
		SubscriptionRetryInterval: subscriptionRetryInterval,

		client:       client,
		logger:       logger,
		eventChannel: make(chan routing_api.Event, 1024),
		clock:        clock,
	}
}

func (r *RouteFetcher) Run(signals <-chan os.Signal, ready chan<- struct{}) error {
	r.startEventCycle()

	ticker := r.clock.NewTicker(r.FetchRoutesInterval)
	r.logger.Debug("created-ticker", zap.Duration("interval", r.FetchRoutesInterval))
	r.logger.Info("syncer-started")

	close(ready)
	for {
		select {
		case <-ticker.C():
			err := r.FetchRoutes()
			if err != nil {
				r.logger.Error("failed-to-fetch-routes", zap.Error(err))
			}
		case e := <-r.eventChannel:
			r.HandleEvent(e)

		case <-signals:
			r.logger.Info("stopping")
			atomic.StoreInt32(&r.stopEventSource, 1)
			if es := r.eventSource.Load(); es != nil {
				err := es.(routing_api.EventSource).Close()
				if err != nil {
					r.logger.Error("failed-closing-routing-api-event-source", zap.Error(err))
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
			token, err := r.UaaTokenFetcher.FetchToken(context.Background(), forceUpdate)
			if err != nil {
				metrics.IncrementCounter(TokenFetchErrors)
				r.logger.Error("failed-to-fetch-token", zap.Error(err))
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
				time.Sleep(r.SubscriptionRetryInterval)
			}
		}
	}()
}

func (r *RouteFetcher) subscribeToEvents(token *oauth2.Token) error {
	r.client.SetToken(token.AccessToken)

	r.logger.Info("subscribing-to-routing-api-event-stream")
	source, err := r.client.SubscribeToEventsWithMaxRetries(maxRetries)
	if err != nil {
		metrics.IncrementCounter(SubscribeEventsErrors)
		r.logger.Error("failed-subscribing-to-routing-api-event-stream", zap.Error(err))
		return err
	}
	r.logger.Info("Successfully-subscribed-to-routing-api-event-stream")

	err = r.FetchRoutes()
	if err != nil {
		r.logger.Error("failed-to-refresh-routes", zap.Error(err))
	}

	r.eventSource.Store(source)
	var event routing_api.Event

	for {
		event, err = source.Next()
		if err != nil {
			metrics.IncrementCounter(SubscribeEventsErrors)
			r.logger.Error("failed-getting-next-event: ", zap.Error(err))

			closeErr := source.Close()
			if closeErr != nil {
				r.logger.Error("failed-closing-event-source", zap.Error(closeErr))
			}
			break
		}
		r.logger.Debug("received-event", zap.Object("event", event))
		r.eventChannel <- event
	}
	return err
}

func (r *RouteFetcher) HandleEvent(e routing_api.Event) {
	eventRoute := e.Route
	uri := route.Uri(eventRoute.Route)
	endpoint := route.NewEndpoint(&route.EndpointOpts{
		AppId:                   eventRoute.LogGuid,
		Host:                    eventRoute.IP,
		Port:                    uint16(eventRoute.Port),
		ServerCertDomainSAN:     eventRoute.LogGuid,
		StaleThresholdInSeconds: eventRoute.GetTTL(),
		RouteServiceUrl:         eventRoute.RouteServiceUrl,
		ModificationTag:         eventRoute.ModificationTag,
		UseTLS:                  false,
	})
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

	routes, err := r.fetchRoutesWithTokenRefresh()
	if err != nil {
		return err
	}

	r.logger.Debug("syncer-refreshing-endpoints", zap.Int("number-of-routes", len(routes)))
	r.refreshEndpoints(routes)
	return nil
}

func (r *RouteFetcher) fetchRoutesWithTokenRefresh() ([]models.Route, error) {
	forceUpdate := false
	var err error
	var routes []models.Route
	for count := 0; count < 2; count++ {
		r.logger.Debug("syncer-fetching-token")
		token, tokenErr := r.UaaTokenFetcher.FetchToken(context.Background(), forceUpdate)

		if tokenErr != nil {
			metrics.IncrementCounter(TokenFetchErrors)
			return []models.Route{}, tokenErr
		}
		r.client.SetToken(token.AccessToken)
		r.logger.Debug("syncer-fetching-routes")
		routes, err = r.client.Routes()
		if err != nil {
			if err.Error() == "unauthorized" {
				forceUpdate = true
			} else {
				return []models.Route{}, err
			}
		} else {
			break
		}
	}

	return routes, err
}

func (r *RouteFetcher) getEndpoints() []models.Route {
	r.endpointsMutex.Lock()
	defer r.endpointsMutex.Unlock()

	result := make([]models.Route, len(r.endpoints))
	copy(result, r.endpoints)

	return result
}

func (r *RouteFetcher) setEndpoints(endpoints []models.Route) {
	r.endpointsMutex.Lock()
	defer r.endpointsMutex.Unlock()
	r.endpoints = endpoints
}

func (r *RouteFetcher) refreshEndpoints(validRoutes []models.Route) {
	r.deleteEndpoints(validRoutes)

	r.setEndpoints(validRoutes)

	for _, aRoute := range validRoutes {
		r.RouteRegistry.Register(
			route.Uri(aRoute.Route),
			route.NewEndpoint(&route.EndpointOpts{
				AppId:                   aRoute.LogGuid,
				Host:                    aRoute.IP,
				Port:                    uint16(aRoute.Port),
				ServerCertDomainSAN:     aRoute.LogGuid,
				StaleThresholdInSeconds: aRoute.GetTTL(),
				RouteServiceUrl:         aRoute.RouteServiceUrl,
				ModificationTag:         aRoute.ModificationTag,
				UseTLS:                  false,
			}),
		)
	}
}

func (r *RouteFetcher) deleteEndpoints(validRoutes []models.Route) {
	var diff []models.Route

	for _, curRoute := range r.getEndpoints() {
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
			route.NewEndpoint(&route.EndpointOpts{
				AppId:                   aRoute.LogGuid,
				Host:                    aRoute.IP,
				Port:                    uint16(aRoute.Port),
				ServerCertDomainSAN:     aRoute.LogGuid,
				StaleThresholdInSeconds: aRoute.GetTTL(),
				RouteServiceUrl:         aRoute.RouteServiceUrl,
				ModificationTag:         aRoute.ModificationTag,
				UseTLS:                  false,
			}),
		)
	}
}

func routeEquals(current, desired models.Route) bool {
	if current.Route == desired.Route && current.IP == desired.IP && current.Port == desired.Port {
		return true
	}

	return false
}
