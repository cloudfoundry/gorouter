package registry

import (
	"encoding/json"
	"sync"
	"time"

	mbus "github.com/cloudfoundry/go_cfmessagebus"
	"github.com/cloudfoundry/gorouter/stats"
	"github.com/cloudfoundry/gorouter/util"
	steno "github.com/cloudfoundry/gosteno"

	"github.com/cloudfoundry/gorouter/config"
	"github.com/cloudfoundry/gorouter/log"
	"github.com/cloudfoundry/gorouter/route"
)

type Registry struct {
	sync.RWMutex

	*steno.Logger

	*stats.ActiveApps
	*stats.TopApps

	byUri  map[route.Uri]*route.Pool
	byAddr map[string]*route.Endpoint

	staleTracker *util.ListMap

	pruneStaleDropletsInterval time.Duration
	dropletStaleThreshold      time.Duration

	messageBus mbus.MessageBus

	timeOfLastUpdate time.Time
}

func NewRegistry(c *config.Config, mbus mbus.MessageBus) *Registry {
	r := &Registry{}

	r.Logger = steno.NewLogger("router.registry")

	r.ActiveApps = stats.NewActiveApps()
	r.TopApps = stats.NewTopApps()

	r.byUri = make(map[route.Uri]*route.Pool)
	r.byAddr = make(map[string]*route.Endpoint)

	r.staleTracker = util.NewListMap()

	r.pruneStaleDropletsInterval = c.PruneStaleDropletsInterval
	r.dropletStaleThreshold = c.DropletStaleThreshold

	r.messageBus = mbus

	return r
}

func (registry *Registry) Register(endpoint *route.Endpoint) {
	if len(endpoint.Uris) == 0 {
		return
	}

	registry.Lock()
	defer registry.Unlock()

	addr := endpoint.CanonicalAddr()

	endpointToRegister, found := registry.byAddr[addr]
	if found {
		for _, uri := range endpoint.Uris {
			endpointToRegister.Register(uri)
		}
	} else {
		registry.byAddr[addr] = endpoint
		endpointToRegister = endpoint
	}

	for _, uri := range endpointToRegister.Uris {
		pool, found := registry.byUri[uri.ToLower()]
		if !found {
			pool = route.NewPool()
			registry.byUri[uri.ToLower()] = pool
		}

		pool.Add(endpointToRegister)
	}

	endpointToRegister.UpdatedAtFORNOW = time.Now()

	registry.staleTracker.PushBack(endpointToRegister)
	registry.timeOfLastUpdate = time.Now()
}

func (registry *Registry) Unregister(endpoint *route.Endpoint) {
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

func (r *Registry) Lookup(host string) (*route.Endpoint, bool) {
	r.RLock()
	defer r.RUnlock()

	x, ok := r.byUri[route.Uri(host).ToLower()]
	if !ok {
		return nil, false
	}

	return x.Sample()
}

func (registry *Registry) StartPruningCycle() {
	go registry.checkAndPrune()
}

func (registry *Registry) IsStale(endpoint *route.Endpoint) bool {
	return endpoint.UpdatedAtFORNOW.Add(registry.dropletStaleThreshold).Before(time.Now())
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

func (r *Registry) LookupByPrivateInstanceId(host string, p string) (*route.Endpoint, bool) {
	r.RLock()
	defer r.RUnlock()

	x, ok := r.byUri[route.Uri(host).ToLower()]
	if !ok {
		return nil, false
	}

	return x.FindByPrivateInstanceId(p)
}

func (r *Registry) CaptureRoutingRequest(x *route.Endpoint, t time.Time) {
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

func (r *Registry) TimeOfLastUpdate() time.Time {
	return r.timeOfLastUpdate
}

func (r *Registry) NumEndpoints() int {
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
		endpoint := registry.staleTracker.Front().(*route.Endpoint)
		if !registry.IsStale(endpoint) {
			log.Infof("Droplet is not stale; NOT pruning: %v", endpoint.CanonicalAddr())
			break
		}

		log.Infof("Pruning stale droplet: %v", endpoint.CanonicalAddr())

		for _, uri := range endpoint.Uris {
			log.Infof("Pruning stale droplet: %v, uri: %s", endpoint.CanonicalAddr(), uri)
			registry.unregisterUri(endpoint, uri)
		}
	}
}

func (registry *Registry) pauseStaleTracker() {
	for routeElement := registry.staleTracker.FrontElement(); routeElement != nil; routeElement = routeElement.Next() {
		routeElement.Value.(*route.Endpoint).UpdatedAtFORNOW = time.Now()
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

func (registry *Registry) unregisterUri(endpoint *route.Endpoint, uri route.Uri) {
	uri = uri.ToLower()

	ok := endpoint.Unregister(uri)
	if ok {
		endpoints := registry.byUri[uri]

		endpoints.Remove(endpoint)

		if endpoints.IsEmpty() {
			delete(registry.byUri, uri)
		}
	}

	// Remove backend if it no longer has uris
	if len(endpoint.Uris) == 0 {
		delete(registry.byAddr, endpoint.CanonicalAddr())
		registry.staleTracker.Delete(endpoint)
	}
}
