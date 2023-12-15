package route_test

import (
	"fmt"
	"testing"
	"time"

	"code.cloudfoundry.org/gorouter/logger/fakes"
	"code.cloudfoundry.org/gorouter/route"
)

func loadBalance(lb route.EndpointIterator) {
	e := lb.Next(1)
	lb.PreRequest(e)
	lb.PostRequest(e)
}

func loadBalanceFor(strategy string, b *testing.B) {

	pool := route.NewPool(&route.PoolOpts{
		Logger:             new(fakes.FakeLogger),
		RetryAfterFailure:  2 * time.Minute,
		Host:               "",
		ContextPath:        "",
		MaxConnsPerBackend: 0,
	})

	total := 5
	endpoints := make([]*route.Endpoint, 0)
	for i := 0; i < total; i++ {
		ip := fmt.Sprintf("10.0.1.%d", i)
		e := route.NewEndpoint(&route.EndpointOpts{Host: ip})
		endpoints = append(endpoints, e)
		pool.Put(e)
	}

	var lb route.EndpointIterator
	switch strategy {
	case "round-robin":
		lb = route.NewRoundRobin(pool, "")
	case "least-connection":
		lb = route.NewLeastConnection(pool, "", false, "meow-az")
	default:
		panic("invalid load balancing strategy")
	}

	for n := 0; n < b.N; n++ {
		loadBalance(lb)
	}
}

func BenchmarkLeastConnection(b *testing.B) {
	loadBalanceFor("least-connection", b)
}

func BenchmarkRoundRobin(b *testing.B) {
	loadBalanceFor("round-robin", b)
}
