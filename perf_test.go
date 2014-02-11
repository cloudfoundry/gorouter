package main

import (
	"github.com/cloudfoundry/yagnats/fakeyagnats"
	"strconv"
	"testing"

	"github.com/cloudfoundry/gorouter/access_log"
	"github.com/cloudfoundry/gorouter/config"
	"github.com/cloudfoundry/gorouter/proxy"
	"github.com/cloudfoundry/gorouter/registry"
	"github.com/cloudfoundry/gorouter/route"
	"github.com/cloudfoundry/gorouter/varz"
)

const (
	Host = "1.2.3.4"
	Port = 1234
)

func BenchmarkRegister(b *testing.B) {
	c := config.DefaultConfig()
	mbus := fakeyagnats.New()
	r := registry.NewCFRegistry(c, mbus)

	proxy.NewProxy(proxy.ProxyArgs{
		EndpointTimeout: c.EndpointTimeout,
		Ip:              c.Ip,
		TraceKey:        c.TraceKey,
		Registry:        r,
		Reporter:        varz.NewVarz(r),
		Logger:          access_log.CreateRunningAccessLogger(c),
	})

	for i := 0; i < b.N; i++ {
		str := strconv.Itoa(i)

		r.Register(
			route.Uri("bench.vcap.me."+str),
			&route.Endpoint{
				Host: "localhost",
				Port: uint16(i),
			},
		)
	}
}
