package route

import (
	"encoding/json"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"code.cloudfoundry.org/gorouter/config"
	"code.cloudfoundry.org/routing-api/models"
)

type Counter struct {
	value int64
}

func NewCounter(initial int64) *Counter {
	return &Counter{initial}
}

func (c *Counter) Increment() {
	atomic.AddInt64(&c.value, 1)
}
func (c *Counter) Decrement() {
	atomic.AddInt64(&c.value, -1)
}
func (c *Counter) Count() int64 {
	return atomic.LoadInt64(&c.value)
}

type Stats struct {
	NumberConnections *Counter
}

func NewStats() *Stats {
	return &Stats{
		NumberConnections: &Counter{},
	}
}

type Endpoint struct {
	ApplicationId        string
	addr                 string
	Tags                 map[string]string
	PrivateInstanceId    string
	staleThreshold       time.Duration
	RouteServiceUrl      string
	PrivateInstanceIndex string
	ModificationTag      models.ModificationTag
	Stats                *Stats
}

//go:generate counterfeiter -o fakes/fake_endpoint_iterator.go . EndpointIterator
type EndpointIterator interface {
	Next() *Endpoint
	EndpointFailed()
	PreRequest(e *Endpoint)
	PostRequest(e *Endpoint)
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

func NewEndpoint(appId, host string, port uint16, privateInstanceId string, privateInstanceIndex string,
	tags map[string]string, staleThresholdInSeconds int, routeServiceUrl string, modificationTag models.ModificationTag) *Endpoint {
	return &Endpoint{
		ApplicationId:        appId,
		addr:                 fmt.Sprintf("%s:%d", host, port),
		Tags:                 tags,
		PrivateInstanceId:    privateInstanceId,
		PrivateInstanceIndex: privateInstanceIndex,
		staleThreshold:       time.Duration(staleThresholdInSeconds) * time.Second,
		RouteServiceUrl:      routeServiceUrl,
		ModificationTag:      modificationTag,
		Stats:                NewStats(),
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

// Returns true if endpoint was added or updated, false otherwise
func (p *Pool) Put(endpoint *Endpoint) bool {
	p.lock.Lock()
	defer p.lock.Unlock()

	e, found := p.index[endpoint.CanonicalAddr()]
	if found {
		if e.endpoint == endpoint {
			return false
		}

		// check modification tag
		if !e.endpoint.ModificationTag.SucceededBy(&endpoint.ModificationTag) {
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

	return true
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

func (p *Pool) PruneEndpoints(defaultThreshold time.Duration) []*Endpoint {
	p.lock.Lock()

	last := len(p.endpoints)
	now := time.Now()

	prunedEndpoints := []*Endpoint{}

	for i := 0; i < last; {
		e := p.endpoints[i]

		staleTime := now.Add(-defaultThreshold)
		if e.endpoint.staleThreshold > 0 && e.endpoint.staleThreshold < defaultThreshold {
			staleTime = now.Add(-e.endpoint.staleThreshold)
		}

		if e.updated.Before(staleTime) {
			p.removeEndpoint(e)
			prunedEndpoints = append(prunedEndpoints, e.endpoint)
			last--
		} else {
			i++
		}
	}

	p.lock.Unlock()
	return prunedEndpoints
}

// Returns true if the endpoint was removed from the Pool, false otherwise.
func (p *Pool) Remove(endpoint *Endpoint) bool {
	var e *endpointElem

	p.lock.Lock()
	defer p.lock.Unlock()
	l := len(p.endpoints)
	if l > 0 {
		e = p.index[endpoint.CanonicalAddr()]
		if e != nil && e.endpoint.modificationTagSameOrNewer(endpoint) {
			p.removeEndpoint(e)
			return true
		}
	}

	return false
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

func (p *Pool) Endpoints(defaultLoadBalance, initial string) EndpointIterator {
	switch defaultLoadBalance {
	case config.LOAD_BALANCE_LC:
		return NewLeastConnection(p, initial)
	default:
		return NewRoundRobin(p, initial)
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

func (e *Endpoint) modificationTagSameOrNewer(other *Endpoint) bool {
	return e.ModificationTag == other.ModificationTag || e.ModificationTag.SucceededBy(&other.ModificationTag)
}
