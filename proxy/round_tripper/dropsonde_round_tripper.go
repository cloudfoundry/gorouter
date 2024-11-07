package round_tripper

import (
	"net/http"

	"github.com/cloudfoundry/dropsonde"

	"code.cloudfoundry.org/gorouter/proxy/utils"
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

func (t *FactoryImpl) New(expectedServerName string, isRouteService bool, isHttp2 bool) ProxyRoundTripper {
	var template *http.Transport
	if isRouteService {
		template = t.RouteServiceTemplate
	} else {
		template = t.BackendTemplate
	}

	customTLSConfig := utils.TLSConfigWithServerName(expectedServerName, template.TLSClientConfig, isRouteService)

	newTransport := &http.Transport{
		DialContext:            template.DialContext,
		DisableKeepAlives:      template.DisableKeepAlives,
		MaxIdleConns:           template.MaxIdleConns,
		IdleConnTimeout:        template.IdleConnTimeout,
		MaxIdleConnsPerHost:    template.MaxIdleConnsPerHost,
		DisableCompression:     template.DisableCompression,
		TLSClientConfig:        customTLSConfig,
		TLSHandshakeTimeout:    template.TLSHandshakeTimeout,
		ForceAttemptHTTP2:      isHttp2,
		ExpectContinueTimeout:  template.ExpectContinueTimeout,
		MaxResponseHeaderBytes: template.MaxResponseHeaderBytes,
	}
	if t.IsInstrumented {
		return NewDropsondeRoundTripper(newTransport)
	} else {
		return newTransport
	}

}
