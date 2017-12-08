package main_test

import (
	"crypto/tls"
	"strconv"

	"code.cloudfoundry.org/gorouter/access_log"
	"code.cloudfoundry.org/gorouter/config"
	"code.cloudfoundry.org/gorouter/metrics"
	"code.cloudfoundry.org/gorouter/proxy"
	"code.cloudfoundry.org/gorouter/registry"
	"code.cloudfoundry.org/gorouter/route"
	"code.cloudfoundry.org/gorouter/routeservice"
	"code.cloudfoundry.org/gorouter/test_util"
	"code.cloudfoundry.org/gorouter/varz"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	"code.cloudfoundry.org/gorouter/metrics/fakes"
)

var _ = Describe("AccessLogRecord", func() {
	Measure("Register", func(b Benchmarker) {
		sender := new(fakes.MetricSender)
		batcher := new(fakes.MetricBatcher)
		metricsReporter := &metrics.MetricsReporter{Sender: sender, Batcher: batcher}
		logger := test_util.NewTestZapLogger("test")
		c, err := config.DefaultConfig()
		Expect(err).ToNot(HaveOccurred())
		r := registry.NewRouteRegistry(logger, c, new(fakes.FakeRouteRegistryReporter))
		combinedReporter := &metrics.CompositeReporter{VarzReporter: varz.NewVarz(r), ProxyReporter: metricsReporter}
		accesslog, err := access_log.CreateRunningAccessLogger(logger, c)
		Expect(err).ToNot(HaveOccurred())

		proxy.NewProxy(logger, accesslog, c, r, combinedReporter, &routeservice.RouteServiceConfig{},
			&tls.Config{}, nil)

		b.Time("RegisterTime", func() {
			for i := 0; i < 1000; i++ {
				str := strconv.Itoa(i)
				r.Register(
					route.Uri("bench.vcap.me."+str),
					route.NewEndpoint(&route.EndpointOpts{
						Host: "localhost",
						Port: uint16(i),
						StaleThresholdInSeconds: -1,
						UseTLS:                  false,
					}),
				)
			}
		})
	}, 10)

})
