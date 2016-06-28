package route

import (
	"math/rand"
	"time"
)

var randomize = rand.New(rand.NewSource(time.Now().UnixNano()))

type LeastConnection struct {
	pool            *Pool
	initialEndpoint string
	lastEndpoint    *Endpoint
}

func NewLeastConnection(p *Pool, initial string) EndpointIterator {
	return &LeastConnection{
		pool:            p,
		initialEndpoint: initial,
	}
}

func (r *LeastConnection) Next() *Endpoint {
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

func (r *LeastConnection) PreRequest(e *Endpoint) {
	e.Stats.NumberConnections.Increment()
}

func (r *LeastConnection) PostRequest(e *Endpoint) {
	e.Stats.NumberConnections.Decrement()
}

func (r *LeastConnection) next() *Endpoint {
	r.pool.lock.Lock()
	defer r.pool.lock.Unlock()

	var selected *Endpoint

	// none
	total := len(r.pool.endpoints)
	if total == 0 {
		return nil
	}

	// single endpoint
	if total == 1 {
		return r.pool.endpoints[0].endpoint
	}

	// more than 1 endpoint
	// select the least connection endpoint OR
	// random one within the least connection endpoints
	randIndices := randomize.Perm(total)

	for i := 0; i < total; i++ {
		randIdx := randIndices[i]
		cur := r.pool.endpoints[randIdx].endpoint

		// our first is the least
		if i == 0 {
			selected = cur
			continue
		}

		if cur.Stats.NumberConnections.Count() < selected.Stats.NumberConnections.Count() {
			selected = cur
		}
	}
	return selected
}

func (r *LeastConnection) EndpointFailed() {
	if r.lastEndpoint != nil {
		r.pool.endpointFailed(r.lastEndpoint)
	}
}
