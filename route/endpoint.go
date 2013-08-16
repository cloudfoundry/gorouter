package route

import (
	"encoding/json"
	"fmt"
	"sync"
	"time"
)

type Endpoint struct {
	sync.Mutex

	ApplicationId     string
	Host              string
	Port              uint16
	Tags              map[string]string
	PrivateInstanceId string

	Uris Uris

	UpdatedAtFORNOW time.Time
}

func (e *Endpoint) MarshalJSON() ([]byte, error) {
	return json.Marshal(e.CanonicalAddr())
}

func (e *Endpoint) CanonicalAddr() string {
	return fmt.Sprintf("%s:%d", e.Host, e.Port)
}

func (e *Endpoint) ToLogData() interface{} {
	return struct {
		ApplicationId string
		Host          string
		Port          uint16
		Tags          map[string]string
	}{
		e.ApplicationId,
		e.Host,
		e.Port,
		e.Tags,
	}
}

func (e *Endpoint) Register(uri Uri) bool {
	if !e.Uris.Has(uri) {
		e.Uris = append(e.Uris, uri)
		return true
	}

	return false
}

func (e *Endpoint) Unregister(uri Uri) bool {
	remainingUris, ok := e.Uris.Remove(uri)
	if ok {
		e.Uris = remainingUris
	}

	return ok
}
