package route

import (
	"time"
)

type RoundRobin struct {
	pool *EndpointPool

	initialEndpoint       string
	lastEndpoint          *Endpoint
	locallyOptimistic     bool
	localAvailabilityZone string
}

func NewRoundRobin(p *EndpointPool, initial string, locallyOptimistic bool, localAvailabilityZone string) EndpointIterator {
	return &RoundRobin{
		pool:                  p,
		initialEndpoint:       initial,
		locallyOptimistic:     locallyOptimistic,
		localAvailabilityZone: localAvailabilityZone,
	}
}

func (r *RoundRobin) Next(attempt int) *Endpoint {
	var e *endpointElem
	if r.initialEndpoint != "" {
		e = r.pool.findById(r.initialEndpoint)
		r.initialEndpoint = ""

		if e != nil && e.isOverloaded() {
			e = nil
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
	r.pool.Lock()
	defer r.pool.Unlock()

	localDesired := r.locallyOptimistic && attempt == 0

	poolSize := len(r.pool.endpoints)
	if poolSize == 0 {
		return nil
	}

	if r.pool.NextIdx == -1 {
		r.pool.NextIdx = r.pool.random.Intn(poolSize)
	} else if r.pool.NextIdx >= poolSize {
		r.pool.NextIdx = 0
	}

	startingIndex := r.pool.NextIdx
	currentIndex := startingIndex
	var nextIndex int

	for {
		e := r.pool.endpoints[currentIndex]
		currentEndpointIsLocal := e.endpoint.AvailabilityZone == r.localAvailabilityZone

		// We tried using the actual modulo operator, but it has a 10x performance penalty
		nextIndex = currentIndex + 1
		if nextIndex == poolSize {
			nextIndex = 0
		}

		r.clearExpiredFailures(e)

		if !localDesired || (localDesired && currentEndpointIsLocal) {
			if e.failedAt == nil && !e.isOverloaded() {
				r.pool.NextIdx = nextIndex
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
