package proxy

import (
	"errors"
	"net/http"

	router_http "code.cloudfoundry.org/gorouter/common/http"
	"code.cloudfoundry.org/gorouter/handlers"
)

func (p *proxy) modifyResponse(res *http.Response) error {
	req := res.Request
	if req == nil {
		return errors.New("Response does not have an attached request")
	}
	if res.Header.Get(handlers.VcapRequestIdHeader) == "" {
		vcapID := req.Header.Get(handlers.VcapRequestIdHeader)
		res.Header.Set(handlers.VcapRequestIdHeader, vcapID)
	}

	reqInfo, err := handlers.ContextRequestInfo(req)
	if err != nil {
		return err
	}
	endpoint := reqInfo.RouteEndpoint
	if endpoint == nil {
		return errors.New("reqInfo.RouteEndpoint is empty on a successful response")
	}
	routePool := reqInfo.RoutePool
	if routePool == nil {
		return errors.New("reqInfo.RoutePool is empty on a successful response")
	}

	if p.config.TraceKey != "" && req.Header.Get(router_http.VcapTraceHeader) == p.config.TraceKey {
		res.Header.Set(router_http.VcapRouterHeader, p.config.Ip)
		res.Header.Set(router_http.VcapBackendHeader, endpoint.CanonicalAddr())
		res.Header.Set(router_http.CfRouteEndpointHeader, endpoint.CanonicalAddr())
	}

	return nil
}
