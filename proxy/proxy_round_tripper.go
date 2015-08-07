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

type RoundTripper interface {
	BeforeRoundTrip(request *http.Request) (*route.Endpoint, error)
	HandleError(err error)
}

type BackendRoundTripper struct {
	Iter    route.EndpointIterator
	Handler *RequestHandler
}

type RouteServiceRoundTripper struct {
	Handler *RequestHandler
}

func (be *BackendRoundTripper) BeforeRoundTrip(request *http.Request) (*route.Endpoint, error) {
	endpoint := be.Iter.Next()

	if endpoint == nil {
		be.Handler.reporter.CaptureBadGateway(request)
		err := noEndpointsAvailable
		be.Handler.HandleBadGateway(err)
		return nil, err
	}
	be.setupBackendRequest(request, endpoint)

	return endpoint, nil
}

func (be *BackendRoundTripper) HandleError(err error) {
	be.Iter.EndpointFailed()
	be.Handler.Logger().Set("Error", err.Error())
	be.Handler.Logger().Warnf("proxy.endpoint.failed")
}

func (rs *RouteServiceRoundTripper) BeforeRoundTrip(request *http.Request) (*route.Endpoint, error) {
	return newRouteServiceEndpoint(), nil
}

func (rs *RouteServiceRoundTripper) HandleError(err error) {
	rs.Handler.Logger().Set("Error", err.Error())
	rs.Handler.Logger().Warnf("proxy.route-service.failed")
}

func (be *BackendRoundTripper) setupBackendRequest(request *http.Request, endpoint *route.Endpoint) {
	be.Handler.Logger().Debug("proxy.backend")

	request.URL.Host = endpoint.CanonicalAddr()
	request.Header.Set("X-CF-ApplicationID", endpoint.ApplicationId)
	setRequestXCfInstanceId(request, endpoint)
}

func (p *ProxyRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	var err error
	var res *http.Response
	var endpoint *route.Endpoint

	var rt RoundTripper

	if p.ServingBackend {
		rt = &BackendRoundTripper{
			Iter:    p.Iter,
			Handler: p.Handler,
		}
	} else {
		rt = &RouteServiceRoundTripper{
			Handler: p.Handler,
		}
	}

	retry := 0
	for {
		endpoint, err = rt.BeforeRoundTrip(request)
		if err != nil {
			return nil, err
		}

		res, err = p.Transport.RoundTrip(request)

		if err == nil {
			break
		}
		if ne, netErr := err.(*net.OpError); !netErr || ne.Op != "dial" {
			break
		}

		rt.HandleError(err)

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
