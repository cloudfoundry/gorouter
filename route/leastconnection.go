package route

import (
	"math/rand"
	"time"
)

type LeastConnection struct {
	pool                  *EndpointPool
	initialEndpoint       string
	lastEndpoint          *Endpoint
	randomize             *rand.Rand
	locallyOptimistic     bool
	localAvailabilityZone string
}

func NewLeastConnection(p *EndpointPool, initial string, locallyOptimistic bool, localAvailabilityZone string) EndpointIterator {
	return &LeastConnection{
		pool:                  p,
		initialEndpoint:       initial,
		randomize:             rand.New(rand.NewSource(time.Now().UnixNano())),
		locallyOptimistic:     locallyOptimistic,
		localAvailabilityZone: localAvailabilityZone,
	}
}

func (r *LeastConnection) Next(attempt int) *Endpoint {
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

func (r *LeastConnection) PreRequest(e *Endpoint) {
	e.Stats.NumberConnections.Increment()
}

func (r *LeastConnection) PostRequest(e *Endpoint) {
	e.Stats.NumberConnections.Decrement()
}

func (r *LeastConnection) next(attempt int) *endpointElem {
	r.pool.Lock()
	defer r.pool.Unlock()

	var selected, selectedLocal *endpointElem
	localDesired := r.locallyOptimistic && attempt == 0

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

	randIndices := r.randomize.Perm(total)
	for i := 0; i < total; i++ {
		randIdx := randIndices[i]
		cur := r.pool.endpoints[randIdx]
		curIsLocal := cur.endpoint.AvailabilityZone == r.localAvailabilityZone

		// Never select an endpoint that is overloaded
		if cur.isOverloaded() {
			continue
		}

		// Initialize selectedLocal to the first non-overloaded local endpoint
		if localDesired {
			if curIsLocal && selectedLocal == nil {
				selectedLocal = cur
			}
		}

		// Initialize selected to the first non-overloaded endpoint
		if i == 0 || selected == nil {
			selected = cur
			continue
		}

		// If the current option is better than the selected option, select the current
		if cur.endpoint.Stats.NumberConnections.Count() < selected.endpoint.Stats.NumberConnections.Count() {
			selected = cur
		}

		if localDesired {
			// If the current option is local and is better than the selectedLocal endpoint, then swap
			if curIsLocal && cur.endpoint.Stats.NumberConnections.Count() < selectedLocal.endpoint.Stats.NumberConnections.Count() {
				selectedLocal = cur
			}
		}
	}

	if localDesired && selectedLocal != nil {
		return selectedLocal
	}

	return selected
}

func (r *LeastConnection) EndpointFailed(err error) {
	if r.lastEndpoint != nil {
		r.pool.EndpointFailed(r.lastEndpoint, err)
	}
}
