package proxy

import (
	"net"
	"net/http"

	"github.com/cloudfoundry/gorouter/route"
)

type ProxyRoundTripper struct {
	Transport      http.RoundTripper
	After          AfterRoundTrip
	Iter           route.EndpointIterator
	Handler        *RequestHandler
	ServingBackend bool
}

type RoundTripEventHandler interface {
	SelectEndpoint(request *http.Request) (*route.Endpoint, error)
	PreprocessRequest(request *http.Request, endpoint *route.Endpoint)
	HandleError(err error)
}

type BackendRoundTripper struct {
	Iter    route.EndpointIterator
	Handler *RequestHandler
}

type RouteServiceRoundTripper struct {
	Handler *RequestHandler
}

func (be *BackendRoundTripper) SelectEndpoint(request *http.Request) (*route.Endpoint, error) {
	endpoint := be.Iter.Next()

	if endpoint == nil {
		be.Handler.reporter.CaptureBadGateway(request)
		err := noEndpointsAvailable
		be.Handler.HandleBadGateway(err)
		return nil, err
	}
	return endpoint, nil
}

func (be *BackendRoundTripper) HandleError(err error) {
	be.Iter.EndpointFailed()
	be.Handler.Logger().Set("Error", err.Error())
	be.Handler.Logger().Warnf("proxy.endpoint.failed")
}

func (rs *RouteServiceRoundTripper) SelectEndpoint(request *http.Request) (*route.Endpoint, error) {
	return newRouteServiceEndpoint(), nil
}
func (be *RouteServiceRoundTripper) PreprocessRequest(request *http.Request, endpoint *route.Endpoint) {
}

func (rs *RouteServiceRoundTripper) HandleError(err error) {
	rs.Handler.Logger().Set("Error", err.Error())
	rs.Handler.Logger().Warnf("proxy.route-service.failed")
}

func (be *BackendRoundTripper) PreprocessRequest(request *http.Request, endpoint *route.Endpoint) {
	be.Handler.Logger().Debug("proxy.backend")

	request.URL.Host = endpoint.CanonicalAddr()
	request.Header.Set("X-CF-ApplicationID", endpoint.ApplicationId)
	setRequestXCfInstanceId(request, endpoint)
}

func (p *ProxyRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	var err error
	var res *http.Response
	var endpoint *route.Endpoint

	handler := p.eventHandler()

	retry := 0
	for {
		endpoint, err = handler.SelectEndpoint(request)
		if err != nil {
			return nil, err
		}

		handler.PreprocessRequest(request, endpoint)

		res, err = p.Transport.RoundTrip(request)
		if err == nil {
			break
		}
		if ne, netErr := err.(*net.OpError); !netErr || ne.Op != "dial" {
			break
		}

		handler.HandleError(err)

		retry++
		if retry == retries {
			break
		}
	}

	if p.After != nil {
		p.After(res, endpoint, err)
	}

	return res, err
}

func (p *ProxyRoundTripper) eventHandler() RoundTripEventHandler {
	var r RoundTripEventHandler
	if p.ServingBackend {
		r = &BackendRoundTripper{
			Iter:    p.Iter,
			Handler: p.Handler,
		}
	} else {
		r = &RouteServiceRoundTripper{
			Handler: p.Handler,
		}
	}
	return r
}
