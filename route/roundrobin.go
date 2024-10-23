package route

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

type RoundRobin struct {
	logger *slog.Logger
	pool   *EndpointPool
	lock   *sync.Mutex

	initialEndpoint       string
	mustBeSticky          bool
	lastEndpoint          *Endpoint
	locallyOptimistic     bool
	localAvailabilityZone string

	nextIdx int
}

func NewRoundRobin(logger *slog.Logger, p *EndpointPool, initial string, mustBeSticky bool, locallyOptimistic bool, localAvailabilityZone string) EndpointIterator {
	return &RoundRobin{
		logger:                logger,
		pool:                  p,
		lock:                  &sync.Mutex{},
		initialEndpoint:       initial,
		mustBeSticky:          mustBeSticky,
		locallyOptimistic:     locallyOptimistic,
		localAvailabilityZone: localAvailabilityZone,
		nextIdx:               -1,
	}
}

func (r *RoundRobin) Next(attempt int) *Endpoint {
	r.lock.Lock()
	defer r.lock.Unlock()

	var e *endpointElem
	if r.initialEndpoint != "" {
		e = r.pool.findById(r.initialEndpoint)
		if e != nil && e.isOverloaded() {
			if r.mustBeSticky {
				if r.logger.Enabled(context.Background(), slog.LevelDebug) {
					r.logger.Debug("endpoint-overloaded-but-request-must-be-sticky", e.endpoint.ToLogData()...)
				}
				return nil
			}
			e = nil
		}

		if e == nil && r.mustBeSticky {
			r.logger.Debug("endpoint-missing-but-request-must-be-sticky", slog.String("requested-endpoint", r.initialEndpoint))
			return nil
		}

		if !r.mustBeSticky {
			r.logger.Debug("endpoint-missing-choosing-alternate", slog.String("requested-endpoint", r.initialEndpoint))
			r.initialEndpoint = ""
		}
	}

	if e != nil {
		e.RLock()
		defer e.RUnlock()
		r.lastEndpoint = e.endpoint
		return e.endpoint
	}

	e = r.next(attempt)
	if e != nil {
		e.RLock()
		defer e.RUnlock()
		r.lastEndpoint = e.endpoint
		return e.endpoint
	}

	r.lastEndpoint = nil
	return nil
}

func (r *RoundRobin) next(attempt int) *endpointElem {
	// Note: the iterator lock must be held when calling this function.
	r.pool.Lock()
	defer r.pool.Unlock()

	localDesired := r.locallyOptimistic && attempt == 0

	poolSize := len(r.pool.endpoints)
	if poolSize == 0 {
		return nil
	}

	if r.nextIdx == -1 {
		r.nextIdx = r.pool.NextIndex()
	}
	// Check the next index of iterator in case the pool size decreased
	if r.nextIdx >= poolSize {
		r.nextIdx = 0
	}

	startingIndex := r.nextIdx
	currentIndex := startingIndex
	var nextIndex int

	for {
		e := r.pool.endpoints[currentIndex]
		currentEndpointIsLocal := e.endpoint.AvailabilityZone == r.localAvailabilityZone

		// We tried using the actual modulo operator, but it has a 10x performance penalty
		nextIndex = currentIndex + 1
		if nextIndex >= poolSize {
			nextIndex = 0
		}

		r.clearExpiredFailures(e)

		if !localDesired || (localDesired && currentEndpointIsLocal) {
			if e.failedAt == nil && !e.isOverloaded() {
				r.nextIdx = nextIndex
				return e
			}
		}

		// If we've cycled through all of the indices and we WILL be back where we started.
		if nextIndex == startingIndex {
			if r.allEndpointsAreOverloaded() {
				return nil
			}

			// could not find a valid route in the same AZ
			// start again but consider all AZs
			localDesired = false

			// all endpoints are marked failed so reset everything to available
			for _, e2 := range r.pool.endpoints {
				e2.failedAt = nil
			}

		}

		currentIndex = nextIndex
	}
}

func (r *RoundRobin) clearExpiredFailures(e *endpointElem) {
	if e.failedAt != nil {
		curTime := time.Now()
		if curTime.Sub(*e.failedAt) > r.pool.retryAfterFailure {
			e.failedAt = nil
		}
	}
}

func (r *RoundRobin) allEndpointsAreOverloaded() bool {
	allEndpointsAreOverloaded := true
	for _, e2 := range r.pool.endpoints {
		allEndpointsAreOverloaded = allEndpointsAreOverloaded && e2.isOverloaded()
	}
	return allEndpointsAreOverloaded
}

func (r *RoundRobin) EndpointFailed(err error) {
	if r.lastEndpoint != nil {
		r.pool.EndpointFailed(r.lastEndpoint, err)
	}
}

func (r *RoundRobin) PreRequest(e *Endpoint) {
	e.Stats.NumberConnections.Increment()
}

func (r *RoundRobin) PostRequest(e *Endpoint) {
	e.Stats.NumberConnections.Decrement()
}
