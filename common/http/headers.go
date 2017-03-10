package http

import "net/http"

const (
	VcapBackendHeader     = "X-Vcap-Backend"
	CfRouteEndpointHeader = "X-Cf-RouteEndpoint"
	VcapRouterHeader      = "X-Vcap-Router"
	VcapTraceHeader       = "X-Vcap-Trace"
	CfInstanceIdHeader    = "X-CF-InstanceID"
	CfAppInstance         = "X-CF-APP-INSTANCE"
	CfRouterError         = "X-Cf-RouterError"
)

func SetTraceHeaders(responseWriter http.ResponseWriter, routerIp, addr string) {
	responseWriter.Header().Set(VcapRouterHeader, routerIp)
	responseWriter.Header().Set(VcapBackendHeader, addr)
	responseWriter.Header().Set(CfRouteEndpointHeader, addr)
}
