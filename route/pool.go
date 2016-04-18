package route

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"sync"
	"time"
)

var random = rand.New(rand.NewSource(time.Now().UnixNano()))

type Endpoint struct {
	ApplicationId     string
	addr              string
	Tags              map[string]string
	PrivateInstanceId string
	staleThreshold    time.Duration
	RouteServiceUrl   string
}

type EndpointIterator interface {
	Next() *Endpoint
	EndpointFailed()
}

type endpointIterator struct {
	pool *Pool

	initialEndpoint string
	lastEndpoint    *Endpoint
}

type endpointElem struct {
	endpoint *Endpoint
	index    int
	updated  time.Time
	failedAt *time.Time
}

type Pool struct {
	lock      sync.Mutex
	endpoints []*endpointElem
	index     map[string]*endpointElem

	contextPath     string
	routeServiceUrl string

	retryAfterFailure time.Duration
	nextIdx           int
}

func NewEndpoint(appId, host string, port uint16, privateInstanceId string,
	tags map[string]string, staleThresholdInSeconds int, routeServiceUrl string) *Endpoint {
	return &Endpoint{
		ApplicationId:     appId,
		addr:              fmt.Sprintf("%s:%d", host, port),
		Tags:              tags,
		PrivateInstanceId: privateInstanceId,
		staleThreshold:    time.Duration(staleThresholdInSeconds) * time.Second,
		RouteServiceUrl:   routeServiceUrl,
	}
}

func NewPool(retryAfterFailure time.Duration, contextPath string) *Pool {
	return &Pool{
		endpoints:         make([]*endpointElem, 0, 1),
		index:             make(map[string]*endpointElem),
		retryAfterFailure: retryAfterFailure,
		nextIdx:           -1,
		contextPath:       contextPath,
	}
}

func (p *Pool) ContextPath() string {
	return p.contextPath
}

func (p *Pool) Put(endpoint *Endpoint) bool {
	p.lock.Lock()
	defer p.lock.Unlock()

	e, found := p.index[endpoint.CanonicalAddr()]
	if found {
		if e.endpoint == endpoint {
			return false
		}

		oldEndpoint := e.endpoint
		e.endpoint = endpoint

		if oldEndpoint.PrivateInstanceId != endpoint.PrivateInstanceId {
			delete(p.index, oldEndpoint.PrivateInstanceId)
			p.index[endpoint.PrivateInstanceId] = e
		}
	} else {
		e = &endpointElem{
			endpoint: endpoint,
			index:    len(p.endpoints),
		}

		p.endpoints = append(p.endpoints, e)

		p.index[endpoint.CanonicalAddr()] = e
		p.index[endpoint.PrivateInstanceId] = e
	}

	e.updated = time.Now()

	return !found
}

func (p *Pool) RouteServiceUrl() string {
	p.lock.Lock()
	defer p.lock.Unlock()

	if len(p.endpoints) > 0 {
		endpt := p.endpoints[0]
		return endpt.endpoint.RouteServiceUrl
	} else {
		return ""
	}
}

func (p *Pool) PruneEndpoints(defaultThreshold time.Duration) {
	p.lock.Lock()

	last := len(p.endpoints)
	now := time.Now()

	for i := 0; i < last; {
		e := p.endpoints[i]

		staleTime := now.Add(-defaultThreshold)
		if e.endpoint.staleThreshold > 0 && e.endpoint.staleThreshold < defaultThreshold {
			staleTime = now.Add(-e.endpoint.staleThreshold)
		}

		if e.updated.Before(staleTime) {
			p.removeEndpoint(e)
			last--
		} else {
			i++
		}
	}

	p.lock.Unlock()
}

func (p *Pool) Remove(endpoint *Endpoint) bool {
	var e *endpointElem

	p.lock.Lock()
	l := len(p.endpoints)
	if l > 0 {
		e = p.index[endpoint.CanonicalAddr()]
		if e != nil {
			p.removeEndpoint(e)
		}
	}
	p.lock.Unlock()

	return e != nil
}

func (p *Pool) removeEndpoint(e *endpointElem) {
	i := e.index
	es := p.endpoints
	last := len(es)
	// re-ordering delete
	es[last-1], es[i], es = nil, es[last-1], es[:last-1]
	if i < last-1 {
		es[i].index = i
	}
	p.endpoints = es

	delete(p.index, e.endpoint.CanonicalAddr())
	delete(p.index, e.endpoint.PrivateInstanceId)
}

