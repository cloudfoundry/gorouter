package round_tripper

import (
	"net/http"

	"code.cloudfoundry.org/gorouter/proxy/utils"
	"github.com/cloudfoundry/dropsonde"
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
	RouteServiceTemplate *http.Transport
	IsInstrumented       bool
}

func (t *FactoryImpl) New(expectedServerName string, isRouteService bool) ProxyRoundTripper {
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
	}
	if t.IsInstrumented {
		return NewDropsondeRoundTripper(newTransport)
	} else {
		return newTransport
	}

}
