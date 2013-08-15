package router

import (
	"encoding/json"
	"fmt"
	mbus "github.com/cloudfoundry/go_cfmessagebus"
	"github.com/cloudfoundry/gorouter/stats"
	"github.com/cloudfoundry/gorouter/util"
	steno "github.com/cloudfoundry/gosteno"
	"math/rand"
	"sync"
	"time"
)

// This is a transient struct. It doesn't maintain state.
type registryMessage struct {
	Host string            `json:"host"`
	Port uint16            `json:"port"`
	Uris Uris              `json:"uris"`
	Tags map[string]string `json:"tags"`
	App  string            `json:"app"`

	PrivateInstanceId string `json:"private_instance_id"`
}

func (m registryMessage) RouteEndpointId() (b RouteEndpointId, ok bool) {
	if m.Host != "" && m.Port != 0 {
		b = RouteEndpointId(fmt.Sprintf("%s:%d", m.Host, m.Port))
		ok = true
	}

	return
}

type Registry struct {
	sync.RWMutex

	*steno.Logger

	*stats.ActiveApps
	*stats.TopApps

	byUri       map[Uri][]*RouteEndpoint
	byRouteEndpointId map[RouteEndpointId]*RouteEndpoint

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

	r.byUri = make(map[Uri][]*RouteEndpoint)
	r.byRouteEndpointId = make(map[RouteEndpointId]*RouteEndpoint)

	r.staleTracker = util.NewListMap()

	r.pruneStaleDropletsInterval = c.PruneStaleDropletsInterval
	r.dropletStaleThreshold = c.DropletStaleThreshold

	return r
}

func (registry *Registry) StartPruningCycle() {
	go registry.checkAndPrune()
}

func (registry *Registry) isStateStale() bool {
	return !registry.messageBus.Ping()
}

func (registry *Registry) NumUris() int {
	registry.RLock()
	defer registry.RUnlock()

	return len(registry.byUri)
}

func (r *Registry) NumRouteEndpoints() int {
	r.RLock()
	defer r.RUnlock()

	return len(r.byRouteEndpointId)
}

func (r *Registry) registerUri(b *RouteEndpoint, u Uri) {
	u = u.ToLower()

	ok := b.register(u)
	if ok {
		x := r.byUri[u]
		r.byUri[u] = append(x, b)
	}
}

func (registry *Registry) Register(message *registryMessage) {
	i, ok := message.RouteEndpointId()
	if !ok || len(message.Uris) == 0 {
		return
	}

	registry.Lock()
	defer registry.Unlock()

	routeEndpoint, ok := registry.byRouteEndpointId[i]
	if !ok {
		routeEndpoint = newRouteEndpoint(i, message, registry.Logger)
		registry.byRouteEndpointId[i] = routeEndpoint
	}

	for _, uri := range message.Uris {
		registry.registerUri(routeEndpoint, uri)
	}

	routeEndpoint.updated_at = time.Now()

	registry.staleTracker.PushBack(routeEndpoint)
	registry.timeOfLastUpdate = time.Now()
}

func (registry *Registry) unregisterUri(routeEndpoint *RouteEndpoint, uri Uri) {
	uri = uri.ToLower()

	ok := routeEndpoint.unregister(uri)
	if ok {
		routeEndpoints := registry.byUri[uri]
		for i, b := range routeEndpoints {
			if b == routeEndpoint {
				// Remove b from list of backends
				routeEndpoints[i] = routeEndpoints[len(routeEndpoints)-1]
				routeEndpoints = routeEndpoints[:len(routeEndpoints)-1]
				break
			}
		}

		if len(routeEndpoints) == 0 {
			delete(registry.byUri, uri)
		} else {
			registry.byUri[uri] = routeEndpoints
		}
	}

	// Remove backend if it no longer has uris
	if len(routeEndpoint.U) == 0 {
		delete(registry.byRouteEndpointId, routeEndpoint.RouteEndpointId)
		registry.staleTracker.Delete(routeEndpoint)
	}
}

func (registry *Registry) Unregister(message *registryMessage) {
	id, ok := message.RouteEndpointId()
	if !ok {
		return
	}

	registry.Lock()
	defer registry.Unlock()

	registryForId, ok := registry.byRouteEndpointId[id]
	if !ok {
		return
	}

	for _, uri := range message.Uris {
		registry.unregisterUri(registryForId, uri)
	}
}

func (registry *Registry) pruneStaleDroplets() {
	for registry.staleTracker.Len() > 0 {
		routeEndpoint := registry.staleTracker.Front().(*RouteEndpoint)
		if !registry.IsStale(routeEndpoint) {
			log.Infof("Droplet is not stale; NOT pruning: %v", routeEndpoint.RouteEndpointId)
			break
		}

		log.Infof("Pruning stale droplet: %v ", routeEndpoint.RouteEndpointId)

		for _, uri := range routeEndpoint.U {
			registry.unregisterUri(routeEndpoint, uri)
		}
	}
}

func (registry *Registry) IsStale(routeEndpoint *RouteEndpoint) bool {
	return routeEndpoint.updated_at.Add(registry.dropletStaleThreshold).Before(time.Now())
}

func (registry *Registry) pauseStaleTracker() {
	for routeElement := registry.staleTracker.FrontElement(); routeElement != nil; routeElement = routeElement.Next() {
		routeElement.Value.(*RouteEndpoint).updated_at = time.Now()
	}
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

func (r *Registry) Lookup(host string) (*RouteEndpoint, bool) {
	r.RLock()
	defer r.RUnlock()

	x, ok := r.byUri[Uri(host).ToLower()]
	if !ok {
		return nil, false
	}

	// Return random backend from slice of backends for the specified uri
	return x[rand.Intn(len(x))], true
}

func (r *Registry) LookupByPrivateInstanceId(host string, p string) (*RouteEndpoint, bool) {
	r.RLock()
	defer r.RUnlock()

	x, ok := r.byUri[Uri(host).ToLower()]
	if !ok {
		return nil, false
	}

	for _, b := range x {
		if b.PrivateInstanceId == p {
			return b, true
		}
	}

	return nil, false
}

func (r *Registry) CaptureRoutingRequest(x *RouteEndpoint, t time.Time) {
	if x.ApplicationId != "" {
		r.ActiveApps.Mark(x.ApplicationId, t)
		r.TopApps.Mark(x.ApplicationId, t)
	}
}

func (r *Registry) MarshalJSON() ([]byte, error) {
	r.RLock()
	defer r.RUnlock()

	return json.Marshal(r.byUri)
}
