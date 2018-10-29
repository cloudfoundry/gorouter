package route

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/uber-go/zap"

	"code.cloudfoundry.org/gorouter/config"
	"code.cloudfoundry.org/gorouter/logger"
	"code.cloudfoundry.org/gorouter/proxy/fails"
	"code.cloudfoundry.org/routing-api/models"
)

type Counter struct {
	value int64
}

type PoolPutResult int

const (
	UNMODIFIED = PoolPutResult(iota)
	UPDATED
	ADDED
)

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

type ProxyRoundTripper interface {
	http.RoundTripper
	CancelRequest(*http.Request)
}

type Endpoint struct {
	sync.RWMutex
	ApplicationId        string
	addr                 string
	Tags                 map[string]string
	ServerCertDomainSAN  string
	PrivateInstanceId    string
	StaleThreshold       time.Duration
	RouteServiceUrl      string
	PrivateInstanceIndex string
	ModificationTag      models.ModificationTag
	Stats                *Stats
	IsolationSegment     string
	useTls               bool
	RoundTripper         ProxyRoundTripper
	UpdatedAt            time.Time
}

//go:generate counterfeiter -o fakes/fake_endpoint_iterator.go . EndpointIterator
type EndpointIterator interface {
	Next() *Endpoint
	EndpointFailed(err error)
	PreRequest(e *Endpoint)
	PostRequest(e *Endpoint)
}

type endpointElem struct {
	endpoint           *Endpoint
	index              int
	updated            time.Time
	failedAt           *time.Time
	maxConnsPerBackend int64
}

type Pool struct {
	lock      sync.Mutex
	endpoints []*endpointElem
	index     map[string]*endpointElem

	host            string
	contextPath     string
	routeServiceUrl string

	retryAfterFailure  time.Duration
	nextIdx            int
	maxConnsPerBackend int64

	random *rand.Rand
	logger logger.Logger
}

type EndpointOpts struct {
	AppId                   string
	Host                    string
	Port                    uint16
	ServerCertDomainSAN     string
	PrivateInstanceId       string
	PrivateInstanceIndex    string
	Tags                    map[string]string
	StaleThresholdInSeconds int
	RouteServiceUrl         string
	ModificationTag         models.ModificationTag
	IsolationSegment        string
	UseTLS                  bool
	UpdatedAt               time.Time
}

func NewEndpoint(opts *EndpointOpts) *Endpoint {
	return &Endpoint{
		ApplicationId:        opts.AppId,
		addr:                 fmt.Sprintf("%s:%d", opts.Host, opts.Port),
		Tags:                 opts.Tags,
		useTls:               opts.UseTLS,
		ServerCertDomainSAN:  opts.ServerCertDomainSAN,
		PrivateInstanceId:    opts.PrivateInstanceId,
		PrivateInstanceIndex: opts.PrivateInstanceIndex,
		StaleThreshold:       time.Duration(opts.StaleThresholdInSeconds) * time.Second,
		RouteServiceUrl:      opts.RouteServiceUrl,
		ModificationTag:      opts.ModificationTag,
		Stats:                NewStats(),
		IsolationSegment:     opts.IsolationSegment,
		UpdatedAt:            opts.UpdatedAt,
	}
}

func (e *Endpoint) IsTLS() bool {
	return e.useTls
}

type PoolOpts struct {
	RetryAfterFailure  time.Duration
	Host               string
	ContextPath        string
	MaxConnsPerBackend int64
	Logger             logger.Logger
}

func NewPool(opts *PoolOpts) *Pool {
	return &Pool{
		endpoints:          make([]*endpointElem, 0, 1),
		index:              make(map[string]*endpointElem),
		retryAfterFailure:  opts.RetryAfterFailure,
		nextIdx:            -1,
		maxConnsPerBackend: opts.MaxConnsPerBackend,
		host:               opts.Host,
		contextPath:        opts.ContextPath,
		random:             rand.New(rand.NewSource(time.Now().UnixNano())),
		logger:             opts.Logger,
	}
}

func PoolsMatch(p1, p2 *Pool) bool {
	return p1.Host() == p2.Host() && p1.ContextPath() == p2.ContextPath()
}

func (p *Pool) Host() string {
	return p.host
}

func (p *Pool) ContextPath() string {
	return p.contextPath
}

func (p *Pool) MaxConnsPerBackend() int64 {
	return p.maxConnsPerBackend
}

