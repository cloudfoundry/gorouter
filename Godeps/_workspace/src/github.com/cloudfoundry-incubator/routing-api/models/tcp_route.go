package models

import "fmt"

type TcpRouteMapping struct {
	TcpRoute
	HostPort        uint16          `json:"backend_port"`
	HostIP          string          `json:"backend_ip"`
	ModificationTag ModificationTag `json:"modification_tag"`
	TTL             uint16          `json:"ttl"`
}

type TcpRoute struct {
	RouterGroupGuid string `json:"router_group_guid"`
	ExternalPort    uint16 `json:"port"`
}

func NewTcpRouteMapping(routerGroupGuid string, externalPort uint16, hostIP string, hostPort uint16, ttl uint16) TcpRouteMapping {
	return TcpRouteMapping{
		TcpRoute: TcpRoute{RouterGroupGuid: routerGroupGuid, ExternalPort: externalPort},
		HostPort: hostPort,
		HostIP:   hostIP,
		TTL:      ttl,
	}
}

func (m TcpRouteMapping) String() string {
	return fmt.Sprintf("%s:%d<->%s:%d", m.RouterGroupGuid, m.ExternalPort, m.HostIP, m.HostPort)
}

func (m TcpRouteMapping) Matches(other TcpRouteMapping) bool {
	return m.RouterGroupGuid == other.RouterGroupGuid &&
		m.ExternalPort == other.ExternalPort &&
		m.HostIP == other.HostIP &&
		m.HostPort == other.HostPort &&
		m.TTL == other.TTL
}
