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

	last := len(r.pool.endpoints)
	if last == 0 {
		return nil
	}

	if r.pool.NextIdx == -1 {
		r.pool.NextIdx = r.pool.random.Intn(last)
	} else if r.pool.NextIdx >= last {
		r.pool.NextIdx = 0
	}

	startIdx := r.pool.NextIdx
	curIdx := startIdx

	var curIsLocal bool
	for {
		e := r.pool.endpoints[curIdx]
		curIsLocal = e.endpoint.AvailabilityZone == r.localAvailabilityZone

		curIdx++
		if curIdx == last {
			curIdx = 0
		}

		if e.isOverloaded() {

			// We've checked every endpoint in the pool
			if curIdx == startIdx {
				if localDesired {
					// Search the pool again without the localDesired constraint
					localDesired = false
					continue
				}

				// No endpoints are available
				return nil
			}

			// Move on to the next endpoint in the pool
			continue
		}

		if e.failedAt != nil {
			curTime := time.Now()
			if curTime.Sub(*e.failedAt) > r.pool.retryAfterFailure {
				// exipired failure window
				e.failedAt = nil
			}
		}

		if (localDesired && curIsLocal) || !localDesired {
			if e.failedAt == nil {
				r.pool.NextIdx = curIdx
				return e
			}
		}

		if curIdx == startIdx {

			// could not find a valid route in the same AZ
			// start again but consider all AZs
			if localDesired {
				localDesired = false
			}
			// all endpoints are marked failed so reset everything to available
			for _, e2 := range r.pool.endpoints {
				e2.failedAt = nil
			}
		}
	}
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
