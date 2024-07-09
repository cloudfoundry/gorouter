package registry

import (
	"encoding/json"
	"strings"
	"sync"
	"time"

	"github.com/uber-go/zap"

	"code.cloudfoundry.org/gorouter/config"
	"code.cloudfoundry.org/gorouter/logger"
	"code.cloudfoundry.org/gorouter/metrics"
	"code.cloudfoundry.org/gorouter/registry/container"
	"code.cloudfoundry.org/gorouter/route"
)

//go:generate counterfeiter -o fakes/fake_registry.go . Registry
type Registry interface {
	Register(uri route.Uri, endpoint *route.Endpoint)
	Unregister(uri route.Uri, endpoint *route.Endpoint)
	Lookup(uri route.Uri) *route.EndpointPool
	LookupWithInstance(uri route.Uri, appID, appIndex string) *route.EndpointPool
}

type PruneStatus int

const (
	CONNECTED = PruneStatus(iota)
	DISCONNECTED
)

type RouteRegistry struct {
	sync.RWMutex

	logger logger.Logger

	// Access to the Trie datastructure should be governed by the RWMutex of RouteRegistry
	byURI *container.Trie

	// used for ability to suspend pruning
	suspendPruning func() bool
	pruningStatus  PruneStatus

	pruneStaleDropletsInterval time.Duration
	dropletStaleThreshold      time.Duration

	reporter metrics.RouteRegistryReporter

	ticker           *time.Ticker
	timeOfLastUpdate time.Time
	updateTimeLock   sync.RWMutex

	routingTableShardingMode string
	isolationSegments        []string

	maxConnsPerBackend int64

	EmptyPoolTimeout              time.Duration
	EmptyPoolResponseCode503      bool
	DefaultLoadBalancingAlgorithm string
}

func NewRouteRegistry(logger logger.Logger, c *config.Config, reporter metrics.RouteRegistryReporter) *RouteRegistry {
	r := &RouteRegistry{}
	r.logger = logger
	r.byURI = container.NewTrie()

	r.pruneStaleDropletsInterval = c.PruneStaleDropletsInterval
	r.dropletStaleThreshold = c.DropletStaleThreshold
	r.suspendPruning = func() bool { return false }

	r.reporter = reporter

	r.routingTableShardingMode = c.RoutingTableShardingMode
	r.isolationSegments = c.IsolationSegments

	r.maxConnsPerBackend = c.Backends.MaxConns
	r.EmptyPoolTimeout = c.EmptyPoolTimeout
	r.EmptyPoolResponseCode503 = c.EmptyPoolResponseCode503
	r.DefaultLoadBalancingAlgorithm = c.LoadBalance
	return r
}

func (r *RouteRegistry) Register(uri route.Uri, endpoint *route.Endpoint) {
	if !r.endpointInRouterShard(endpoint) {
		return
	}

	endpointAdded := r.register(uri, endpoint)

	r.reporter.CaptureRegistryMessage(endpoint)

	if endpointAdded == route.ADDED && !endpoint.UpdatedAt.IsZero() {
		r.reporter.CaptureRouteRegistrationLatency(time.Since(endpoint.UpdatedAt))
	}

	switch endpointAdded {
	case route.ADDED:
		r.logger.Info("endpoint-registered", zapData(uri, endpoint)...)
	case route.UPDATED:
		r.logger.Debug("endpoint-registered", zapData(uri, endpoint)...)
	default:
		r.logger.Debug("endpoint-not-registered", zapData(uri, endpoint)...)
	}
}

func (r *RouteRegistry) register(uri route.Uri, endpoint *route.Endpoint) route.PoolPutResult {
	r.RLock()
	defer r.RUnlock()

	t := time.Now()
	routekey := uri.RouteKey()
	pool := r.byURI.Find(routekey)

	if pool == nil {
		// release read lock, insertRouteKey() will acquire a write lock.
		r.RUnlock()
		pool = r.insertRouteKey(routekey, uri)
		r.RLock()
	}

	if endpoint.StaleThreshold > r.dropletStaleThreshold || endpoint.StaleThreshold == 0 {
		endpoint.StaleThreshold = r.dropletStaleThreshold
	}

	endpointAdded := pool.Put(endpoint)
	//Check endpoint for a load balancing algorithm. If it does exist & differs from that of the pool, then take the lb algorithm of the endpoint, otherwise keep the pools lb algorithm.
	if len(endpoint.LoadBalancingAlgorithm) > 0 && endpoint.LoadBalancingAlgorithm != pool.LBAlgorithm {
		if config.IsLoadBalancingAlgorithmValid(endpoint.LoadBalancingAlgorithm) {
			pool.Lock()
			pool.LBAlgorithm = endpoint.LoadBalancingAlgorithm
			pool.Unlock()

		} else {
			r.logger.Error("Invalid load balancing algorithm provided for a route, keeping the pool load balancing algorithm.",
				zap.String("endpointLBAlgorithm", endpoint.LoadBalancingAlgorithm),
				zap.String("poolLBAlgorithm", pool.LBAlgorithm))
		}
	}

	r.SetTimeOfLastUpdate(t)

	return endpointAdded
}

