package router

import (
	"encoding/json"
	"fmt"
	"sync"
	"time"
)

type RouteEndpoint struct {
	sync.Mutex

	ApplicationId     string
	Host              string
	Port              uint16
	Tags              map[string]string
	PrivateInstanceId string

	Uris       Uris
	updated_at time.Time
}

func (routeEndpoint *RouteEndpoint) MarshalJSON() ([]byte, error) {
	return json.Marshal(routeEndpoint.CanonicalAddr())
}

func newRouteEndpoint(message *registryMessage) *RouteEndpoint {
	b := &RouteEndpoint{
		ApplicationId:     message.App,
		Host:              message.Host,
		Port:              message.Port,
		Tags:              message.Tags,
		PrivateInstanceId: message.PrivateInstanceId,

		Uris:       make([]Uri, 0),
		updated_at: time.Now(),
	}

	return b
}

func (routeEndpoint *RouteEndpoint) CanonicalAddr() string {
	return fmt.Sprintf("%s:%d", routeEndpoint.Host, routeEndpoint.Port)
}

func (routeEndpoint *RouteEndpoint) ToLogData() interface{} {
	return struct {
		ApplicationId string
		Host          string
		Port          uint16
		Tags          map[string]string
	}{
		routeEndpoint.ApplicationId,
		routeEndpoint.Host,
		routeEndpoint.Port,
		routeEndpoint.Tags,
	}
}

func (routeEndpoint *RouteEndpoint) register(uri Uri) bool {
	if !routeEndpoint.Uris.Has(uri) {
		routeEndpoint.Uris = append(routeEndpoint.Uris, uri)
		return true
	}

	return false
}

func (routeEndpoint *RouteEndpoint) unregister(uri Uri) bool {
	remainingUris, ok := routeEndpoint.Uris.Remove(uri)
	if ok {
		routeEndpoint.Uris = remainingUris
	}

	return ok
}
