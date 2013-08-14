package router

import (
	"encoding/json"
	"fmt"
	steno "github.com/cloudfoundry/gosteno"
	"sync"
	"time"
)

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

	U          Uris
	updated_at time.Time
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

		U:          make([]Uri, 0),
		updated_at: time.Now(),
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
