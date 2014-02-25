package registry

import (
	"encoding/json"
	"sync"
	"time"

	steno "github.com/cloudfoundry/gosteno"
	"github.com/cloudfoundry/yagnats"

	"github.com/cloudfoundry/gorouter/config"
	"github.com/cloudfoundry/gorouter/log"
	"github.com/cloudfoundry/gorouter/route"
)

type CFRegistry struct {
	sync.RWMutex

	logger *steno.Logger

	byUri map[route.Uri]*route.Pool

	table map[tableKey]*tableEntry

	pruneStaleDropletsInterval time.Duration
	dropletStaleThreshold      time.Duration

	messageBus yagnats.NATSClient

	timeOfLastUpdate time.Time
}

type tableKey struct {
	addr string
	uri  route.Uri
}

type tableEntry struct {
	endpoint  *route.Endpoint
	updatedAt time.Time
}

func NewCFRegistry(c *config.Config, mbus yagnats.NATSClient) *CFRegistry {
	r := &CFRegistry{}

	r.logger = steno.NewLogger("router.registry")

	r.byUri = make(map[route.Uri]*route.Pool)

	r.table = make(map[tableKey]*tableEntry)

	r.pruneStaleDropletsInterval = c.PruneStaleDropletsInterval
	r.dropletStaleThreshold = c.DropletStaleThreshold

	r.messageBus = mbus

	return r
}

func (registry *CFRegistry) Register(uri route.Uri, endpoint *route.Endpoint) {
	registry.Lock()
	defer registry.Unlock()

	uri = uri.ToLower()

	key := tableKey{
		addr: endpoint.CanonicalAddr(),
		uri:  uri,
	}

	var endpointToRegister *route.Endpoint

	entry, found := registry.table[key]
	if found {
		endpointToRegister = entry.endpoint
	} else {
		endpointToRegister = endpoint
		entry = &tableEntry{endpoint: endpoint}

		registry.table[key] = entry
	}

	pool, found := registry.byUri[uri]
	if !found {
		pool = route.NewPool()
		registry.byUri[uri] = pool
	}

	pool.Add(endpointToRegister)

	entry.updatedAt = time.Now()

	registry.timeOfLastUpdate = time.Now()
}

func (registry *CFRegistry) Unregister(uri route.Uri, endpoint *route.Endpoint) {
	registry.Lock()
	defer registry.Unlock()

	uri = uri.ToLower()

	key := tableKey{
		addr: endpoint.CanonicalAddr(),
		uri:  uri,
	}

	registry.unregisterUri(key)
}

func (r *CFRegistry) Lookup(uri route.Uri) (*route.Endpoint, bool) {
	r.RLock()
	defer r.RUnlock()

	pool, ok := r.lookupByUri(uri)
	if !ok {
		return nil, false
	}

	return pool.Sample()
}

func (r *CFRegistry) LookupByPrivateInstanceId(uri route.Uri, p string) (*route.Endpoint, bool) {
	r.RLock()
	defer r.RUnlock()

	pool, ok := r.lookupByUri(uri)
	if !ok {
		return nil, false
	}

	return pool.FindByPrivateInstanceId(p)
}

func (r *CFRegistry) lookupByUri(uri route.Uri) (*route.Pool, bool) {
	uri = uri.ToLower()
	pool, ok := r.byUri[uri]
	return pool, ok
}

func (registry *CFRegistry) StartPruningCycle() {
	go registry.checkAndPrune()
}

func (registry *CFRegistry) PruneStaleDroplets() {
	if registry.isStateStale() {
		log.Info("State is stale; NOT pruning")
		registry.pauseStaleTracker()
		return
	}

	registry.Lock()
	defer registry.Unlock()

	registry.pruneStaleDroplets()
}

func (registry *CFRegistry) NumUris() int {
	registry.RLock()
	defer registry.RUnlock()

	return len(registry.byUri)
}

func (r *CFRegistry) TimeOfLastUpdate() time.Time {
	r.RLock()
	defer r.RUnlock()
	return r.timeOfLastUpdate
}

func (r *CFRegistry) NumEndpoints() int {
	r.RLock()
	defer r.RUnlock()

	mapForSize := make(map[string]bool)
	for _, entry := range r.table {
		mapForSize[entry.endpoint.CanonicalAddr()] = true
	}

	return len(mapForSize)
}

func (r *CFRegistry) MarshalJSON() ([]byte, error) {
	r.RLock()
	defer r.RUnlock()

	return json.Marshal(r.byUri)
}

func (registry *CFRegistry) isStateStale() bool {
	return !registry.messageBus.Ping()
}

func (registry *CFRegistry) pruneStaleDroplets() {
	for key, entry := range registry.table {
		if !registry.isEntryStale(entry) {
			continue
		}

		log.Infof("Pruning stale droplet: %v, uri: %s", entry, key.uri)
		registry.unregisterUri(key)
	}
}

func (r *CFRegistry) isEntryStale(entry *tableEntry) bool {
	return entry.updatedAt.Add(r.dropletStaleThreshold).Before(time.Now())
}

func (registry *CFRegistry) pauseStaleTracker() {
	registry.Lock()
	defer registry.Unlock()
	for _, entry := range registry.table {
		entry.updatedAt = time.Now()
	}
}

func (r *CFRegistry) checkAndPrune() {
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

func (registry *CFRegistry) unregisterUri(key tableKey) {
	entry, found := registry.table[key]
	if !found {
		return
	}

	endpoints, found := registry.byUri[key.uri]
	if found {
		endpoints.Remove(entry.endpoint)

		if endpoints.IsEmpty() {
			delete(registry.byUri, key.uri)
		}
	}

	delete(registry.table, key)
}
