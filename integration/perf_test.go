package integration

import (
	"crypto/tls"
	"fmt"
	"strconv"

	"code.cloudfoundry.org/gorouter/common/health"

	"code.cloudfoundry.org/gorouter/accesslog"
	"code.cloudfoundry.org/gorouter/config"
	"code.cloudfoundry.org/gorouter/errorwriter"
	"code.cloudfoundry.org/gorouter/metrics"
	"code.cloudfoundry.org/gorouter/proxy"
	"code.cloudfoundry.org/gorouter/registry"
	"code.cloudfoundry.org/gorouter/route"
	"code.cloudfoundry.org/gorouter/router"
	"code.cloudfoundry.org/gorouter/routeservice"
	"code.cloudfoundry.org/gorouter/test_util"
	"code.cloudfoundry.org/gorouter/varz"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	schemaFakes "code.cloudfoundry.org/gorouter/accesslog/schema/fakes"
	"code.cloudfoundry.org/gorouter/metrics/fakes"
)

var _ = Describe("AccessLogRecord", func() {
	Measure("Register", func(b Benchmarker) {
		sender := new(fakes.MetricSender)
		batcher := new(fakes.MetricBatcher)
		metricsReporter := &metrics.MetricsReporter{Sender: sender, Batcher: batcher}
		ls := &schemaFakes.FakeLogSender{}
		logger := test_util.NewTestZapLogger("test")
		c, err := config.DefaultConfig()
		Expect(err).ToNot(HaveOccurred())
		r := registry.NewRouteRegistry(logger, c, new(fakes.FakeRouteRegistryReporter))
		combinedReporter := &metrics.CompositeReporter{VarzReporter: varz.NewVarz(r), ProxyReporter: metricsReporter}
		accesslog, err := accesslog.CreateRunningAccessLogger(logger, ls, c)
		Expect(err).ToNot(HaveOccurred())

		ew := errorwriter.NewPlaintextErrorWriter()

		rss, err := router.NewRouteServicesServer()
		Expect(err).ToNot(HaveOccurred())
		var h *health.Health
		proxy.NewProxy(logger, accesslog, nil, ew, c, r, combinedReporter, &routeservice.RouteServiceConfig{},
			&tls.Config{}, &tls.Config{}, h, rss.GetRoundTripper())

		b.Time("RegisterTime", func() {
			for i := 0; i < 1000; i++ {
				str := strconv.Itoa(i)
				r.Register(
					route.Uri(fmt.Sprintf("bench.%s.%s", test_util.LocalhostDNS, str)),
					route.NewEndpoint(&route.EndpointOpts{
						Host:                    "localhost",
						Port:                    uint16(i),
						StaleThresholdInSeconds: -1,
						UseTLS:                  false,
					}),
				)
			}
		})
	}, 10)
})
