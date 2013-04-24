package router

import (
	"encoding/json"
	"fmt"
	steno "github.com/cloudfoundry/gosteno"
	"math/rand"
	"router/config"
	"router/stats"
	"router/util"
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

func (x Uris) Has(y Uri) bool {
	for _, xb := range x {
		if xb == y {
			return true
		}
	}

	return false
}

func (x Uris) Remove(y Uri) (Uris, bool) {
	for i, xb := range x {
		if xb == y {
			x[i] = x[len(x)-1]
			x = x[:len(x)-1]
			return x, true
		}
	}

	return x, false
}

type BackendId string

type Backend struct {
	sync.Mutex

	*steno.Logger

	BackendId BackendId

	ApplicationId     string
	Host              string
	Port              uint16
	Tags              map[string]string
	PrivateInstanceId string

	U Uris
	t time.Time
}

func (b *Backend) MarshalJSON() ([]byte, error) {
	return json.Marshal(b.CanonicalAddr())
}

func newBackend(i BackendId, m *registryMessage, l *steno.Logger) *Backend {
	b := &Backend{
		Logger: l,

		BackendId: i,

		ApplicationId:     m.App,
		Host:              m.Host,
		Port:              m.Port,
		Tags:              m.Tags,
		PrivateInstanceId: m.PrivateInstanceId,

		U: make([]Uri, 0),
		t: time.Now(),
	}

	return b
}

func (b *Backend) CanonicalAddr() string {
	return fmt.Sprintf("%s:%d", b.Host, b.Port)
}

func (b *Backend) ToLogData() interface{} {
	return struct {
		ApplicationId string
		Host          string
		Port          uint16
		Tags          map[string]string
	}{
		b.ApplicationId,
		b.Host,
		b.Port,
		b.Tags,
	}
}

func (b *Backend) register(u Uri) bool {
	if !b.U.Has(u) {
		b.Infof("Register %s (%s)", u, b.BackendId)
		b.U = append(b.U, u)
		return true
	}

	return false
}

func (b *Backend) unregister(u Uri) bool {
	x, ok := b.U.Remove(u)
	if ok {
		b.Infof("Unregister %s (%s)", u, b.BackendId)
		b.U = x
	}

	return ok
}

// This is a transient struct. It doesn't maintain state.
type registryMessage struct {
	Host string            `json:"host"`
	Port uint16            `json:"port"`
	Uris Uris              `json:"uris"`
	Tags map[string]string `json:"tags"`
	App  string            `json:"app"`

	PrivateInstanceId string `json:"private_instance_id"`
}

func (m registryMessage) BackendId() (b BackendId, ok bool) {
	if m.Host != "" && m.Port != 0 {
		b = BackendId(fmt.Sprintf("%s:%d", m.Host, m.Port))
		ok = true
	}

	return
}

type Registry struct {
	sync.RWMutex

	*steno.Logger

	*stats.ActiveApps
	*stats.TopApps

	byUri       map[Uri][]*Backend
	byBackendId map[BackendId]*Backend

	staleTracker *util.ListMap

	pruneStaleDropletsInterval time.Duration
	dropletStaleThreshold      time.Duration

	isStateStale func() bool
}

func NewRegistry(c *config.Config) *Registry {
	r := &Registry{}

	r.Logger = steno.NewLogger("router.registry")

	r.ActiveApps = stats.NewActiveApps()
	r.TopApps = stats.NewTopApps()

	r.byUri = make(map[Uri][]*Backend)
	r.byBackendId = make(map[BackendId]*Backend)

	r.staleTracker = util.NewListMap()

	r.pruneStaleDropletsInterval = c.PruneStaleDropletsInterval
	r.dropletStaleThreshold = c.DropletStaleThreshold

	r.isStateStale = func() bool { return false }

	go r.checkAndPrune()

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

func (r *Registry) registerUri(b *Backend, u Uri) {
	u = u.ToLower()

	ok := b.register(u)
	if ok {
		x := r.byUri[u]
		r.byUri[u] = append(x, b)
	}
}

func (r *Registry) Register(m *registryMessage) {
	i, ok := m.BackendId()
	if !ok || len(m.Uris) == 0 {
		return
	}

	r.Lock()
	defer r.Unlock()

	b, ok := r.byBackendId[i]
	if !ok {
		b = newBackend(i, m, r.Logger)
		r.byBackendId[i] = b
	}

	for _, u := range m.Uris {
		r.registerUri(b, u)
	}

	b.t = time.Now()

	r.staleTracker.PushBack(b)
}

func (r *Registry) unregisterUri(b *Backend, u Uri) {
	u = u.ToLower()

	ok := b.unregister(u)
	if ok {
		x := r.byUri[u]
		for i, y := range x {
			if y == b {
				x[i] = x[len(x)-1]
				x = x[:len(x)-1]
				break
			}
		}

		if len(x) == 0 {
			delete(r.byUri, u)
		} else {
			r.byUri[u] = x
		}
	}

	// Remove backend if it no longer has uris
	if len(b.U) == 0 {
		delete(r.byBackendId, b.BackendId)
		r.staleTracker.Delete(b)
	}
}

func (r *Registry) Unregister(m *registryMessage) {
	i, ok := m.BackendId()
	if !ok {
		return
	}

	r.Lock()
	defer r.Unlock()

	b, ok := r.byBackendId[i]
	if !ok {
		return
	}

	for _, u := range m.Uris {
		r.unregisterUri(b, u)
	}
}

func (r *Registry) pruneStaleDroplets() {
	if r.isStateStale() {
		r.resetTracker()
		return
	}

	for r.staleTracker.Len() > 0 {
		b := r.staleTracker.Front().(*Backend)
		if b.t.Add(r.dropletStaleThreshold).After(time.Now()) {
			break
		}

		log.Infof("Pruning stale droplet: %v ", b.BackendId)

		for _, u := range b.U {
			r.unregisterUri(b, u)
		}
	}
}

func (r *Registry) resetTracker() {
	for r.staleTracker.Len() > 0 {
		r.staleTracker.Delete(r.staleTracker.Front().(*Backend))
	}
}

func (r *Registry) PruneStaleDroplets() {
	r.Lock()
	defer r.Unlock()

	r.pruneStaleDroplets()
}

func (r *Registry) checkAndPrune() {
	if r.pruneStaleDropletsInterval == 0 {
		return
	}

	tick := time.Tick(r.pruneStaleDropletsInterval)
	for {
		select {
		case <-tick:
			log.Debug("Start to check and prune stale droplets")
			r.PruneStaleDroplets()
		}
	}
}

func (r *Registry) Lookup(host string) (*Backend, bool) {
	r.RLock()
	defer r.RUnlock()

	x, ok := r.byUri[Uri(host).ToLower()]
	if !ok {
		return nil, false
	}

	// Return random backend from slice of backends for the specified uri
	return x[rand.Intn(len(x))], true
}

func (r *Registry) LookupByPrivateInstanceId(host string, p string) (*Backend, bool) {
	r.RLock()
	defer r.RUnlock()

	x, ok := r.byUri[Uri(host).ToLower()]
	if !ok {
		return nil, false
	}

	for _, b := range x {
		if b.PrivateInstanceId == p {
			return b, true
		}
	}

	return nil, false
}

func (r *Registry) CaptureBackendRequest(x *Backend, t time.Time) {
	if x.ApplicationId != "" {
		r.ActiveApps.Mark(x.ApplicationId, t)
		r.TopApps.Mark(x.ApplicationId, t)
	}
}

func (r *Registry) MarshalJSON() ([]byte, error) {
	r.RLock()
	defer r.RUnlock()

	return json.Marshal(r.byUri)
}
