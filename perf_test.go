package main_test

import (
	"crypto/tls"

	"code.cloudfoundry.org/gorouter/access_log"
	"code.cloudfoundry.org/gorouter/config"
	"code.cloudfoundry.org/gorouter/proxy"
	"code.cloudfoundry.org/gorouter/registry"
	"code.cloudfoundry.org/gorouter/route"
	"code.cloudfoundry.org/gorouter/routeservice"
	"code.cloudfoundry.org/gorouter/test_util"
	"code.cloudfoundry.org/gorouter/varz"
	"code.cloudfoundry.org/routing-api/models"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	"strconv"

	"code.cloudfoundry.org/gorouter/metrics/reporter/fakes"
)

var _ = Describe("AccessLogRecord", func() {
	Measure("Register", func(b Benchmarker) {
		logger := test_util.NewTestZapLogger("test")
		c := config.DefaultConfig()
		r := registry.NewRouteRegistry(logger, c, new(fakes.FakeRouteRegistryReporter))

		accesslog, err := access_log.CreateRunningAccessLogger(logger, c)
		Expect(err).ToNot(HaveOccurred())

		proxy.NewProxy(logger, accesslog, c, r, varz.NewVarz(r), &routeservice.RouteServiceConfig{},
			&tls.Config{}, nil)

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
