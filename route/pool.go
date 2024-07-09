package route

import (
	"encoding/json"
	"fmt"
	"maps"
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
	ApplicationId          string
	AvailabilityZone       string
	addr                   string
	Protocol               string
	Tags                   map[string]string
	ServerCertDomainSAN    string
	PrivateInstanceId      string
	StaleThreshold         time.Duration
	RouteServiceUrl        string
	PrivateInstanceIndex   string
	ModificationTag        models.ModificationTag
	Stats                  *Stats
	IsolationSegment       string
	useTls                 bool
	roundTripper           ProxyRoundTripper
	roundTripperMutex      sync.RWMutex
	UpdatedAt              time.Time
	RoundTripperInit       sync.Once
	LoadBalancingAlgorithm string
}

func (e *Endpoint) RoundTripper() ProxyRoundTripper {
	e.roundTripperMutex.RLock()
	defer e.roundTripperMutex.RUnlock()

	return e.roundTripper
}

func (e *Endpoint) SetRoundTripper(tripper ProxyRoundTripper) {
	e.roundTripperMutex.Lock()
	defer e.roundTripperMutex.Unlock()

	e.roundTripper = tripper
}

func (e *Endpoint) SetRoundTripperIfNil(roundTripperCtor func() ProxyRoundTripper) {
	e.roundTripperMutex.Lock()
	defer e.roundTripperMutex.Unlock()

	if e.roundTripper == nil {
		e.roundTripper = roundTripperCtor()
	}
}

func (e *Endpoint) Equal(e2 *Endpoint) bool {
	if e2 == nil {
		return false
	}
	return e.ApplicationId == e2.ApplicationId &&
		e.addr == e2.addr &&
		e.Protocol == e2.Protocol &&
		maps.Equal(e.Tags, e2.Tags) &&
		e.ServerCertDomainSAN == e2.ServerCertDomainSAN &&
		e.PrivateInstanceId == e2.PrivateInstanceId &&
		e.StaleThreshold == e2.StaleThreshold &&
		e.RouteServiceUrl == e2.RouteServiceUrl &&
		e.PrivateInstanceIndex == e2.PrivateInstanceIndex &&
		e.ModificationTag == e2.ModificationTag &&
		e.IsolationSegment == e2.IsolationSegment &&
		e.useTls == e2.useTls &&
		e.UpdatedAt == e2.UpdatedAt

}

//go:generate counterfeiter -o fakes/fake_endpoint_iterator.go . EndpointIterator
type EndpointIterator interface {
	// Next MUST either return the next endpoint available or nil. It MUST NOT return the same endpoint.
	// All available endpoints MUST have been used before any can be used again.
	// ProxyRoundTripper will not retry more often than endpoints available.
	Next(attempt int) *Endpoint
	EndpointFailed(err error)
	PreRequest(e *Endpoint)
	PostRequest(e *Endpoint)
}

type endpointElem struct {
	sync.RWMutex
	endpoint           *Endpoint
	index              int
	updated            time.Time
	failedAt           *time.Time
	maxConnsPerBackend int64
}

type EndpointPool struct {
	sync.Mutex
	endpoints []*endpointElem
	index     map[string]*endpointElem

	host        string
	contextPath string
	RouteSvcUrl string

	retryAfterFailure  time.Duration
	NextIdx            int
	maxConnsPerBackend int64

	random      *rand.Rand
	logger      logger.Logger
	updatedAt   time.Time
	LBAlgorithm string
}

type EndpointOpts struct {
	AppId                   string
	AvailabilityZone        string
	Host                    string
	Port                    uint16
	Protocol                string
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
	LoadBalancingAlgorithm  string
}

