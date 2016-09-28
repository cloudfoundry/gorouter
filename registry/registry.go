package registry

import (
	"encoding/json"
	"strings"
	"sync"
	"time"

	"code.cloudfoundry.org/gorouter/config"
	"code.cloudfoundry.org/gorouter/metrics/reporter"
	"code.cloudfoundry.org/gorouter/registry/container"
	"code.cloudfoundry.org/gorouter/route"
	"code.cloudfoundry.org/lager"
)

//go:generate counterfeiter -o fakes/fake_registry_interface.go . RegistryInterface
type RegistryInterface interface {
	Register(uri route.Uri, endpoint *route.Endpoint)
	Unregister(uri route.Uri, endpoint *route.Endpoint)
	Lookup(uri route.Uri) *route.Pool
	LookupWithInstance(uri route.Uri, appId, appIndex string) *route.Pool
	StartPruningCycle()
	StopPruningCycle()
	NumUris() int
	NumEndpoints() int
	MarshalJSON() ([]byte, error)
}

type PruneStatus int

const (
	CONNECTED = PruneStatus(iota)
	DISCONNECTED
)

type RouteRegistry struct {
	sync.RWMutex

	logger lager.Logger

	// Access to the Trie datastructure should be governed by the RWMutex of RouteRegistry
	byUri *container.Trie

	// used for ability to suspend pruning
	suspendPruning func() bool
	pruningStatus  PruneStatus

	pruneStaleDropletsInterval time.Duration
	dropletStaleThreshold      time.Duration

	reporter reporter.RouteRegistryReporter

	ticker           *time.Ticker
	timeOfLastUpdate time.Time
}

func NewRouteRegistry(logger lager.Logger, c *config.Config, reporter reporter.RouteRegistryReporter) *RouteRegistry {
	r := &RouteRegistry{}
	r.logger = logger
	r.byUri = container.NewTrie()

	r.pruneStaleDropletsInterval = c.PruneStaleDropletsInterval
	r.dropletStaleThreshold = c.DropletStaleThreshold
	r.suspendPruning = func() bool { return false }

	r.reporter = reporter
	return r
}

func (r *RouteRegistry) Register(uri route.Uri, endpoint *route.Endpoint) {
	t := time.Now()
	data := lager.Data{"uri": uri, "backend": endpoint.CanonicalAddr(), "modification_tag": endpoint.ModificationTag}

	r.reporter.CaptureRegistryMessage(endpoint)

	r.Lock()

	routekey := uri.RouteKey()

	pool := r.byUri.Find(routekey)
	if pool == nil {
		contextPath := parseContextPath(uri)
		pool = route.NewPool(r.dropletStaleThreshold/4, contextPath)
		r.byUri.Insert(routekey, pool)
		r.logger.Debug("uri-added", lager.Data{
			"uri": uri, "routekey": routekey})
	}

	endpointAdded := pool.Put(endpoint)

	r.timeOfLastUpdate = t
	r.Unlock()

	if endpointAdded {
		r.logger.Debug("endpoint-registered", data)
	} else {
		r.logger.Debug("endpoint-not-registered", data)
	}
}

func (r *RouteRegistry) Unregister(uri route.Uri, endpoint *route.Endpoint) {
	data := lager.Data{"uri": uri, "backend": endpoint.CanonicalAddr(), "modification_tag": endpoint.ModificationTag}
	r.reporter.CaptureRegistryMessage(endpoint)

	r.Lock()

	uri = uri.RouteKey()

	pool := r.byUri.Find(uri)
	if pool != nil {
		endpointRemoved := pool.Remove(endpoint)
		if endpointRemoved {
			r.logger.Debug("endpoint-unregistered", data)
		} else {
			r.logger.Debug("endpoint-not-unregistered", data)
		}

		if pool.IsEmpty() {
			r.byUri.Delete(uri)
		}
	}

	r.Unlock()
}

func (r *RouteRegistry) Lookup(uri route.Uri) *route.Pool {
	started := time.Now()

	r.RLock()

	uri = uri.RouteKey()
	var err error
	pool := r.byUri.MatchUri(uri)
	for pool == nil && err == nil {
		uri, err = uri.NextWildcard()
		pool = r.byUri.MatchUri(uri)
	}

	r.RUnlock()
	endLookup := time.Now()
	r.reporter.CaptureLookupTime(endLookup.Sub(started))
	return pool
}

