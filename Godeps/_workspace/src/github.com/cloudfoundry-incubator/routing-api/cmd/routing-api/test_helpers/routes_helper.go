package test_helpers

import "github.com/cloudfoundry-incubator/routing-api/models"

type Routes []models.Route

func (rs Routes) ContainsAll(routes ...models.Route) bool {
	for _, r := range routes {
		if !rs.Contains(r) {
			return false
		}
	}
	return true
}

func (rs Routes) Contains(route models.Route) bool {
	for _, r := range rs {
		if r.Matches(route) {
			return true
		}
	}
	return false
}

type TcpRouteMappings []models.TcpRouteMapping

func (ms TcpRouteMappings) ContainsAll(tcpRouteMappings ...models.TcpRouteMapping) bool {
	for _, m := range tcpRouteMappings {
		if !ms.Contains(m) {
			return false
		}
	}
	return true
}

func (ms TcpRouteMappings) Contains(tcpRouteMapping models.TcpRouteMapping) bool {
	for _, m := range ms {
		if m.Matches(tcpRouteMapping) {
			return true
		}
	}
	return false
}
