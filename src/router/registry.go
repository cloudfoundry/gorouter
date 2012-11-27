package router

import (
	"fmt"
	"net/http"
	"router/stats"
	"strings"
	"sync"
	"time"
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

type BackendId string
type BackendIds []BackendId

func (x BackendIds) Has(y BackendId) bool {
	for _, xb := range x {
		if xb == y {
			return true
		}
	}

	return false
}

func (x BackendIds) Remove(y BackendId) (BackendIds, bool) {
	for i, xb := range x {
		if xb == y {
			x[i] = x[len(x)-1]
			x = x[:len(x)-1]
			return x, true
		}
	}

	return x, false
}

type Backend struct {
	ApplicationId string
	BackendId     BackendId
	Host          string
	Port          uint16
	Tags          map[string]string

	PrivateInstanceId string
}

type registerMessage struct {
	sync.Mutex

	b BackendId

	Host string            `json:"host"`
	Port uint16            `json:"port"`
	Uris Uris              `json:"uris"`
	Tags map[string]string `json:"tags"`
	Dea  string            `json:"dea"`
	App  string            `json:"app"`

	PrivateInstanceId string `json:"private_instance_id"`
}

func (m *registerMessage) BackendId() BackendId {
	m.Lock()
	defer m.Unlock()

	if m.b == "" {
		// Synthesize ID when it isn't set
		if m.Host != "" && m.Port != 0 {
			m.b = BackendId(fmt.Sprintf("%s:%d", m.Host, m.Port))
		}
	}

	return m.b
}

func (m *registerMessage) Equals(n *registerMessage) bool {
	return m.BackendId() == n.BackendId()
}

type Registry struct {
	sync.RWMutex

	*stats.ActiveApps
	*stats.TopApps

	byUri       map[Uri]BackendIds
	byBackendId map[BackendId]*registerMessage
}

func NewRegistry() *Registry {
	r := &Registry{}

	r.ActiveApps = stats.NewActiveApps()
	r.TopApps = stats.NewTopApps()

	r.byUri = make(map[Uri]BackendIds)
	r.byBackendId = make(map[BackendId]*registerMessage)

	return r
}

func (r *Registry) NumUris() int {
	r.RLock()
	defer r.RUnlock()

	return len(r.byUri)
}

func (r *Registry) NumBackends() int {
	r.RLock()
	defer r.RUnlock()

	return len(r.byBackendId)
}

func (r *Registry) registerUri(u Uri, i BackendId) {
	u = u.ToLower()

	x := r.byUri[u]
	if x == nil {
		x = make([]BackendId, 0)
	} else {
		if x.Has(i) {
			// The caller is expected to filter this
			panic("list of backend ids already contains backend")
		}
	}

	x = append(x, i)
	r.byUri[u] = x
}

func (r *Registry) Register(m *registerMessage) {
	r.Lock()
	defer r.Unlock()

	i := m.BackendId()
	if i == "" {
		return
	}

	n := r.byBackendId[i]
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
	r.byBackendId[i] = m
}

func (r *Registry) unregisterUri(u Uri, i BackendId) {
	u = u.ToLower()

	x := r.byUri[u]
	if x == nil {
		// The caller bs expected to filter this
		panic("no such uri")
	}

	x, ok := x.Remove(i)
	if !ok {
		// The caller is expected to filter this
		panic("list of backend ids already contains backend")
	}

	if len(x) == 0 {
		delete(r.byUri, u)
	} else {
		r.byUri[u] = x
	}
}

func (r *Registry) Unregister(m *registerMessage) {
	r.Lock()
	defer r.Unlock()

	i := m.BackendId()

	// The message may contain URIs the registry doesn't know about.
	// Only unregister what the registry knows about.
	n := r.byBackendId[i]
	if n != nil {
		for _, u := range n.Uris {
			r.unregisterUri(u, i)
		}
	}

	delete(r.byBackendId, i)
}

func (r *Registry) Lookup(req *http.Request) BackendIds {
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

func (r *Registry) LookupByBackendId(i BackendId) (Backend, bool) {
	r.RLock()
	defer r.RUnlock()

	var b Backend

	m, ok := r.byBackendId[i]
	if ok {
		b = Backend{
			BackendId:     i,
			ApplicationId: m.App,
			Host:          m.Host,
			Port:          m.Port,
			Tags:          m.Tags,

			PrivateInstanceId: m.PrivateInstanceId,
		}

		return b, true
	}

	return b, false
}

func (r *Registry) LookupByBackendIds(x []BackendId) ([]Backend, bool) {
	y := make([]Backend, len(x))

	r.RLock()
	defer r.RUnlock()

	for i, j := range x {
		m, ok := r.byBackendId[j]
		if !ok {
			return nil, false
		}

		y[i] = Backend{
			BackendId:     j,
			ApplicationId: m.App,
			Host:          m.Host,
			Port:          m.Port,
			Tags:          m.Tags,

			PrivateInstanceId: m.PrivateInstanceId,
		}
	}

	return y, true
}

func (r *Registry) CaptureBackendRequest(x Backend, t time.Time) {
	if x.ApplicationId != "" {
		r.ActiveApps.Mark(x.ApplicationId, t)
		r.TopApps.Mark(x.ApplicationId, t)
	}
}
