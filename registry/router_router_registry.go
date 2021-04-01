package registry

import (
	"code.cloudfoundry.org/gorouter/config"
	"code.cloudfoundry.org/gorouter/logger"
	"code.cloudfoundry.org/gorouter/route"
	"code.cloudfoundry.org/routing-api/models"
	"time"
)

type RouterRouterRegistry struct {
	pool *route.EndpointPool
}

func NewRouterRouterRegistry(logger logger.Logger, c *config.Config) *RouterRouterRegistry {
	backends := c.RouterRouterTargetRouters
	pool := route.NewPool(&route.PoolOpts{
		Logger:             logger,
		RetryAfterFailure:  c.DropletStaleThreshold / 4,
		MaxConnsPerBackend: c.Backends.MaxConns,
	})
	for _, backend := range backends {
		endpoint := route.NewEndpoint(&route.EndpointOpts{
			AppId:                   "",
			Host:                    backend.Host,
			Port:                    backend.Port,
			ServerCertDomainSAN:     "",
			PrivateInstanceId:       "",
			PrivateInstanceIndex:    "",
			Tags:                    nil,
			StaleThresholdInSeconds: 60 * 60 * 24 * (365*42 + 10), //Don't forget about extra leap year days
			RouteServiceUrl:         "",
			ModificationTag:         models.ModificationTag{},
			IsolationSegment:        "",
			UseTLS:                  false,
			UpdatedAt:               time.Now(),
		})
		pool.Put(endpoint)
	}

	return &RouterRouterRegistry{
		pool: pool,
	}
}

func (r *RouterRouterRegistry) Register(uri route.Uri, endpoint *route.Endpoint) {
	//probably better if we ignore these...
}

func (r *RouterRouterRegistry) Unregister(uri route.Uri, endpoint *route.Endpoint) {
	//nope
}

func (r *RouterRouterRegistry) Lookup(uri route.Uri) *route.EndpointPool {
	return r.pool
}

func (r *RouterRouterRegistry) LookupWithInstance(uri route.Uri, appID string, appIndex string) *route.EndpointPool {
	//🤷
	return r.pool
}

func (r *RouterRouterRegistry) NumEndpoints() int {
	return 1
}

func (r *RouterRouterRegistry) NumUris() int {
	return 1
}

func (r *RouterRouterRegistry) StartPruningCycle() {
}

func (r *RouterRouterRegistry) TimeOfLastUpdate() time.Time {
	return time.Now()
}

func (r *RouterRouterRegistry) MarshalJSON() ([]byte, error) {
	return []byte{}, nil
}