// Returns true if endpoint was added or updated, false otherwise
func (p *Pool) Put(endpoint *Endpoint) PoolPutResult {
	p.lock.Lock()
	defer p.lock.Unlock()

	var result PoolPutResult
	e, found := p.index[endpoint.CanonicalAddr()]
	if found {
		result = UPDATED
		if e.endpoint != endpoint {
			e.endpoint.Lock()
			defer e.endpoint.Unlock()

			if !e.endpoint.ModificationTag.SucceededBy(&endpoint.ModificationTag) {
				return UNMODIFIED
			}

			oldEndpoint := e.endpoint
			e.endpoint = endpoint

			if oldEndpoint.PrivateInstanceId != endpoint.PrivateInstanceId {
				delete(p.index, oldEndpoint.PrivateInstanceId)
				p.index[endpoint.PrivateInstanceId] = e
			}

			if oldEndpoint.ServerCertDomainSAN == endpoint.ServerCertDomainSAN {
				endpoint.RoundTripper = oldEndpoint.RoundTripper
			}
		}
	} else {
		result = ADDED
		e = &endpointElem{
			endpoint:           endpoint,
			index:              len(p.endpoints),
			maxConnsPerBackend: p.maxConnsPerBackend,
		}

		p.endpoints = append(p.endpoints, e)

		p.index[endpoint.CanonicalAddr()] = e
		p.index[endpoint.PrivateInstanceId] = e
	}

	e.updated = time.Now()

	return result
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

func (p *Pool) PruneEndpoints() []*Endpoint {
	p.lock.Lock()

	last := len(p.endpoints)
	now := time.Now()

	prunedEndpoints := []*Endpoint{}

	for i := 0; i < last; {
		e := p.endpoints[i]

		if e.endpoint.useTls {
			i++
			continue
		}

		staleTime := now.Add(-e.endpoint.StaleThreshold)

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

func (p *Pool) findById(id string) *endpointElem {
	p.lock.Lock()
	defer p.lock.Unlock()
	return p.index[id]
}

func (p *Pool) IsEmpty() bool {
	p.lock.Lock()
	l := len(p.endpoints)
	p.lock.Unlock()

	return l == 0
}

func (p *Pool) IsOverloaded() bool {
	if p.IsEmpty() {
		return true
	}

	p.lock.Lock()
	defer p.lock.Unlock()
	if p.maxConnsPerBackend == 0 {
		return false
	}

	if p.maxConnsPerBackend > 0 {
		for _, e := range p.endpoints {
			if e.endpoint.Stats.NumberConnections.value < p.maxConnsPerBackend {
				return false
			}
		}
	}

	return true
}

func (p *Pool) MarkUpdated(t time.Time) {
	p.lock.Lock()
	for _, e := range p.endpoints {
		e.updated = t
	}
	p.lock.Unlock()
}

func (p *Pool) EndpointFailed(endpoint *Endpoint, err error) {
	p.lock.Lock()
	defer p.lock.Unlock()
	e := p.index[endpoint.CanonicalAddr()]
	if e == nil {
		return
	}

	logger := p.logger.With(zap.Nest("route-endpoint", endpoint.ToLogData()...))
	if fails.ErrorTypes.Classify(err) {
		if e.endpoint.useTls {
			logger.Error("prune-failed-endpoint")
			p.removeEndpoint(e)

			return
		}

		logger.Error("endpoint-marked-as-ineligible")
		e.failed()
	}

	return
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
	endpoints := make([]*Endpoint, 0, len(p.endpoints))
	for _, e := range p.endpoints {
		endpoints = append(endpoints, e.endpoint)
	}
	p.lock.Unlock()

	return json.Marshal(endpoints)
}

func (e *endpointElem) failed() {
	t := time.Now()
	e.failedAt = &t
}

func (e *endpointElem) isOverloaded() bool {
	if e.maxConnsPerBackend == 0 {
		return false
	}

	return e.endpoint.Stats.NumberConnections.value >= e.maxConnsPerBackend
}

func (e *Endpoint) MarshalJSON() ([]byte, error) {
	var jsonObj struct {
		Address             string            `json:"address"`
		TLS                 bool              `json:"tls"`
		TTL                 int               `json:"ttl"`
		RouteServiceUrl     string            `json:"route_service_url,omitempty"`
		Tags                map[string]string `json:"tags"`
		IsolationSegment    string            `json:"isolation_segment,omitempty"`
		PrivateInstanceId   string            `json:"private_instance_id,omitempty"`
		ServerCertDomainSAN string            `json:"server_cert_domain_san,omitempty"`
	}

	jsonObj.Address = e.addr
	jsonObj.TLS = e.IsTLS()
	jsonObj.RouteServiceUrl = e.RouteServiceUrl
	jsonObj.TTL = int(e.StaleThreshold.Seconds())
	jsonObj.Tags = e.Tags
	jsonObj.IsolationSegment = e.IsolationSegment
	jsonObj.PrivateInstanceId = e.PrivateInstanceId
	jsonObj.ServerCertDomainSAN = e.ServerCertDomainSAN
	return json.Marshal(jsonObj)
}

func (e *Endpoint) CanonicalAddr() string {
	return e.addr
}

func (rm *Endpoint) Component() string {
	return rm.Tags["component"]
}

func (e *Endpoint) ToLogData() []zap.Field {
	return []zap.Field{
		zap.String("ApplicationId", e.ApplicationId),
		zap.String("Addr", e.addr),
		zap.Object("Tags", e.Tags),
		zap.String("RouteServiceUrl", e.RouteServiceUrl),
	}
}

func (e *Endpoint) modificationTagSameOrNewer(other *Endpoint) bool {
	return e.ModificationTag == other.ModificationTag || e.ModificationTag.SucceededBy(&other.ModificationTag)
}
