package varz_test

import (
	"code.cloudfoundry.org/gorouter/config"
	"code.cloudfoundry.org/gorouter/logger"
	"code.cloudfoundry.org/gorouter/metrics/fakes"
	"code.cloudfoundry.org/gorouter/registry"
	"code.cloudfoundry.org/gorouter/route"
	"code.cloudfoundry.org/gorouter/test_util"
	. "code.cloudfoundry.org/gorouter/varz"
	"code.cloudfoundry.org/routing-api/models"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

var _ = Describe("Varz", func() {
	var Varz Varz
	var Registry *registry.RouteRegistry
	var logger logger.Logger

	BeforeEach(func() {
		logger = test_util.NewTestZapLogger("test")
		Registry = registry.NewRouteRegistry(logger, config.DefaultConfig(), new(fakes.FakeRouteRegistryReporter))
		Varz = NewVarz(Registry)
	})

	It("contains the following items", func() {
		v := Varz

		members := []string{
			"responses_2xx",
			"responses_3xx",
			"responses_4xx",
			"responses_5xx",
			"responses_xxx",
			"latency",
			"rate",
			"tags",
			"urls",
			"droplets",
			"requests",
			"bad_requests",
			"bad_gateways",
			"requests_per_sec",
			"top10_app_requests",
			"ms_since_last_registry_update",
		}

		b, e := json.Marshal(v)
		Expect(e).ToNot(HaveOccurred())

		d := make(map[string]interface{})
		e = json.Unmarshal(b, &d)
		Expect(e).ToNot(HaveOccurred())

		for _, k := range members {
			_, ok := d[k]
			Expect(ok).To(BeTrue(), k)
		}
	})

	It("reports seconds since last registry update", func() {
		Registry.Register("foo", &route.Endpoint{})

		time.Sleep(10 * time.Millisecond)

		timeSince := findValue(Varz, "ms_since_last_registry_update").(float64)
		Expect(timeSince).To(BeNumerically("<", 1000))
		Expect(timeSince).To(BeNumerically(">=", 10))
	})

	It("has urls", func() {
		Expect(findValue(Varz, "urls")).To(Equal(float64(0)))

		var fooReg = route.NewEndpoint("12345", "192.168.1.1", 1234, "", "", map[string]string{}, -1, "", models.ModificationTag{}, "", false)

		// Add a route
		Registry.Register("foo.vcap.me", fooReg)
		Registry.Register("fooo.vcap.me", fooReg)

		Expect(findValue(Varz, "urls")).To(Equal(float64(2)))
	})

	It("updates bad requests", func() {
		Varz.CaptureBadRequest()
		Expect(findValue(Varz, "bad_requests")).To(Equal(float64(1)))

		Varz.CaptureBadRequest()
		Expect(findValue(Varz, "bad_requests")).To(Equal(float64(2)))
	})

	It("updates bad gateways", func() {
		Varz.CaptureBadGateway()
		Expect(findValue(Varz, "bad_gateways")).To(Equal(float64(1)))

		Varz.CaptureBadGateway()
		Expect(findValue(Varz, "bad_gateways")).To(Equal(float64(2)))
	})

	It("updates requests", func() {
		b := &route.Endpoint{}

		Varz.CaptureRoutingRequest(b)
		Expect(findValue(Varz, "requests")).To(Equal(float64(1)))

		Varz.CaptureRoutingRequest(b)
		Expect(findValue(Varz, "requests")).To(Equal(float64(2)))
	})

	It("updates requests with tags", func() {
		b1 := &route.Endpoint{
			Tags: map[string]string{
				"component": "cc",
			},
		}

		b2 := &route.Endpoint{
			Tags: map[string]string{
				"component": "cc",
			},
		}

		Varz.CaptureRoutingRequest(b1)
		Varz.CaptureRoutingRequest(b2)

		Expect(findValue(Varz, "tags", "component", "cc", "requests")).To(Equal(float64(2)))
	})

	It("updates responses", func() {
		var b *route.Endpoint = &route.Endpoint{}
		var t time.Time
		var d time.Duration

		r1 := http.StatusOK

		r2 := http.StatusNotFound

		Varz.CaptureRoutingResponseLatency(b, r1, t, d)
		Varz.CaptureRoutingResponseLatency(b, r2, t, d)
		Varz.CaptureRoutingResponseLatency(b, r2, t, d)

		Expect(findValue(Varz, "responses_2xx")).To(Equal(float64(1)))
		Expect(findValue(Varz, "responses_4xx")).To(Equal(float64(2)))
	})

	It("update responses with tags", func() {
		var t time.Time
		var d time.Duration

		b1 := &route.Endpoint{
			Tags: map[string]string{
				"component": "cc",
			},
		}

		b2 := &route.Endpoint{
			Tags: map[string]string{
				"component": "cc",
			},
		}

		r1 := http.StatusOK

		r2 := http.StatusNotFound

		Varz.CaptureRoutingResponseLatency(b1, r1, t, d)
		Varz.CaptureRoutingResponseLatency(b2, r2, t, d)
		Varz.CaptureRoutingResponseLatency(b2, r2, t, d)

		Expect(findValue(Varz, "tags", "component", "cc", "responses_2xx")).To(Equal(float64(1)))
		Expect(findValue(Varz, "tags", "component", "cc", "responses_4xx")).To(Equal(float64(2)))
	})

	It("updates response latency", func() {
		var routeEndpoint *route.Endpoint = &route.Endpoint{}
		var startedAt = time.Now()
		var duration = 1 * time.Millisecond

		statusCode := http.StatusOK

		Varz.CaptureRoutingResponseLatency(routeEndpoint, statusCode, startedAt, duration)

		Expect(findValue(Varz, "latency", "50").(float64)).To(Equal(float64(duration) / float64(time.Second)))
		Expect(findValue(Varz, "latency", "75").(float64)).To(Equal(float64(duration) / float64(time.Second)))
		Expect(findValue(Varz, "latency", "90").(float64)).To(Equal(float64(duration) / float64(time.Second)))
		Expect(findValue(Varz, "latency", "95").(float64)).To(Equal(float64(duration) / float64(time.Second)))
		Expect(findValue(Varz, "latency", "99").(float64)).To(Equal(float64(duration) / float64(time.Second)))
	})
})

// Extract value using key(s) from JSON data
// For example, when extracting value from
//       {
//         "foo": { "bar" : 1 },
//         "foobar": 2,
//        }
// findValue(Varz,"foo", "bar") returns 1
// findValue(Varz,"foobar") returns 2
func findValue(varz Varz, x ...string) interface{} {
	var z interface{}
	var ok bool

	b, err := json.Marshal(varz)
	Expect(err).ToNot(HaveOccurred())

	y := make(map[string]interface{})
	err = json.Unmarshal(b, &y)
	Expect(err).ToNot(HaveOccurred())
	z = y

	for _, e := range x {
		u := z.(map[string]interface{})
		z, ok = u[e]
		Expect(ok).To(BeTrue(), fmt.Sprintf("no key: %s", e))
	}

	return z
}
