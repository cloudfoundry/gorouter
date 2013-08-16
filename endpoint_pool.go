package router

import (
	"encoding/json"
	"math/rand"
)

type EndpointPool struct {
	endpoints map[*RouteEndpoint]bool
}

func NewEndpointPool() *EndpointPool {
	return &EndpointPool{
		endpoints: make(map[*RouteEndpoint]bool),
	}
}

func (p *EndpointPool) Add(endpoint *RouteEndpoint) {
	p.endpoints[endpoint] = true
}

func (p *EndpointPool) Remove(endpoint *RouteEndpoint) {
	delete(p.endpoints, endpoint)
}

func (p *EndpointPool) Sample() (*RouteEndpoint, bool) {
	if len(p.endpoints) == 0 {
		return nil, false
	}

	index := rand.Intn(len(p.endpoints))

	ticker := 0
	for endpoint, _ := range p.endpoints {
		if ticker == index {
			return endpoint, true
		}

		ticker += 1
	}

	panic("unreachable")
}

func (p *EndpointPool) FindByPrivateInstanceId(id string) (*RouteEndpoint, bool) {
	for endpoint, _ := range p.endpoints {
		if endpoint.PrivateInstanceId == id {
			return endpoint, true
		}
	}

	return nil, false
}

func (p *EndpointPool) IsEmpty() bool {
	return len(p.endpoints) == 0
}

func (p *EndpointPool) MarshalJSON() ([]byte, error) {
	addresses := []string{}

	for endpoint, _ := range p.endpoints {
		addresses = append(addresses, endpoint.CanonicalAddr())
	}

	return json.Marshal(addresses)
}
