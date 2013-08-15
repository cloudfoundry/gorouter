package router

import (
	"encoding/json"
	"fmt"
	steno "github.com/cloudfoundry/gosteno"
	"sync"
	"time"
)

type RouteEndpointId string

type RouteEndpoint struct {
	sync.Mutex

	*steno.Logger

	RouteEndpointId RouteEndpointId

	ApplicationId     string
	Host              string
	Port              uint16
	Tags              map[string]string
	PrivateInstanceId string

	U          Uris
	updated_at time.Time
}

func (routeEndpoint *RouteEndpoint) MarshalJSON() ([]byte, error) {
	return json.Marshal(routeEndpoint.CanonicalAddr())
}

func newRouteEndpoint(routeEndpointId RouteEndpointId, message *registryMessage, logger *steno.Logger) *RouteEndpoint {
	b := &RouteEndpoint{
		Logger: logger,

		RouteEndpointId: routeEndpointId,

		ApplicationId:     message.App,
		Host:              message.Host,
		Port:              message.Port,
		Tags:              message.Tags,
		PrivateInstanceId: message.PrivateInstanceId,

		U:          make([]Uri, 0),
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
	if !routeEndpoint.U.Has(uri) {
		routeEndpoint.Infof("Register %s (%s)", uri, routeEndpoint.RouteEndpointId)
		routeEndpoint.U = append(routeEndpoint.U, uri)
		return true
	}

	return false
}

func (routeEndpoint *RouteEndpoint) unregister(uri Uri) bool {
	remainingUris, ok := routeEndpoint.U.Remove(uri)
	if ok {
		routeEndpoint.Infof("Unregister %s (%s)", uri, routeEndpoint.RouteEndpointId)
		routeEndpoint.U = remainingUris
	}

	return ok
}
