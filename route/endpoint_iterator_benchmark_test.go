package route_test

import (
	"fmt"
	"math/rand"
	"testing"
	"time"

	"code.cloudfoundry.org/gorouter/logger/fakes"
	"code.cloudfoundry.org/gorouter/route"
)

func testLoadBalance(lb route.EndpointIterator, b *testing.B) {
	b.ResetTimer() // don't include setup in time
	for n := 0; n < b.N; n++ {
		e := lb.Next(0)
		lb.PreRequest(e)
		lb.PostRequest(e)
	}
}

const (
	allEndpointsInLocalAZ = iota
	allEndpointsInNonLocalAZ
	halfEndpointsInLocalAZ

	localAZ = "az-local"
)

func setupEndpointIterator(total int, azDistribution int, strategy string) route.EndpointIterator {
	// Make pool
	pool := route.NewPool(&route.PoolOpts{
		Logger:             new(fakes.FakeLogger),
		RetryAfterFailure:  2 * time.Minute,
		Host:               "",
		ContextPath:        "",
		MaxConnsPerBackend: 0,
	})

	// Create endpoints with desired AZ distribution
	endpoints := make([]*route.Endpoint, 0)
	var az string
	for i := 0; i < total; i++ {
		ip := fmt.Sprintf("10.0.1.%d", i)

		switch azDistribution {
		case allEndpointsInLocalAZ:
			az = localAZ
		case allEndpointsInNonLocalAZ:
			az = "meow-fake-az"
		case halfEndpointsInLocalAZ:
			if i%2 == 0 {
				az = localAZ
			} else {
				az = "meow-fake-az"
			}
		}

		e := route.NewEndpoint(&route.EndpointOpts{Host: ip, AvailabilityZone: az})
		endpoints = append(endpoints, e)
	}

	// Shuffle the endpoints, then add them to the pool
	rand.Shuffle(total, func(i, j int) { endpoints[i], endpoints[j] = endpoints[j], endpoints[i] })
	for _, e := range endpoints {
		pool.Put(e)
	}

	var lb route.EndpointIterator
	switch strategy {
	case "round-robin":
		lb = route.NewRoundRobin(pool, "", false, localAZ)
	case "round-robin-locally-optimistic":
		lb = route.NewRoundRobin(pool, "", true, localAZ)
	case "least-connection":
		lb = route.NewLeastConnection(pool, "", false, localAZ)
	case "least-connection-locally-optimistic":
		lb = route.NewLeastConnection(pool, "", true, localAZ)
	default:
		panic("invalid load balancing strategy")
	}

	return lb
}

// Least Connection, locally optimistic tests

func BenchmarkLeastConnLocal1AllLocalEndpoints(b *testing.B) {
	numEndpoints := 1
	strategy := "least-connection-locally-optimistic"
	iter := setupEndpointIterator(numEndpoints, allEndpointsInLocalAZ, strategy)
	testLoadBalance(iter, b)
}

func BenchmarkLeastConnLocal1NoneLocalEndpoints(b *testing.B) {
	numEndpoints := 1
	strategy := "least-connection-locally-optimistic"
	iter := setupEndpointIterator(numEndpoints, allEndpointsInNonLocalAZ, strategy)
	testLoadBalance(iter, b)
}

func BenchmarkLeastConnLocal5AllLocalEndpoints(b *testing.B) {
	numEndpoints := 5
	strategy := "least-connection-locally-optimistic"
	iter := setupEndpointIterator(numEndpoints, allEndpointsInLocalAZ, strategy)
	testLoadBalance(iter, b)
}

func BenchmarkLeastConnLocal5HalfLocalEndpoints(b *testing.B) {
	numEndpoints := 5
	strategy := "least-connection-locally-optimistic"
	iter := setupEndpointIterator(numEndpoints, halfEndpointsInLocalAZ, strategy)
	testLoadBalance(iter, b)
}

func BenchmarkLeastConnLocal5NoneLocalEndpoints(b *testing.B) {
	numEndpoints := 5
	strategy := "least-connection-locally-optimistic"
	iter := setupEndpointIterator(numEndpoints, allEndpointsInNonLocalAZ, strategy)
	testLoadBalance(iter, b)
}

func BenchmarkLeastConnLocal15AllLocalEndpoints(b *testing.B) {
	numEndpoints := 15
	strategy := "least-connection-locally-optimistic"
	iter := setupEndpointIterator(numEndpoints, allEndpointsInLocalAZ, strategy)
	testLoadBalance(iter, b)
}

func BenchmarkLeastConnLocal15HalfLocalEndpoints(b *testing.B) {
	numEndpoints := 15
	strategy := "least-connection-locally-optimistic"
	iter := setupEndpointIterator(numEndpoints, halfEndpointsInLocalAZ, strategy)
	testLoadBalance(iter, b)
}

