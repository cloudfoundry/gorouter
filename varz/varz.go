package varz

import (
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"code.cloudfoundry.org/gorouter/registry"
	"code.cloudfoundry.org/gorouter/route"
	"code.cloudfoundry.org/gorouter/stats"
	metrics "github.com/rcrowley/go-metrics"
)

type topAppsEntry struct {
	ApplicationId     string `json:"application_id"`
	RequestsPerSecond int64  `json:"rps"`
	RequestsPerMinute int64  `json:"rpm"`
}

type varz struct {
	All  *HttpMetric `json:"all"`
	Tags struct {
		Component TaggedHttpMetric `json:"component"`
	} `json:"tags"`

	Urls     int `json:"urls"`
	Droplets int `json:"droplets"`

	BadRequests    int     `json:"bad_requests"`
	BadGateways    int     `json:"bad_gateways"`
	RequestsPerSec float64 `json:"requests_per_sec"`

	TopApps []topAppsEntry `json:"top10_app_requests"`

	MillisSinceLastRegistryUpdate int64 `json:"ms_since_last_registry_update"`
}

type httpMetric struct {
	Requests int64      `json:"requests"`
	Rate     [3]float64 `json:"rate"`

	Responses2xx int64              `json:"responses_2xx"`
	Responses3xx int64              `json:"responses_3xx"`
	Responses4xx int64              `json:"responses_4xx"`
	Responses5xx int64              `json:"responses_5xx"`
	ResponsesXxx int64              `json:"responses_xxx"`
	Latency      map[string]float64 `json:"latency"`
}

type HttpMetric struct {
	Requests metrics.Counter
	Rate     metrics.Meter

	Responses2xx metrics.Counter
	Responses3xx metrics.Counter
	Responses4xx metrics.Counter
	Responses5xx metrics.Counter
	ResponsesXxx metrics.Counter
	Latency      metrics.Histogram
}

func NewHttpMetric() *HttpMetric {
	x := &HttpMetric{
		Requests: metrics.NewCounter(),
		Rate:     metrics.NewMeter(),

		Responses2xx: metrics.NewCounter(),
		Responses3xx: metrics.NewCounter(),
		Responses4xx: metrics.NewCounter(),
		Responses5xx: metrics.NewCounter(),
		ResponsesXxx: metrics.NewCounter(),
		Latency:      metrics.NewHistogram(metrics.NewExpDecaySample(1028, 0.015)),
	}
	return x
}

func (x *HttpMetric) MarshalJSON() ([]byte, error) {
	y := httpMetric{}

	y.Requests = x.Requests.Count()
	y.Rate[0] = x.Rate.Rate1()
	y.Rate[1] = x.Rate.Rate5()
	y.Rate[2] = x.Rate.Rate15()

	y.Responses2xx = x.Responses2xx.Count()
	y.Responses3xx = x.Responses3xx.Count()
	y.Responses4xx = x.Responses4xx.Count()
	y.Responses5xx = x.Responses5xx.Count()
	y.ResponsesXxx = x.ResponsesXxx.Count()

	p := []float64{0.50, 0.75, 0.90, 0.95, 0.99}
	z := x.Latency.Percentiles(p)

	y.Latency = make(map[string]float64)
	for i, e := range p {
		y.Latency[fmt.Sprintf("%d", int(e*100))] = z[i] / float64(time.Second)
	}

	// Add fields for backwards compatibility with the collector
	y.Latency["value"] = p[0] / float64(time.Millisecond)
	y.Latency["samples"] = 1

	return json.Marshal(y)
}

func (x *HttpMetric) CaptureRequest() {
	x.Requests.Inc(1)
	x.Rate.Mark(1)
}

func (x *HttpMetric) CaptureResponse(statusCode int, duration time.Duration) {
	statusCode = statusCode / 100

	switch statusCode {
	case 2:
		x.Responses2xx.Inc(1)
	case 3:
		x.Responses3xx.Inc(1)
	case 4:
		x.Responses4xx.Inc(1)
	case 5:
		x.Responses5xx.Inc(1)
	default:
		x.ResponsesXxx.Inc(1)
	}

	x.Latency.Update(duration.Nanoseconds())
}

type TaggedHttpMetric map[string]*HttpMetric

