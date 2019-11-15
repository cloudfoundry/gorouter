package route

import (
	"time"
)

type RoundRobin struct {
	pool *EndpointPool

	initialEndpoint string
	lastEndpoint    *Endpoint
}

func NewRoundRobin(p *EndpointPool, initial string) EndpointIterator {
	return &RoundRobin{
		pool:            p,
		initialEndpoint: initial,
	}
}

func (r *RoundRobin) Next() *Endpoint {
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

	e = r.next()
	if e != nil {
		e.RLock()
		defer e.RUnlock()
		r.lastEndpoint = e.endpoint
		return e.endpoint
	}

	r.lastEndpoint = nil
	return nil
}

func (r *RoundRobin) next() *endpointElem {
	r.pool.Lock()
	defer r.pool.Unlock()

	last := len(r.pool.endpoints)
	if last == 0 {
		return nil
	}

	if r.pool.nextIdx == -1 {
		r.pool.nextIdx = r.pool.random.Intn(last)
	} else if r.pool.nextIdx >= last {
		r.pool.nextIdx = 0
	}

	startIdx := r.pool.nextIdx
	curIdx := startIdx
	for {
		e := r.pool.endpoints[curIdx]

		curIdx++
		if curIdx == last {
			curIdx = 0
		}

		if e.isOverloaded() {
			if curIdx == startIdx {
				return nil
			}
			continue
		}

		if e.failedAt != nil {
			curTime := time.Now()
			if curTime.Sub(*e.failedAt) > r.pool.retryAfterFailure {
				// exipired failure window
				e.failedAt = nil
			}
		}

		if e.failedAt == nil {
			r.pool.nextIdx = curIdx
			return e
		}

		if curIdx == startIdx {
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
