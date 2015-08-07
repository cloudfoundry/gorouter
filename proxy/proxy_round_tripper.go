package proxy

import (
	"net"
	"net/http"

	"github.com/cloudfoundry/gorouter/route"
)

func NewProxyRoundTripper(backend bool, transport http.RoundTripper, endpointIterator route.EndpointIterator,
	handler RequestHandler, afterRoundTrip AfterRoundTrip) http.RoundTripper {
	if backend {
		return &BackendRoundTripper{
			transport: transport,
			iter:      endpointIterator,
			handler:   &handler,
			after:     afterRoundTrip,
		}
	} else {
		return &RouteServiceRoundTripper{
			transport: transport,
			handler:   &handler,
			after:     afterRoundTrip,
		}
	}
}

type BackendRoundTripper struct {
	iter      route.EndpointIterator
	transport http.RoundTripper
	after     AfterRoundTrip
	handler   *RequestHandler
}

func (rt *BackendRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	var err error
	var res *http.Response
	var endpoint *route.Endpoint

	for retry := 0; retry < maxRetries; retry++ {
		endpoint, err = rt.selectEndpoint(request)
		if err != nil {
			return nil, err
		}

		rt.setupRequest(request, endpoint)

		res, err = rt.transport.RoundTrip(request)
		if err == nil || !retryableError(err) {
			break
		}

		rt.reportError(err)
	}

	if rt.after != nil {
		rt.after(res, endpoint, err)
	}

	return res, err
}

func (rt *BackendRoundTripper) selectEndpoint(request *http.Request) (*route.Endpoint, error) {
	endpoint := rt.iter.Next()

	if endpoint == nil {
		rt.handler.reporter.CaptureBadGateway(request)
		err := noEndpointsAvailable
		rt.handler.HandleBadGateway(err)
		return nil, err
	}
	return endpoint, nil
}

func (rt *BackendRoundTripper) setupRequest(request *http.Request, endpoint *route.Endpoint) {
	rt.handler.Logger().Debug("proxy.backend")
	request.URL.Host = endpoint.CanonicalAddr()
	request.Header.Set("X-CF-ApplicationID", endpoint.ApplicationId)
	setRequestXCfInstanceId(request, endpoint)
}

func (rt *BackendRoundTripper) reportError(err error) {
	rt.iter.EndpointFailed()
	rt.handler.Logger().Set("Error", err.Error())
	rt.handler.Logger().Warnf("proxy.endpoint.failed")
}

type RouteServiceRoundTripper struct {
	transport http.RoundTripper
	after     AfterRoundTrip
	handler   *RequestHandler
}

func (rt *RouteServiceRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	var err error
	var res *http.Response

	for retry := 0; retry < maxRetries; retry++ {
		res, err = rt.transport.RoundTrip(request)
		if err == nil || !retryableError(err) {
			break
		}

		rt.reportError(err)
	}

	if rt.after != nil {
		endpoint := newRouteServiceEndpoint()
		rt.after(res, endpoint, err)
	}

	return res, err
}

func (rs *RouteServiceRoundTripper) reportError(err error) {
	rs.handler.Logger().Set("Error", err.Error())
	rs.handler.Logger().Warnf("proxy.route-service.failed")
}

func retryableError(err error) bool {
	ne, netErr := err.(*net.OpError)
	if netErr && ne.Op == "dial" {
		return true
	}

	return false
}
