package round_tripper

import (
	"net/http"

	"code.cloudfoundry.org/gorouter/proxy/utils"
	"github.com/cloudfoundry/dropsonde"
	"golang.org/x/net/http2"
)

func NewDropsondeRoundTripper(p ProxyRoundTripper) ProxyRoundTripper {
	return &dropsondeRoundTripper{
		p: p,
		d: dropsonde.InstrumentedRoundTripper(p),
	}
}

type dropsondeRoundTripper struct {
	p ProxyRoundTripper
	d http.RoundTripper
}

func (d *dropsondeRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) {
	return d.d.RoundTrip(r)
}

func (d *dropsondeRoundTripper) CancelRequest(r *http.Request) {
	d.p.CancelRequest(r)
}

type FactoryImpl struct {
	BackendTemplate      *http.Transport
	Http2BackendTemplate *http2.Transport
	RouteServiceTemplate *http.Transport
	IsInstrumented       bool
}

// type CancellableHttp2Transport struct {
// 	t *http2.Transport
// }

// func (cht CancellableHttp2Transport) CancelRequest(r *http.Request) {}

// func (cht CancellableHttp2Transport) RoundTrip(r *http.Request) (*http.Response, error) {
// 	return cht.t.RoundTrip(r)
// }

func (t *FactoryImpl) New(expectedServerName string, isRouteService, isHttp2 bool) ProxyRoundTripper {

	// if isHttp2 {
	// cht := CancellableHttp2Transport{t: t.Http2BackendTemplate}
	// return cht
	// }

	var template *http.Transport
	if isRouteService {
		template = t.RouteServiceTemplate
	} else {
		template = t.BackendTemplate
	}

	customTLSConfig := utils.TLSConfigWithServerName(expectedServerName, template.TLSClientConfig)

	newTransport := &http.Transport{
		Dial:                template.Dial,
		DisableKeepAlives:   template.DisableKeepAlives,
		MaxIdleConns:        template.MaxIdleConns,
		IdleConnTimeout:     template.IdleConnTimeout,
		MaxIdleConnsPerHost: template.MaxIdleConnsPerHost,
		DisableCompression:  template.DisableCompression,
		TLSClientConfig:     customTLSConfig,
		TLSHandshakeTimeout: template.TLSHandshakeTimeout,
		ForceAttemptHTTP2:   isHttp2,
	}
	if t.IsInstrumented {
		return NewDropsondeRoundTripper(newTransport)
	} else {
		return newTransport
	}

}
