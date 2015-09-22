package metrics
import (
"net/http"
"github.com/cloudfoundry/gorouter/route"
"time"
)

type ProxyReporter interface {
	CaptureBadRequest(req *http.Request)
	CaptureBadGateway(req *http.Request)
	CaptureRoutingRequest(b *route.Endpoint, req *http.Request)
	CaptureRoutingResponse(b *route.Endpoint, res *http.Response, t time.Time, d time.Duration)
}

type RouteReporter interface {
	CaptureRouteStats(totalRoutes int, msSinceLastUpdate uint64)
}