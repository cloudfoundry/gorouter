package router

import (
	"fmt"
	"net/http"
	"strings"
	"sync"
)

type registerMessage struct {
	Host string            `json:"host"`
	Port uint16            `json:"port"`
	Uris []string          `json:"uris"`
	Tags map[string]string `json:"tags"`
	Dea  string            `json:"dea"`
	App  string            `json:"app"`

	Sticky string
}

func (m *registerMessage) HostPort() string {
	return fmt.Sprintf("%s:%d", m.Host, m.Port)
}

type Registry struct {
	sync.RWMutex

	varz *Varz

	r map[string][]*registerMessage
	d map[string]int
}

func NewRegistry() *Registry {
	r := &Registry{}

	r.r = make(map[string][]*registerMessage)
	r.d = make(map[string]int)

	return r
}

func (r *Registry) NumUris() int {
	r.RLock()
	defer r.RUnlock()

	return len(r.r)
}

func (r *Registry) NumDroplets() int {
	r.RLock()
	defer r.RUnlock()

	return len(r.d)
}

func (r *Registry) registerUri(m *registerMessage, uri string) {
	s := r.r[uri]
	if s == nil {
		s = make([]*registerMessage, 0)

		if r.varz != nil {
			r.varz.RegisterApp(uri)
		}
	}

	exist := false
	for _, d := range s {
		if d.Host == m.Host && d.Port == m.Port {
			exist = true
			break
		}
	}

	if !exist {
		s = append(s, m)
		r.r[uri] = s
		hp := m.HostPort()
		r.d[hp]++
	}
}

func (r *Registry) Register(m *registerMessage) {
	r.Lock()
	defer r.Unlock()

	// Store droplet in registry
	for _, uri := range m.Uris {
		uri = strings.ToLower(uri)
		r.registerUri(m, uri)
	}

	if r.varz != nil {
		r.varz.Urls = len(r.r)
		r.varz.Droplets = len(r.d)
	}
}

func (r *Registry) unregisterUri(m *registerMessage, uri string) {
	s := r.r[uri]
	if s == nil {
		return
	}

	exist := false
	for i, d := range s {
		if d.Host == m.Host && d.Port == m.Port {
			s[i] = s[len(s)-1]
			s = s[:len(s)-1]
			exist = true
			break
		}
	}

	if exist {
		if len(s) == 0 {
			delete(r.r, uri)

			if r.varz != nil {
				r.varz.UnregisterApp(uri)
			}
		} else {
			r.r[uri] = s
		}

		hp := m.HostPort()
		r.d[hp]--
		if r.d[hp] == 0 {
			delete(r.d, hp)
		}
	}
}

func (r *Registry) Unregister(m *registerMessage) {
	r.Lock()
	defer r.Unlock()

	// Delete droplets from registry
	for _, uri := range m.Uris {
		uri = strings.ToLower(uri)
		r.unregisterUri(m, uri)
	}

	if r.varz != nil {
		r.varz.Urls = len(r.r)
		r.varz.Droplets = len(r.d)
	}
}

func (r *Registry) Lookup(req *http.Request) []*registerMessage {
	host := req.Host

	// Remove :<port>
	i := strings.Index(host, ":")
	if i >= 0 {
		host = host[0:i]
	}

	host = strings.ToLower(host)

	r.RLock()
	defer r.RUnlock()

	return r.r[host]
}