func NewEndpoint(opts *EndpointOpts) *Endpoint {
	return &Endpoint{
		ApplicationId:          opts.AppId,
		AvailabilityZone:       opts.AvailabilityZone,
		addr:                   fmt.Sprintf("%s:%d", opts.Host, opts.Port),
		Protocol:               opts.Protocol,
		Tags:                   opts.Tags,
		useTls:                 opts.UseTLS,
		ServerCertDomainSAN:    opts.ServerCertDomainSAN,
		PrivateInstanceId:      opts.PrivateInstanceId,
		PrivateInstanceIndex:   opts.PrivateInstanceIndex,
		StaleThreshold:         time.Duration(opts.StaleThresholdInSeconds) * time.Second,
		RouteServiceUrl:        opts.RouteServiceUrl,
		ModificationTag:        opts.ModificationTag,
		Stats:                  NewStats(),
		IsolationSegment:       opts.IsolationSegment,
		UpdatedAt:              opts.UpdatedAt,
		LoadBalancingAlgorithm: opts.LoadBalancingAlgorithm,
	}
}

func (e *Endpoint) IsTLS() bool {
	return e.useTls
}

type PoolOpts struct {
	RetryAfterFailure      time.Duration
	Host                   string
	ContextPath            string
	MaxConnsPerBackend     int64
	Logger                 logger.Logger
	LoadBalancingAlgorithm string
}

func NewPool(opts *PoolOpts) *EndpointPool {
	return &EndpointPool{
		endpoints:          make([]*endpointElem, 0, 1),
		index:              make(map[string]*endpointElem),
		retryAfterFailure:  opts.RetryAfterFailure,
		NextIdx:            -1,
		maxConnsPerBackend: opts.MaxConnsPerBackend,
		host:               opts.Host,
		contextPath:        opts.ContextPath,
		random:             rand.New(rand.NewSource(time.Now().UnixNano())),
		logger:             opts.Logger,
		updatedAt:          time.Now(),
		LBAlgorithm:        opts.LoadBalancingAlgorithm,
	}
}

func PoolsMatch(p1, p2 *EndpointPool) bool {
	return p1.Host() == p2.Host() && p1.ContextPath() == p2.ContextPath()
}

func (p *EndpointPool) Host() string {
	return p.host
}

func (p *EndpointPool) ContextPath() string {
	return p.contextPath
}

func (p *EndpointPool) MaxConnsPerBackend() int64 {
	return p.maxConnsPerBackend
}

func (p *EndpointPool) LastUpdated() time.Time {
	return p.updatedAt
}

func (p *EndpointPool) Update() {
	p.updatedAt = time.Now()
}