// insertRouteKey acquires a write lock, inserts the route key into the registry and releases the write lock.
func (r *RouteRegistry) insertRouteKey(routekey route.Uri, uri route.Uri) *route.EndpointPool {
	r.Lock()
	defer r.Unlock()

	// double check that the route key is still not found, now with the write lock.
	pool := r.byURI.Find(routekey)
	if pool == nil {
		host, contextPath := splitHostAndContextPath(uri)
		pool = route.NewPool(&route.PoolOpts{
			Logger:                 r.logger,
			RetryAfterFailure:      r.dropletStaleThreshold / 4,
			Host:                   host,
			ContextPath:            contextPath,
			MaxConnsPerBackend:     r.maxConnsPerBackend,
			LoadBalancingAlgorithm: r.DefaultLoadBalancingAlgorithm,
		})
		r.byURI.Insert(routekey, pool)
		r.logger.Info("route-registered", zap.Stringer("uri", routekey))
		// for backward compatibility:
		r.logger.Debug("uri-added", zap.Stringer("uri", routekey))
	}
	return pool
}

func (r *RouteRegistry) Unregister(uri route.Uri, endpoint *route.Endpoint) {
	if !r.endpointInRouterShard(endpoint) {
		return
	}

	r.unregister(uri, endpoint)

	r.reporter.CaptureUnregistryMessage(endpoint)
}

func (r *RouteRegistry) unregister(uri route.Uri, endpoint *route.Endpoint) {
	r.Lock()
	defer r.Unlock()

	uri = uri.RouteKey()

	pool := r.byURI.Find(uri)
	if pool != nil {
		endpointRemoved := pool.Remove(endpoint)
		if endpointRemoved {
			r.logger.Info("endpoint-unregistered", zapData(uri, endpoint)...)
		} else {
			r.logger.Info("endpoint-not-unregistered", zapData(uri, endpoint)...)
		}

		if pool.IsEmpty() {
			if r.EmptyPoolResponseCode503 && r.EmptyPoolTimeout > 0 {
				if time.Since(pool.LastUpdated()) > r.EmptyPoolTimeout {
					r.byURI.Delete(uri)
					r.logger.Info("route-unregistered", zap.Stringer("uri", uri))
				}
			} else {
				r.byURI.Delete(uri)
				r.logger.Info("route-unregistered", zap.Stringer("uri", uri))
			}
		}
	}
}

func (r *RouteRegistry) Lookup(uri route.Uri) *route.EndpointPool {
	started := time.Now()

	pool := r.lookup(uri)

	endLookup := time.Now()
	r.reporter.CaptureLookupTime(endLookup.Sub(started))

	return pool
}

func (r *RouteRegistry) lookup(uri route.Uri) *route.EndpointPool {
	r.RLock()
	defer r.RUnlock()

	uri = uri.RouteKey()
	var err error
	pool := r.byURI.MatchUri(uri)
	for pool == nil && err == nil {
		uri, err = uri.NextWildcard()
		pool = r.byURI.MatchUri(uri)
	}
	return pool
}

func (r *RouteRegistry) endpointInRouterShard(endpoint *route.Endpoint) bool {
	if r.routingTableShardingMode == config.SHARD_ALL {
		return true
	}

	if r.routingTableShardingMode == config.SHARD_SHARED_AND_SEGMENTS && endpoint.IsolationSegment == "" {
		return true
	}

	for _, v := range r.isolationSegments {
		if endpoint.IsolationSegment == v {
			return true
		}
	}

	return false
}

func (r *RouteRegistry) LookupWithInstance(uri route.Uri, appID string, appIndex string) *route.EndpointPool {
	uri = uri.RouteKey()
	p := r.Lookup(uri)

	if p == nil {
		return nil
	}

	var surgicalPool *route.EndpointPool

	p.Each(func(e *route.Endpoint) {
		if (e.ApplicationId == appID) && (e.PrivateInstanceIndex == appIndex) {
			surgicalPool = route.NewPool(&route.PoolOpts{
				Logger:             r.logger,
				RetryAfterFailure:  0,
				Host:               p.Host(),
				ContextPath:        p.ContextPath(),
				MaxConnsPerBackend: p.MaxConnsPerBackend(),
			})
			surgicalPool.Put(e)
		}
	})

	return surgicalPool
}

func (r *RouteRegistry) StartPruningCycle() {
	if r.pruneStaleDropletsInterval > 0 {
		r.Lock()
		defer r.Unlock()
		r.ticker = time.NewTicker(r.pruneStaleDropletsInterval)

		go func() {
			for {
				<-r.ticker.C
				r.logger.Debug("start-pruning-routes")
				r.pruneStaleDroplets()
				r.logger.Debug("finished-pruning-routes")
				r.reporter.CaptureRouteStats(r.NumUris(), r.MSSinceLastUpdate())
			}
		}()
	}
}

