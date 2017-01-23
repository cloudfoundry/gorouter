package metrics

import (
	"net/http"
	"time"

	"code.cloudfoundry.org/gorouter/metrics/reporter"
	"code.cloudfoundry.org/gorouter/route"
)

type CompositeReporter struct {
	first  reporter.ProxyReporter
	second reporter.ProxyReporter
}

func NewCompositeReporter(first, second reporter.ProxyReporter) reporter.ProxyReporter {
	return &CompositeReporter{
		first:  first,
		second: second,
	}
}

func (c *CompositeReporter) CaptureBadRequest() {
	c.first.CaptureBadRequest()
	c.second.CaptureBadRequest()
}

func (c *CompositeReporter) CaptureBadGateway() {
	c.first.CaptureBadGateway()
	c.second.CaptureBadGateway()
}

func (c *CompositeReporter) CaptureRoutingRequest(b *route.Endpoint) {
	c.first.CaptureRoutingRequest(b)
	c.second.CaptureRoutingRequest(b)
}

func (c *CompositeReporter) CaptureRouteServiceResponse(res *http.Response) {
	c.first.CaptureRouteServiceResponse(res)
	c.second.CaptureRouteServiceResponse(res)
}

func (c *CompositeReporter) CaptureRoutingResponse(b *route.Endpoint, res *http.Response, t time.Time, d time.Duration) {
	c.first.CaptureRoutingResponse(b, res, t, d)
	c.second.CaptureRoutingResponse(b, res, t, d)
}
