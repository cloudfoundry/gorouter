package route

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
		e = r.pool.next()
	}

	r.lastEndpoint = e

	return e
}

func (r *RoundRobin) EndpointFailed() {
	if r.lastEndpoint != nil {
		r.pool.endpointFailed(r.lastEndpoint)
	}
}