func (p *Pool) Endpoints(initial string) EndpointIterator {
	return newEndpointIterator(p, initial)
}

func (p *Pool) next() *Endpoint {
	p.lock.Lock()
	defer p.lock.Unlock()

	last := len(p.endpoints)
	if last == 0 {
		return nil
	}

	if p.nextIdx == -1 {
		p.nextIdx = random.Intn(last)
	} else if p.nextIdx >= last {
		p.nextIdx = 0
	}

	startIdx := p.nextIdx
	curIdx := startIdx
	for {
		e := p.endpoints[curIdx]

		curIdx++
		if curIdx == last {
			curIdx = 0
		}

		if e.failedAt != nil {
			curTime := time.Now()
			if curTime.Sub(*e.failedAt) > p.retryAfterFailure {
				// exipired failure window
				e.failedAt = nil
			}
		}

		if e.failedAt == nil {
			p.nextIdx = curIdx
			return e.endpoint
		}

		if curIdx == startIdx {
			// all endpoints are marked failed so reset everything to available
			for _, e2 := range p.endpoints {
				e2.failedAt = nil
			}
		}
	}
}

func (p *Pool) findById(id string) *Endpoint {
	var endpoint *Endpoint
	p.lock.Lock()
	e := p.index[id]
	if e != nil {
		endpoint = e.endpoint
	}
	p.lock.Unlock()

	return endpoint
}

func (p *Pool) IsEmpty() bool {
	p.lock.Lock()
	l := len(p.endpoints)
	p.lock.Unlock()

	return l == 0
}

func (p *Pool) MarkUpdated(t time.Time) {
	p.lock.Lock()
	for _, e := range p.endpoints {
		e.updated = t
	}
	p.lock.Unlock()
}

func (p *Pool) endpointFailed(endpoint *Endpoint) {
	p.lock.Lock()
	e := p.index[endpoint.CanonicalAddr()]
	if e != nil {
		e.failed()
	}
	p.lock.Unlock()
}

func (p *Pool) Each(f func(endpoint *Endpoint)) {
	p.lock.Lock()
	for _, e := range p.endpoints {
		f(e.endpoint)
	}
	p.lock.Unlock()
}

func (p *Pool) MarshalJSON() ([]byte, error) {
	p.lock.Lock()
	endpoints := make([]Endpoint, 0, len(p.endpoints))
	for _, e := range p.endpoints {
		endpoints = append(endpoints, *e.endpoint)
	}
	p.lock.Unlock()

	return json.Marshal(endpoints)
}

func newEndpointIterator(p *Pool, initial string) EndpointIterator {
	return &endpointIterator{
		pool:            p,
		initialEndpoint: initial,
	}
}

func (i *endpointIterator) Next() *Endpoint {
	var e *Endpoint
	if i.initialEndpoint != "" {
		e = i.pool.findById(i.initialEndpoint)
		i.initialEndpoint = ""
	}

	if e == nil {
		e = i.pool.next()
	}

	i.lastEndpoint = e

	return e
}

func (i *endpointIterator) EndpointFailed() {
	if i.lastEndpoint != nil {
		i.pool.endpointFailed(i.lastEndpoint)
	}
}

func (e *endpointElem) failed() {
	t := time.Now()
	e.failedAt = &t
}

func (e *Endpoint) MarshalJSON() ([]byte, error) {
	var jsonObj struct {
		Address         string `json:"address"`
		TTL             int    `json:"ttl"`
		RouteServiceUrl string `json:"route_service_url,omitempty"`
	}

	jsonObj.Address = e.addr
	jsonObj.RouteServiceUrl = e.RouteServiceUrl
	jsonObj.TTL = int(e.staleThreshold.Seconds())
	return json.Marshal(jsonObj)
}

func (e *Endpoint) CanonicalAddr() string {
	return e.addr
}

func (rm *Endpoint) Component() string {
	return rm.Tags["component"]
}

func (e *Endpoint) ToLogData() interface{} {
	return struct {
		ApplicationId   string
		Addr            string
		Tags            map[string]string
		RouteServiceUrl string
	}{
		e.ApplicationId,
		e.addr,
		e.Tags,
		e.RouteServiceUrl,
	}
}
