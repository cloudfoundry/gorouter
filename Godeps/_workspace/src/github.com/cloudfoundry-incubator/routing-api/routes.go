package routing_api

import "github.com/tedsuo/rata"

const (
	UpsertRoute           = "UpsertRoute"
	DeleteRoute           = "Delete"
	ListRoute             = "List"
	EventStreamRoute      = "EventStream"
	ListRouterGroups      = "ListRouterGroups"
	UpsertTcpRouteMapping = "UpsertTcpRouteMapping"
	DeleteTcpRouteMapping = "DeleteTcpRouteMapping"
	ListTcpRouteMapping   = "ListTcpRouteMapping"
	EventStreamTcpRoute   = "TcpRouteEventStream"
)

var Routes = rata.Routes{
	{Path: "/routing/v1/routes", Method: "POST", Name: UpsertRoute},
	{Path: "/routing/v1/routes", Method: "DELETE", Name: DeleteRoute},
	{Path: "/routing/v1/routes", Method: "GET", Name: ListRoute},
	{Path: "/routing/v1/events", Method: "GET", Name: EventStreamRoute},
	{Path: "/routing/v1/router_groups", Method: "GET", Name: ListRouterGroups},

	{Path: "/routing/v1/tcp_routes/create", Method: "POST", Name: UpsertTcpRouteMapping},
	{Path: "/routing/v1/tcp_routes/delete", Method: "POST", Name: DeleteTcpRouteMapping},
	{Path: "/routing/v1/tcp_routes", Method: "GET", Name: ListTcpRouteMapping},
	{Path: "/routing/v1/tcp_routes/events", Method: "GET", Name: EventStreamTcpRoute},
}