func BenchmarkLeastConnLocal15NoneLocalEndpoints(b *testing.B) {
	numEndpoints := 15
	strategy := "least-connection-locally-optimistic"
	iter := setupEndpointIterator(numEndpoints, allEndpointsInNonLocalAZ, strategy)
	testLoadBalance(iter, b)
}

// Round Robin, locally optimistic tests

func BenchmarkRoundRobinLocal1AllLocalEndpoints(b *testing.B) {
	numEndpoints := 1
	strategy := "round-robin-locally-optimistic"
	iter := setupEndpointIterator(numEndpoints, allEndpointsInLocalAZ, strategy)
	testLoadBalance(iter, b)
}

func BenchmarkRoundRobinLocal1NoneLocalEndpoints(b *testing.B) {
	numEndpoints := 1
	strategy := "round-robin-locally-optimistic"
	iter := setupEndpointIterator(numEndpoints, allEndpointsInNonLocalAZ, strategy)
	testLoadBalance(iter, b)
}

func BenchmarkRoundRobinLocal5AllLocalEndpoints(b *testing.B) {
	numEndpoints := 5
	strategy := "round-robin-locally-optimistic"
	iter := setupEndpointIterator(numEndpoints, allEndpointsInLocalAZ, strategy)
	testLoadBalance(iter, b)
}

func BenchmarkRoundRobinLocal5HalfLocalEndpoints(b *testing.B) {
	numEndpoints := 5
	strategy := "round-robin-locally-optimistic"
	iter := setupEndpointIterator(numEndpoints, halfEndpointsInLocalAZ, strategy)
	testLoadBalance(iter, b)
}

func BenchmarkRoundRobinLocal5NoneLocalEndpoints(b *testing.B) {
	numEndpoints := 5
	strategy := "round-robin-locally-optimistic"
	iter := setupEndpointIterator(numEndpoints, allEndpointsInNonLocalAZ, strategy)
	testLoadBalance(iter, b)
}

func BenchmarkRoundRobinLocal15AllLocalEndpoints(b *testing.B) {
	numEndpoints := 15
	strategy := "round-robin-locally-optimistic"
	iter := setupEndpointIterator(numEndpoints, allEndpointsInLocalAZ, strategy)
	testLoadBalance(iter, b)
}

func BenchmarkRoundRobinLocal15HalfLocalEndpoints(b *testing.B) {
	numEndpoints := 15
	strategy := "round-robin-locally-optimistic"
	iter := setupEndpointIterator(numEndpoints, halfEndpointsInLocalAZ, strategy)
	testLoadBalance(iter, b)
}

func BenchmarkRoundRobinLocal15NoneLocalEndpoints(b *testing.B) {
	numEndpoints := 15
	strategy := "round-robin-locally-optimistic"
	iter := setupEndpointIterator(numEndpoints, allEndpointsInNonLocalAZ, strategy)
	testLoadBalance(iter, b)
}

// Round Robin, non-locally optimistic tests

func BenchmarkRoundRobin1Endpoint(b *testing.B) {
	numEndpoints := 1
	strategy := "round-robin"
	iter := setupEndpointIterator(numEndpoints, allEndpointsInNonLocalAZ, strategy)
	testLoadBalance(iter, b)
}

func BenchmarkRoundRobin5Endpoints(b *testing.B) {
	numEndpoints := 5
	strategy := "round-robin"
	iter := setupEndpointIterator(numEndpoints, allEndpointsInNonLocalAZ, strategy)
	testLoadBalance(iter, b)
}

func BenchmarkRoundRobin15Endpoints(b *testing.B) {
	numEndpoints := 15
	strategy := "round-robin"
	iter := setupEndpointIterator(numEndpoints, allEndpointsInNonLocalAZ, strategy)
	testLoadBalance(iter, b)
}

// Least Connection, non-locally optimistic tests

func BenchmarkLeastConn1Endpoint(b *testing.B) {
	numEndpoints := 1
	strategy := "least-connection"
	iter := setupEndpointIterator(numEndpoints, allEndpointsInNonLocalAZ, strategy)
	testLoadBalance(iter, b)
}

func BenchmarkLeastConn5Endpoints(b *testing.B) {
	numEndpoints := 5
	strategy := "least-connection"
	iter := setupEndpointIterator(numEndpoints, allEndpointsInNonLocalAZ, strategy)
	testLoadBalance(iter, b)
}

func BenchmarkLeastConn15Endpoints(b *testing.B) {
	numEndpoints := 15
	strategy := "least-connection"
	iter := setupEndpointIterator(numEndpoints, allEndpointsInNonLocalAZ, strategy)
	testLoadBalance(iter, b)
}
