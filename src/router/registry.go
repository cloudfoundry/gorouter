package router

import (
	"fmt"
	"net/http"
	"strings"
	"sync"
)

type Uri string
type Uris []Uri

func (u Uri) ToLower() Uri {
	return Uri(strings.ToLower(string(u)))
}

func (ms Uris) Sub(ns Uris) Uris {
	var rs Uris

	for _, m := range ms {
		found := false
		for _, n := range ns {
			if m == n {
				found = true
				break
			}
		}

		if !found {
			rs = append(rs, m)
		}
	}

	return rs
}

type InstanceId string
type InstanceIds []InstanceId

func (is InstanceIds) Has(i InstanceId) bool {
	for _, j := range is {
		if j == i {
			return true
		}
	}

	return false
}

func (is InstanceIds) Remove(ia InstanceId) (InstanceIds, bool) {
	for i, ib := range is {
		if ia == ib {
			is[i] = is[len(is)-1]
			is = is[:len(is)-1]
			return is, true
		}
	}

	return is, false
}

type Endpoint struct {
	ApplicationId string
	InstanceId    InstanceId
	Host          string
	Port          uint16
	Tags          map[string]string
}

type registerMessage struct {
	sync.Mutex

	// TODO: this must be passed by the DEA
	instanceId InstanceId

	Host string            `json:"host"`
	Port uint16            `json:"port"`
	Uris Uris              `json:"uris"`
	Tags map[string]string `json:"tags"`
	Dea  string            `json:"dea"`
	App  string            `json:"app"`

	Sticky string
}

func (m *registerMessage) InstanceId() InstanceId {
	m.Lock()
	defer m.Unlock()

	if m.instanceId == "" {
		// Synthesize ID when it isn't set
		m.instanceId = InstanceId(fmt.Sprintf("%s-%s:%d", m.App, m.Host, m.Port))
	}

	return m.instanceId
}

func (m *registerMessage) Equals(n *registerMessage) bool {
	return m.InstanceId() == n.InstanceId()
}

type Registry struct {
	sync.RWMutex

	varz *Varz

	byUri        map[Uri]InstanceIds
	byInstanceId map[InstanceId]*registerMessage
}

func NewRegistry() *Registry {
	r := &Registry{}

	r.byUri = make(map[Uri]InstanceIds)
	r.byInstanceId = make(map[InstanceId]*registerMessage)

	return r
}

func (r *Registry) NumUris() int {
	r.RLock()
	defer r.RUnlock()

	return len(r.byUri)
}

func (r *Registry) NumInstances() int {
	r.RLock()
	defer r.RUnlock()

	return len(r.byInstanceId)
}

func (r *Registry) registerUri(u Uri, i InstanceId) {
	u = u.ToLower()

	is := r.byUri[u]
	if is == nil {
		is = make(InstanceIds, 0)

		if r.varz != nil {
			r.varz.RegisterApp(string(u))
		}
	} else {
		if is.Has(i) {
			// The caller is expected to filter this
			panic("is already has i")
		}
	}

	is = append(is, i)
	r.byUri[u] = is
}

func (r *Registry) Register(m *registerMessage) {
	r.Lock()
	defer r.Unlock()

	i := m.InstanceId()

	n := r.byInstanceId[i]
	if n != nil {
		// Unregister uri's that are no longer referenced
		for _, u := range n.Uris.Sub(m.Uris) {
			r.unregisterUri(u, i)
		}
		// Register uri's that are newly referenced
		for _, u := range m.Uris.Sub(n.Uris) {
			r.registerUri(u, i)
		}
	} else {
		// Register all uri's
		for _, u := range m.Uris {
			r.registerUri(u, i)
		}
	}

	// Overwrite message
	r.byInstanceId[i] = m

	if r.varz != nil {
		r.varz.Urls = len(r.byUri)
		r.varz.Droplets = len(r.byInstanceId)
	}
}

func (r *Registry) unregisterUri(u Uri, i InstanceId) {
	u = u.ToLower()

	is := r.byUri[u]
	if is == nil {
		// The caller is expected to filter this
		panic("no such uri")
	}

	is, ok := is.Remove(i)
	if !ok {
		// The caller is expected to filter this
		panic("is does not have i")
	}

	if len(is) == 0 {
		delete(r.byUri, u)

		if r.varz != nil {
			r.varz.UnregisterApp(string(u))
		}
	} else {
		r.byUri[u] = is
	}
}

func (r *Registry) Unregister(m *registerMessage) {
	r.Lock()
	defer r.Unlock()

	i := m.InstanceId()

	// The message may contain URIs the registry doesn't know about.
	// Only unregister what the registry knows about.
	n := r.byInstanceId[i]
	if n != nil {
		for _, u := range n.Uris {
			r.unregisterUri(u, i)
		}
	}

	delete(r.byInstanceId, i)

	if r.varz != nil {
		r.varz.Urls = len(r.byUri)
		r.varz.Droplets = len(r.byInstanceId)
	}
}

func (r *Registry) Lookup(req *http.Request) InstanceIds {
	host := req.Host

	// Remove :<port>
	pos := strings.Index(host, ":")
	if pos >= 0 {
		host = host[0:pos]
	}

	r.RLock()
	defer r.RUnlock()

	return r.byUri[Uri(host).ToLower()]
}

func (r *Registry) LookupByInstanceId(i InstanceId) (Endpoint, bool) {
	r.RLock()
	defer r.RUnlock()

	var e Endpoint

	m, ok := r.byInstanceId[i]
	if ok {
		e = Endpoint{
			InstanceId:    i,
			ApplicationId: m.App,
			Host:          m.Host,
			Port:          m.Port,
			Tags:          m.Tags,
		}

		return e, true
	}

	return e, false
}

func (r *Registry) LookupByInstanceIds(is InstanceIds) ([]Endpoint, bool) {
	ms := make([]Endpoint, len(is))

	r.RLock()
	defer r.RUnlock()

	for j, i := range is {
		m, ok := r.byInstanceId[i]
		if !ok {
			return nil, false
		}

		ms[j] = Endpoint{
			InstanceId:    i,
			ApplicationId: m.App,
			Host:          m.Host,
			Port:          m.Port,
			Tags:          m.Tags,
		}
	}

	return ms, true
}
