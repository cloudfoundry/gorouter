package main_test

import (
	"code.cloudfoundry.org/gorouter/access_log"
	"code.cloudfoundry.org/gorouter/config"
	"code.cloudfoundry.org/gorouter/proxy"
	"code.cloudfoundry.org/gorouter/registry"
	"code.cloudfoundry.org/gorouter/route"
	"code.cloudfoundry.org/gorouter/varz"
	"code.cloudfoundry.org/lager/lagertest"
	"code.cloudfoundry.org/routing-api/models"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	"strconv"

	"code.cloudfoundry.org/gorouter/metrics/reporter/fakes"
)

var _ = Describe("AccessLogRecord", func() {
	Measure("Register", func(b Benchmarker) {
		logger := lagertest.NewTestLogger("test")
		c := config.DefaultConfig()
		r := registry.NewRouteRegistry(logger, c, new(fakes.FakeRouteRegistryReporter))

		accesslog, err := access_log.CreateRunningAccessLogger(logger, c)
		Expect(err).ToNot(HaveOccurred())

		proxy.NewProxy(proxy.ProxyArgs{
			EndpointTimeout: c.EndpointTimeout,
			Ip:              c.Ip,
			TraceKey:        c.TraceKey,
			Registry:        r,
			Reporter:        varz.NewVarz(r),
			AccessLogger:    accesslog,
		})

		b.Time("RegisterTime", func() {
			for i := 0; i < 1000; i++ {
				str := strconv.Itoa(i)
				r.Register(
					route.Uri("bench.vcap.me."+str),
					route.NewEndpoint("", "localhost", uint16(i), "", "", nil, -1, "", models.ModificationTag{}),
				)
			}
		})
	}, 10)

})
