package router

import (
	"encoding/json"
	mbus "github.com/cloudfoundry/go_cfmessagebus"
	"github.com/cloudfoundry/gorouter/stats"
	"github.com/cloudfoundry/gorouter/util"
	steno "github.com/cloudfoundry/gosteno"
	"sync"
	"time"
)

type Registry struct {
	sync.RWMutex

	*steno.Logger

	*stats.ActiveApps
	*stats.TopApps

	byUri  map[Uri]*EndpointPool
	byAddr map[string]*RouteEndpoint

	staleTracker *util.ListMap

	pruneStaleDropletsInterval time.Duration
	dropletStaleThreshold      time.Duration

	messageBus mbus.MessageBus

	timeOfLastUpdate time.Time
}

func NewRegistry(c *Config, messageBusClient mbus.MessageBus) *Registry {
	r := &Registry{
		messageBus: messageBusClient,
	}

	r.Logger = steno.NewLogger("router.registry")

	r.ActiveApps = stats.NewActiveApps()
	r.TopApps = stats.NewTopApps()

	r.byUri = make(map[Uri]*EndpointPool)
	r.byAddr = make(map[string]*RouteEndpoint)

	r.staleTracker = util.NewListMap()

	r.pruneStaleDropletsInterval = c.PruneStaleDropletsInterval
	r.dropletStaleThreshold = c.DropletStaleThreshold

	return r
}

func (registry *Registry) Register(endpoint *RouteEndpoint) {
	if len(endpoint.Uris) == 0 {
		return
	}

	registry.Lock()
	defer registry.Unlock()

	addr := endpoint.CanonicalAddr()

	routeEndpoint, found := registry.byAddr[addr]
	if !found {
		registry.byAddr[addr] = endpoint
		routeEndpoint = endpoint
	}

	for _, uri := range endpoint.Uris {
		pool, found := registry.byUri[uri.ToLower()]
		if !found {
			pool = NewEndpointPool()
			registry.byUri[uri.ToLower()] = pool
		}

		pool.Add(endpoint)
	}

	routeEndpoint.updated_at = time.Now()

	registry.staleTracker.PushBack(routeEndpoint)
	registry.timeOfLastUpdate = time.Now()
}

func (registry *Registry) Unregister(endpoint *RouteEndpoint) {
	registry.Lock()
	defer registry.Unlock()

	addr := endpoint.CanonicalAddr()

	registryForId, ok := registry.byAddr[addr]
	if !ok {
		return
	}

	for _, uri := range endpoint.Uris {
		registry.unregisterUri(registryForId, uri)
	}
}

func (r *Registry) Lookup(host string) (*RouteEndpoint, bool) {
	r.RLock()
	defer r.RUnlock()

	x, ok := r.byUri[Uri(host).ToLower()]
	if !ok {
		return nil, false
	}

	return x.Sample()
}

func (registry *Registry) StartPruningCycle() {
	go registry.checkAndPrune()
}

func (registry *Registry) IsStale(routeEndpoint *RouteEndpoint) bool {
	return routeEndpoint.updated_at.Add(registry.dropletStaleThreshold).Before(time.Now())
}

func (registry *Registry) PruneStaleDroplets() {
	if registry.isStateStale() {
		log.Info("State is stale; NOT pruning")
		registry.pauseStaleTracker()
		return
	}

	registry.Lock()
	defer registry.Unlock()

	registry.pruneStaleDroplets()
}

func (r *Registry) LookupByPrivateInstanceId(host string, p string) (*RouteEndpoint, bool) {
	r.RLock()
	defer r.RUnlock()

	x, ok := r.byUri[Uri(host).ToLower()]
	if !ok {
		return nil, false
	}

	return x.FindByPrivateInstanceId(p)
}

func (r *Registry) CaptureRoutingRequest(x *RouteEndpoint, t time.Time) {
	if x.ApplicationId != "" {
		r.ActiveApps.Mark(x.ApplicationId, t)
		r.TopApps.Mark(x.ApplicationId, t)
	}
}

func (registry *Registry) NumUris() int {
	registry.RLock()
	defer registry.RUnlock()

	return len(registry.byUri)
}

func (r *Registry) NumRouteEndpoints() int {
	r.RLock()
	defer r.RUnlock()

	return len(r.byAddr)
}

func (r *Registry) MarshalJSON() ([]byte, error) {
	r.RLock()
	defer r.RUnlock()

	return json.Marshal(r.byUri)
}

func (registry *Registry) isStateStale() bool {
	return !registry.messageBus.Ping()
}

func (registry *Registry) pruneStaleDroplets() {
	for registry.staleTracker.Len() > 0 {
		routeEndpoint := registry.staleTracker.Front().(*RouteEndpoint)
		if !registry.IsStale(routeEndpoint) {
			log.Infof("Droplet is not stale; NOT pruning: %v", routeEndpoint.CanonicalAddr())
			break
		}

		log.Infof("Pruning stale droplet: %v ", routeEndpoint.CanonicalAddr())

		for _, uri := range routeEndpoint.Uris {
			registry.unregisterUri(routeEndpoint, uri)
		}
	}
}

func (registry *Registry) pauseStaleTracker() {
	for routeElement := registry.staleTracker.FrontElement(); routeElement != nil; routeElement = routeElement.Next() {
		routeElement.Value.(*RouteEndpoint).updated_at = time.Now()
	}
}

func (r *Registry) checkAndPrune() {
	if r.pruneStaleDropletsInterval == 0 {
		return
	}

	tick := time.Tick(r.pruneStaleDropletsInterval)
	for {
		select {
		case <-tick:
			log.Debug("Start to check and prune stale droplets")
			r.PruneStaleDroplets()
		}
	}
}

func (registry *Registry) unregisterUri(routeEndpoint *RouteEndpoint, uri Uri) {
	uri = uri.ToLower()

	ok := routeEndpoint.unregister(uri)
	if ok {
		routeEndpoints := registry.byUri[uri]

		routeEndpoints.Remove(routeEndpoint)

		if routeEndpoints.IsEmpty() {
			delete(registry.byUri, uri)
		}
	}

	// Remove backend if it no longer has uris
	if len(routeEndpoint.Uris) == 0 {
		delete(registry.byAddr, routeEndpoint.CanonicalAddr())
		registry.staleTracker.Delete(routeEndpoint)
	}
}
