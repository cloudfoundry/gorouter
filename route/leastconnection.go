package route

import (
	"math/rand"
	"time"
)

var randomize = rand.New(rand.NewSource(time.Now().UnixNano()))

type LeastConnection struct {
	pool            *EndpointPool
	initialEndpoint string
	lastEndpoint    *Endpoint
}

func NewLeastConnection(p *EndpointPool, initial string) EndpointIterator {
	return &LeastConnection{
		pool:            p,
		initialEndpoint: initial,
	}
}

func (r *LeastConnection) Next() *Endpoint {
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

func (r *LeastConnection) PreRequest(e *Endpoint) {
	e.Stats.NumberConnections.Increment()
}

func (r *LeastConnection) PostRequest(e *Endpoint) {
	e.Stats.NumberConnections.Decrement()
}

func (r *LeastConnection) next() *endpointElem {
	r.pool.Lock()
	defer r.pool.Unlock()

	var selected *endpointElem

	// none
	total := len(r.pool.endpoints)
	if total == 0 {
		return nil
	}

	// single endpoint
	if total == 1 {
		e := r.pool.endpoints[0]
		if e.isOverloaded() {
			return nil
		}

		return e
	}

	// more than 1 endpoint
	// select the least connection endpoint OR
	// random one within the least connection endpoints
	randIndices := randomize.Perm(total)

	for i := 0; i < total; i++ {
		randIdx := randIndices[i]
		cur := r.pool.endpoints[randIdx]
		if cur.isOverloaded() {
			continue
		}

		// our first is the least
		if i == 0 || selected == nil {
			selected = cur
			continue
		}

		if cur.endpoint.Stats.NumberConnections.Count() < selected.endpoint.Stats.NumberConnections.Count() {
			selected = cur
		}
	}
	return selected
}

func (r *LeastConnection) EndpointFailed(err error) {
	if r.lastEndpoint != nil {
		r.pool.EndpointFailed(r.lastEndpoint, err)
	}
}