func (x TaggedHttpMetric) httpMetric(t string) *HttpMetric {
	y := x[t]
	if y == nil {
		y = NewHttpMetric()
		x[t] = y
	}

	return y
}

func (x TaggedHttpMetric) CaptureRequest(t string) {
	x.httpMetric(t).CaptureRequest()
}

func (x TaggedHttpMetric) CaptureResponse(t string, statusCode int, z time.Duration) {
	x.httpMetric(t).CaptureResponse(statusCode, z)
}

type Varz interface {
	json.Marshaler

	ActiveApps() *stats.ActiveApps

	CaptureBadRequest()
	CaptureBadGateway()
	CaptureRoutingRequest(b *route.Endpoint)
	CaptureRoutingResponseLatency(b *route.Endpoint, statusCode int, startedAt time.Time, d time.Duration)
}

type RealVarz struct {
	sync.Mutex
	r          *registry.RouteRegistry
	activeApps *stats.ActiveApps
	topApps    *stats.TopApps
	varz
}

func NewVarz(r *registry.RouteRegistry) Varz {
	x := &RealVarz{r: r}

	x.activeApps = stats.NewActiveApps()
	x.topApps = stats.NewTopApps()

	x.All = NewHttpMetric()
	x.Tags.Component = make(map[string]*HttpMetric)

	return x
}

func (x *RealVarz) MarshalJSON() ([]byte, error) {
	x.Lock()
	defer x.Unlock()

	x.varz.Urls = x.r.NumUris()
	x.varz.Droplets = x.r.NumEndpoints()

	x.varz.RequestsPerSec = x.varz.All.Rate.Rate1()
	millis_per_nano := int64(1000000)
	x.varz.MillisSinceLastRegistryUpdate = time.Since(x.r.TimeOfLastUpdate()).Nanoseconds() / millis_per_nano

	x.updateTop()

	d := make(map[string]interface{})
	err := transform(x.varz.All, d)
	if err != nil {
		return []byte{}, err
	}
	err = transform(x.varz, d)
	if err != nil {
		return []byte{}, err
	}
	delete(d, "all")

	return json.Marshal(d)
}

func (x *RealVarz) updateTop() {
	t := time.Now().Add(-1 * time.Minute)
	y := x.topApps.TopSince(t, 10)

	x.varz.TopApps = make([]topAppsEntry, 0)
	for _, z := range y {
		x.varz.TopApps = append(x.varz.TopApps, topAppsEntry{
			ApplicationId:     z.ApplicationId,
			RequestsPerSecond: z.Requests / int64(stats.TopAppsEntryLifetime.Seconds()),
			RequestsPerMinute: z.Requests,
		})
	}
}

func (x *RealVarz) ActiveApps() *stats.ActiveApps {
	return x.activeApps
}

func (x *RealVarz) CaptureBadRequest() {
	x.Lock()
	x.BadRequests++
	x.Unlock()
}

func (x *RealVarz) CaptureBadGateway() {
	x.Lock()
	x.BadGateways++
	x.Unlock()
}

func (x *RealVarz) CaptureAppStats(b *route.Endpoint, t time.Time) {
	if b.ApplicationId != "" {
		x.activeApps.Mark(b.ApplicationId, t)
		x.topApps.Mark(b.ApplicationId, t)
	}
}

func (x *RealVarz) CaptureRoutingRequest(b *route.Endpoint) {
	x.Lock()

	var t string
	var ok bool

	t, ok = b.Tags["component"]
	if ok {
		x.varz.Tags.Component.CaptureRequest(t)
	}

	x.varz.All.CaptureRequest()

	x.Unlock()
}

func (x *RealVarz) CaptureRoutingResponseLatency(endpoint *route.Endpoint, statusCode int, startedAt time.Time, duration time.Duration) {
	x.Lock()

	var tags string
	var ok bool

	tags, ok = endpoint.Tags["component"]
	if ok {
		x.varz.Tags.Component.CaptureResponse(tags, statusCode, duration)
	}

	x.CaptureAppStats(endpoint, startedAt)
	x.varz.All.CaptureResponse(statusCode, duration)

	x.Unlock()
}

func transform(x interface{}, y map[string]interface{}) error {
	var b []byte
	var err error

	b, err = json.Marshal(x)
	if err != nil {
		return err
	}

	err = json.Unmarshal(b, &y)
	if err != nil {
		return err
	}

	return nil
}
