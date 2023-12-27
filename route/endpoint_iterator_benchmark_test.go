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
		e := route.NewEndpoint(&route.EndpointOpts{Host: ip, AvailabilityZone: fmt.Sprintf("az-%d", i)})
		endpoints = append(endpoints, e)
		pool.Put(e)
	}

	var lb route.EndpointIterator
	switch strategy {
	case "round-robin":
		lb = route.NewRoundRobin(pool, "", false, "meow-az")
	case "round-robin-locally-optimistic":
		lb = route.NewRoundRobin(pool, "", true, "az-1")
	case "least-connection":
		lb = route.NewLeastConnection(pool, "", false, "meow-az")
	case "least-connection-locally-optimistic":
		lb = route.NewLeastConnection(pool, "", true, "az-2")
	default:
		panic("invalid load balancing strategy")
	}

	for n := 0; n < b.N; n++ {
		loadBalance(lb)
	}
}

func BenchmarkLeastConnectionLocallyOptimistic(b *testing.B) {
	loadBalanceFor("least-connection-locally-optimistic", b)
}

func BenchmarkLeastConnection(b *testing.B) {
	loadBalanceFor("least-connection", b)
}

func BenchmarkRoundRobin(b *testing.B) {
	loadBalanceFor("round-robin", b)
}

func BenchmarkRoundRobinLocallyOptimistic(b *testing.B) {
	loadBalanceFor("round-robin-locally-optimistic", b)
}