func (r *RouteRegistry) LookupWithInstance(uri route.Uri, appId string, appIndex string) *route.Pool {
	uri = uri.RouteKey()
	p := r.Lookup(uri)

	var surgicalPool *route.Pool

	p.Each(func(e *route.Endpoint) {
		if (e.ApplicationId == appId) && (e.PrivateInstanceIndex == appIndex) {
			surgicalPool = route.NewPool(0, "")
			surgicalPool.Put(e)
		}
	})
	return surgicalPool
}

func (r *RouteRegistry) StartPruningCycle() {
	if r.pruneStaleDropletsInterval > 0 {
		r.Lock()
		r.ticker = time.NewTicker(r.pruneStaleDropletsInterval)
		r.Unlock()

		go func() {
			for {
				select {
				case <-r.ticker.C:
					r.logger.Info("start-pruning-droplets")
					r.pruneStaleDroplets()
					r.logger.Info("finished-pruning-droplets")
					msSinceLastUpdate := uint64(time.Since(r.TimeOfLastUpdate()) / time.Millisecond)
					r.reporter.CaptureRouteStats(r.NumUris(), msSinceLastUpdate)
				}
			}
		}()
	}
}

func (r *RouteRegistry) StopPruningCycle() {
	r.Lock()
	if r.ticker != nil {
		r.ticker.Stop()
	}
	r.Unlock()
}

func (registry *RouteRegistry) NumUris() int {
	registry.RLock()
	uriCount := registry.byUri.PoolCount()
	registry.RUnlock()

	return uriCount
}

func (r *RouteRegistry) TimeOfLastUpdate() time.Time {
	r.RLock()
	t := r.timeOfLastUpdate
	r.RUnlock()

	return t
}

func (r *RouteRegistry) NumEndpoints() int {
	r.RLock()
	count := r.byUri.EndpointCount()
	r.RUnlock()

	return count
}

func (r *RouteRegistry) MarshalJSON() ([]byte, error) {
	r.RLock()
	defer r.RUnlock()

	return json.Marshal(r.byUri.ToMap())
}

func (r *RouteRegistry) pruneStaleDroplets() {
	r.Lock()
	defer r.Unlock()

	// suspend pruning if option enabled and if NATS is unavailable
	if r.suspendPruning() {
		r.logger.Info("prune-suspended")
		r.pruningStatus = DISCONNECTED
		return
	} else {
		if r.pruningStatus == DISCONNECTED {
			// if we are coming back from being disconnected from source,
			// bulk update routes / mark updated to avoid pruning right away
			r.logger.Debug("prune-unsuspended-refresh-routes-start")
			r.freshenRoutes()
			r.logger.Debug("prune-unsuspended-refresh-routes-complete")
		}
		r.pruningStatus = CONNECTED
	}

	r.byUri.EachNodeWithPool(func(t *container.Trie) {
		endpoints := t.Pool.PruneEndpoints(r.dropletStaleThreshold)
		t.Snip()
		if len(endpoints) > 0 {
			addresses := []string{}
			for _, e := range endpoints {
				addresses = append(addresses, e.CanonicalAddr())
			}
			r.logger.Info("pruned-route", lager.Data{"uri": t.ToPath(), "endpoints": addresses})
		}
	})
}

func (r *RouteRegistry) SuspendPruning(f func() bool) {
	r.Lock()
	r.suspendPruning = f
	r.Unlock()
}

// bulk update to mark pool / endpoints as updated
func (r *RouteRegistry) freshenRoutes() {
	now := time.Now()
	r.byUri.EachNodeWithPool(func(t *container.Trie) {
		t.Pool.MarkUpdated(now)
	})
}

func parseContextPath(uri route.Uri) string {
	contextPath := "/"
	split := strings.SplitN(strings.TrimPrefix(uri.String(), "/"), "/", 2)

	if len(split) > 1 {
		contextPath += split[1]
	}

	if idx := strings.Index(string(contextPath), "?"); idx >= 0 {
		contextPath = contextPath[0:idx]
	}

	return contextPath
}
