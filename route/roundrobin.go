package route

import (
	"time"
)

type RoundRobin struct {
	pool *Pool

	initialEndpoint string
	lastEndpoint    *Endpoint
}

func NewRoundRobin(p *Pool, initial string) EndpointIterator {
	return &RoundRobin{
		pool:            p,
		initialEndpoint: initial,
	}
}

func (r *RoundRobin) Next() *Endpoint {
	var e *Endpoint
	if r.initialEndpoint != "" {
		e = r.pool.findById(r.initialEndpoint)
		r.initialEndpoint = ""
	}

	if e == nil {
		e = r.next()
	}

	r.lastEndpoint = e

	return e
}

func (r *RoundRobin) next() *Endpoint {
	r.pool.lock.Lock()
	defer r.pool.lock.Unlock()

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

		if e.failedAt != nil {
			curTime := time.Now()
			if curTime.Sub(*e.failedAt) > r.pool.retryAfterFailure {
				// exipired failure window
				e.failedAt = nil
			}
		}

		if e.failedAt == nil {
			r.pool.nextIdx = curIdx
			return e.endpoint
		}

		if curIdx == startIdx {
			// all endpoints are marked failed so reset everything to available
			for _, e2 := range r.pool.endpoints {
				e2.failedAt = nil
			}
		}
	}
}

func (r *RoundRobin) EndpointFailed() {
	if r.lastEndpoint != nil {
		r.pool.endpointFailed(r.lastEndpoint)
	}
}

func (r *RoundRobin) PreRequest(e *Endpoint) {
	e.Stats.NumberConnections.Increment()
}

func (r *RoundRobin) PostRequest(e *Endpoint) {
	e.Stats.NumberConnections.Decrement()
}
