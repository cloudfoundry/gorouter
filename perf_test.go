package main_test

import (
	"github.com/cloudfoundry/gorouter/access_log"
	"github.com/cloudfoundry/gorouter/config"
	"github.com/cloudfoundry/gorouter/proxy"
	"github.com/cloudfoundry/gorouter/registry"
	"github.com/cloudfoundry/gorouter/route"
	"github.com/cloudfoundry/gorouter/varz"
	"github.com/pivotal-golang/lager/lagertest"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	"strconv"

	"github.com/cloudfoundry/gorouter/metrics/reporter/fakes"
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
					route.NewEndpoint("", "localhost", uint16(i), "", nil, -1, ""),
				)
			}
		})
	}, 10)

})
