package round_tripper

import (
	"net/http"

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
