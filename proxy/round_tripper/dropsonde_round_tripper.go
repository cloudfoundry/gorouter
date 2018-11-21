package round_tripper

import (
	"net/http"

	"code.cloudfoundry.org/gorouter/proxy/utils"
	"github.com/cloudfoundry/dropsonde"
)

func NewDropsondeRoundTripper(p ProxyRoundTripper) ProxyRoundTripper {
	return &dropsondeRoundTripper{
		d: dropsonde.InstrumentedRoundTripper(p),
	}
}

type dropsondeRoundTripper struct {
	d http.RoundTripper
}

func (d *dropsondeRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) {
	return d.d.RoundTrip(r)
}

type FactoryImpl struct {
	Template *http.Transport
}

func (t *FactoryImpl) New(expectedServerName string) ProxyRoundTripper {
	customTLSConfig := utils.TLSConfigWithServerName(expectedServerName, t.Template.TLSClientConfig)

	newTransport := &http.Transport{
		Dial:                t.Template.Dial,
		DisableKeepAlives:   t.Template.DisableKeepAlives,
		MaxIdleConns:        t.Template.MaxIdleConns,
		IdleConnTimeout:     t.Template.IdleConnTimeout,
		MaxIdleConnsPerHost: t.Template.MaxIdleConnsPerHost,
		DisableCompression:  t.Template.DisableCompression,
		TLSClientConfig:     customTLSConfig,
	}
	return NewDropsondeRoundTripper(newTransport)
}