func (r *RouteRegistry) StopPruningCycle() {
	r.Lock()
	defer r.Unlock()
	if r.ticker != nil {
		r.ticker.Stop()
	}
}

func (registry *RouteRegistry) NumUris() int {
	registry.RLock()
	defer registry.RUnlock()

	return registry.byURI.PoolCount()
}

func (r *RouteRegistry) MSSinceLastUpdate() int64 {
	r.RLock()
	defer r.RUnlock()
	timeOfLastUpdate := r.TimeOfLastUpdate()
	if (timeOfLastUpdate == time.Time{}) {
		return -1
	}
	return int64(time.Since(timeOfLastUpdate) / time.Millisecond)
}

func (r *RouteRegistry) TimeOfLastUpdate() time.Time {
	r.updateTimeLock.RLock()
	defer r.updateTimeLock.RUnlock()

	return r.timeOfLastUpdate
}

func (r *RouteRegistry) SetTimeOfLastUpdate(t time.Time) {
	r.updateTimeLock.Lock()
	defer r.updateTimeLock.Unlock()
	r.timeOfLastUpdate = t
}

func (r *RouteRegistry) NumEndpoints() int {
	r.RLock()
	defer r.RUnlock()

	return r.byURI.EndpointCount()
}

func (r *RouteRegistry) MarshalJSON() ([]byte, error) {
	r.RLock()
	defer r.RUnlock()

	return json.Marshal(r.byURI.ToMap())
}

func (r *RouteRegistry) pruneStaleDroplets() {
	r.Lock()
	defer r.Unlock()

	// suspend pruning if option enabled and if NATS is unavailable
	if r.suspendPruning() {
		r.logger.Info("prune-suspended")
		r.pruningStatus = DISCONNECTED
		return
	}
	if r.pruningStatus == DISCONNECTED {
		// if we are coming back from being disconnected from source,
		// bulk update routes / mark updated to avoid pruning right away
		r.logger.Debug("prune-unsuspended-refresh-routes-start")
		r.freshenRoutes()
		r.logger.Debug("prune-unsuspended-refresh-routes-complete")
	}
	r.pruningStatus = CONNECTED

	r.byURI.EachNodeWithPool(func(t *container.Trie) {
		endpoints := t.Pool.PruneEndpoints()
		if r.EmptyPoolResponseCode503 && r.EmptyPoolTimeout > 0 {
			if time.Since(t.Pool.LastUpdated()) > r.EmptyPoolTimeout {
				t.Snip()
			}
		} else {
			t.Snip()
		}

		if len(endpoints) > 0 {
			addresses := []string{}
			for _, e := range endpoints {
				addresses = append(addresses, e.CanonicalAddr())
			}
			isolationSegment := endpoints[0].IsolationSegment
			if isolationSegment == "" {
				isolationSegment = "-"
			}
			r.logger.Info("pruned-route",
				zap.String("uri", t.ToPath()),
				zap.Object("endpoints", addresses),
				zap.Object("isolation_segment", isolationSegment),
			)
			r.reporter.CaptureRoutesPruned(uint64(len(endpoints)))
		}
	})
}

func (r *RouteRegistry) SuspendPruning(f func() bool) {
	r.Lock()
	defer r.Unlock()
	r.suspendPruning = f
}

// bulk update to mark pool / endpoints as updated
func (r *RouteRegistry) freshenRoutes() {
	now := time.Now()
	r.byURI.EachNodeWithPool(func(t *container.Trie) {
		t.Pool.MarkUpdated(now)
	})
}

func splitHostAndContextPath(uri route.Uri) (string, string) {
	contextPath := "/"
	trimmedUri := strings.TrimPrefix(uri.String(), "/")
	before, after, found := strings.Cut(trimmedUri, "/")

	if found {
		contextPath += after
	}

	if idx := strings.Index(contextPath, "?"); idx >= 0 {
		contextPath = contextPath[0:idx]
	}

	return before, contextPath
}

func zapData(uri route.Uri, endpoint *route.Endpoint) []zap.Field {
	isoSegField := zap.String("isolation_segment", "-")
	if endpoint.IsolationSegment != "" {
		isoSegField = zap.String("isolation_segment", endpoint.IsolationSegment)
	}
	return []zap.Field{
		zap.Stringer("uri", uri),
		zap.String("route_service_url", endpoint.RouteServiceUrl),
		zap.String("backend", endpoint.CanonicalAddr()),
		zap.String("application_id", endpoint.ApplicationId),
		zap.String("instance_id", endpoint.PrivateInstanceId),
		zap.String("server_cert_domain_san", endpoint.ServerCertDomainSAN),
		zap.String("protocol", endpoint.Protocol),
		zap.Object("modification_tag", endpoint.ModificationTag),
		isoSegField,
		zap.Bool("isTLS", endpoint.IsTLS()),
	}
}
