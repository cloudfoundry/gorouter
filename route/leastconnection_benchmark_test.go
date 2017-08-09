package route_test

import (
	"fmt"
	"testing"
	"time"

	"code.cloudfoundry.org/gorouter/route"
	"code.cloudfoundry.org/routing-api/models"
)

func loadBalance(lb route.EndpointIterator) {
	e := lb.Next()
	lb.PreRequest(e)
	lb.PostRequest(e)
}

func loadBalanceFor(strategy string, b *testing.B) {

	pool := route.NewPool(2*time.Minute, "", "")
	total := 5
	endpoints := make([]*route.Endpoint, 0)
	for i := 0; i < total; i++ {
		ip := fmt.Sprintf("10.0.1.%d", i)
		e := route.NewEndpoint("", ip, 60000, "", "", nil, -1, "", models.ModificationTag{}, "", false)
		endpoints = append(endpoints, e)
		pool.Put(e)
	}

	var lb route.EndpointIterator
	switch strategy {
	case "round-robin":
		lb = route.NewRoundRobin(pool, "")
	case "least-connection":
		lb = route.NewLeastConnection(pool, "")
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