func (p *EndpointPool) Put(endpoint *Endpoint) PoolPutResult {
	p.Lock()
	defer p.Unlock()

	var result PoolPutResult
	e, found := p.index[endpoint.CanonicalAddr()]
	if found {
		result = UPDATED
		if !e.endpoint.Equal(endpoint) {
			e.Lock()
			defer e.Unlock()

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
				endpoint.SetRoundTripper(oldEndpoint.RoundTripper())
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
	p.RouteSvcUrl = e.endpoint.RouteServiceUrl
	e.updated = time.Now()
	// set the update time of the pool
	p.Update()

	return result
}

func (p *EndpointPool) RouteServiceUrl() string {
	p.Lock()
	defer p.Unlock()
	return p.RouteSvcUrl
}

func (p *EndpointPool) PruneEndpoints() []*Endpoint {
	p.Lock()
	defer p.Unlock()

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

	return prunedEndpoints
}

// Returns true if the endpoint was removed from the EndpointPool, false otherwise.
func (p *EndpointPool) Remove(endpoint *Endpoint) bool {
	var e *endpointElem

	p.Lock()
	defer p.Unlock()
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

func (p *EndpointPool) removeEndpoint(e *endpointElem) {
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
	p.Update()
}

func (p *EndpointPool) Endpoints(logger logger.Logger, initial string, mustBeSticky bool, azPreference string, az string) EndpointIterator {
	switch p.LBAlgorithm {
	case config.LOAD_BALANCE_LC:
		return NewLeastConnection(logger, p, initial, mustBeSticky, azPreference == config.AZ_PREF_LOCAL, az)
	default:
		return NewRoundRobin(logger, p, initial, mustBeSticky, azPreference == config.AZ_PREF_LOCAL, az)
	}
}

func (p *EndpointPool) NumEndpoints() int {
	p.Lock()
	defer p.Unlock()
	return len(p.endpoints)
}

func (p *EndpointPool) findById(id string) *endpointElem {
	p.Lock()
	defer p.Unlock()
	return p.index[id]
}

func (p *EndpointPool) IsEmpty() bool {
	p.Lock()
	l := len(p.endpoints)
	p.Unlock()

	return l == 0
}

func (p *EndpointPool) IsOverloaded() bool {
	if p.IsEmpty() {
		return false
	}

	p.Lock()
	defer p.Unlock()
	if p.maxConnsPerBackend == 0 {
		return false
	}

	if p.maxConnsPerBackend > 0 {
		for _, e := range p.endpoints {
			if e.endpoint.Stats.NumberConnections.Count() < p.maxConnsPerBackend {
				return false
			}
		}
	}

	return true
}

func (p *EndpointPool) MarkUpdated(t time.Time) {
	p.Lock()
	defer p.Unlock()
	for _, e := range p.endpoints {
		e.updated = t
	}
}

func (p *EndpointPool) EndpointFailed(endpoint *Endpoint, err error) {
	p.Lock()
	defer p.Unlock()
	e := p.index[endpoint.CanonicalAddr()]
	if e == nil {
		return
	}

	logger := p.logger.With(zap.Nest("route-endpoint", endpoint.ToLogData()...))
	if e.endpoint.useTls && fails.PrunableClassifiers.Classify(err) {
		logger.Error("prune-failed-endpoint")
		p.removeEndpoint(e)

		return
	}

	if fails.FailableClassifiers.Classify(err) {
		logger.Error("endpoint-marked-as-ineligible")
		e.failed()
		return
	}

}

func (p *EndpointPool) Each(f func(endpoint *Endpoint)) {
	p.Lock()
	for _, e := range p.endpoints {
		f(e.endpoint)
	}
	p.Unlock()
}

func (p *EndpointPool) MarshalJSON() ([]byte, error) {
	p.Lock()
	endpoints := make([]*Endpoint, 0, len(p.endpoints))
	for _, e := range p.endpoints {
		endpoints = append(endpoints, e.endpoint)
	}
	p.Unlock()

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

	return e.endpoint.Stats.NumberConnections.Count() >= e.maxConnsPerBackend
}

func (e *Endpoint) MarshalJSON() ([]byte, error) {
	var jsonObj struct {
		Address             string            `json:"address"`
		AvailabilityZone    string            `json:"availability_zone"`
		Protocol            string            `json:"protocol"`
		TLS                 bool              `json:"tls"`
		TTL                 int               `json:"ttl"`
		RouteServiceUrl     string            `json:"route_service_url,omitempty"`
		Tags                map[string]string `json:"tags"`
		IsolationSegment    string            `json:"isolation_segment,omitempty"`
		PrivateInstanceId   string            `json:"private_instance_id,omitempty"`
		ServerCertDomainSAN string            `json:"server_cert_domain_san,omitempty"`
	}

	jsonObj.Address = e.addr
	jsonObj.AvailabilityZone = e.AvailabilityZone
	jsonObj.Protocol = e.Protocol
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

func (e *Endpoint) Component() string {
	return e.Tags["component"]
}

func (e *Endpoint) ToLogData() []zap.Field {
	return []zap.Field{
		zap.String("ApplicationId", e.ApplicationId),
		zap.String("Addr", e.addr),
		zap.Object("Tags", e.Tags),
		zap.String("RouteServiceUrl", e.RouteServiceUrl),
		zap.String("AZ", e.AvailabilityZone),
	}
}

func (e *Endpoint) modificationTagSameOrNewer(other *Endpoint) bool {
	return e.ModificationTag == other.ModificationTag || e.ModificationTag.SucceededBy(&other.ModificationTag)
}
