package router

import (
	"container/list"
	"fmt"
	steno "github.com/cloudfoundry/gosteno"
	"math/rand"
	"router/config"
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
	sync.Mutex

	steno.Logger

	BackendId BackendId

	ApplicationId     string
	Host              string
	Port              uint16
	Tags              map[string]string
	PrivateInstanceId string

	U Uris
	t time.Time
}

func newBackend(i BackendId, m *registryMessage, l steno.Logger) *Backend {
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

func (b *Backend) register(u Uri) bool {
	b.Debugf("Register %s (%s)", u, b.BackendId)

	if !b.U.Has(u) {
		b.U = append(b.U, u)
		return true
	}

	return false
}

func (b *Backend) unregister(u Uri) bool {
	b.Debugf("Unregister %s (%s)", u, b.BackendId)

	x, ok := b.U.Remove(u)
	if ok {
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
	Dea  string            `json:"dea"`
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

	steno.Logger

	*stats.ActiveApps
	*stats.TopApps

	byUri       map[Uri][]*Backend
	byBackendId map[BackendId]*Backend

	tracker        *list.List
	trackerIndexes map[BackendId]*list.Element

	pruneStaleDropletsInterval time.Duration
	dropletStaleThreshold      time.Duration
}

func NewRegistry(c *config.Config) *Registry {
	r := &Registry{}

	r.Logger = steno.NewLogger("registry")

	r.ActiveApps = stats.NewActiveApps()
	r.TopApps = stats.NewTopApps()

	r.byUri = make(map[Uri][]*Backend)
	r.byBackendId = make(map[BackendId]*Backend)

	r.tracker = list.New()
	r.trackerIndexes = make(map[BackendId]*list.Element)

	r.pruneStaleDropletsInterval = c.PruneStaleDropletsInterval
	r.dropletStaleThreshold = c.DropletStaleThreshold

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
	if !ok {
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

	r.updateInTracker(b)
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
		r.removeFromTracker(b)
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

func (r *Registry) updateInTracker(b *Backend) {
	n := r.trackerIndexes[b.BackendId]
	if n != nil {
		r.tracker.Remove(n)
	}

	b.t = time.Now()
	e := r.tracker.PushBack(b)
	r.trackerIndexes[b.BackendId] = e
}

func (r *Registry) removeFromTracker(b *Backend) {
	if n := r.trackerIndexes[b.BackendId]; n != nil {
		r.tracker.Remove(n)
	}

	delete(r.trackerIndexes, b.BackendId)
}

func (r *Registry) pruneStaleDroplets() {
	for r.tracker.Len() > 0 {
		f := r.tracker.Front()
		b := f.Value.(*Backend)
		if b.t.Add(r.dropletStaleThreshold).After(time.Now()) {
			break
		}

		for _, u := range b.U {
			r.unregisterUri(b, u)
		}

		log.Infof("Pruned stale droplet: %v ", b.BackendId)
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
			log.Info("Start to check and prune stale droplets")
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
