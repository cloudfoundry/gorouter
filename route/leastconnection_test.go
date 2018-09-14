package route_test

import (
	"fmt"
	"time"

	"code.cloudfoundry.org/gorouter/logger/fakes"
	"code.cloudfoundry.org/gorouter/route"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("LeastConnection", func() {
	var pool *route.Pool

	BeforeEach(func() {
		pool = route.NewPool(
			&route.PoolOpts{
				Logger:             new(fakes.FakeLogger),
				RetryAfterFailure:  2 * time.Minute,
				Host:               "",
				ContextPath:        "",
				MaxConnsPerBackend: 0})
	})

	Describe("Next", func() {

		Context("when pool is empty", func() {
			It("does not select an endpoint", func() {
				iter := route.NewLeastConnection(pool, "")
				Expect(iter.Next()).To(BeNil())
			})
		})

		Context("when pool has endpoints", func() {
			var (
				endpoints []*route.Endpoint
				total     int
			)

			BeforeEach(func() {
				total = 5
				endpoints = make([]*route.Endpoint, 0)
				for i := 0; i < total; i++ {
					ip := fmt.Sprintf("10.0.1.%d", i)
					e := route.NewEndpoint(&route.EndpointOpts{Host: ip, Port: 60000})
					endpoints = append(endpoints, e)
					pool.Put(e)
				}
				// 10.0.1.0:6000
				// 10.0.1.1:6000
				// 10.0.1.2:6000
				// 10.0.1.3:6000
				// 10.0.1.4:6000
			})

			Context("when all endpoints have no statistics", func() {
				It("selects a random endpoint", func() {
					iter := route.NewLeastConnection(pool, "")
					n := iter.Next()
					Expect(n).NotTo(BeNil())
				})
			})

			Context("when all endpoints have zero connections", func() {
				BeforeEach(func() {
					// set all to zero
					setConnectionCount(endpoints, []int{0, 0, 0, 0, 0})
				})

				It("selects a random endpoint", func() {
					iter := route.NewLeastConnection(pool, "")
					n := iter.Next()
					Expect(n).NotTo(BeNil())
				})
			})

			Context("when endpoints have varying number of connections", func() {

				It("selects endpoint with least connection", func() {
					setConnectionCount(endpoints, []int{0, 1, 1, 1, 1})
					iter := route.NewLeastConnection(pool, "")
					Expect(iter.Next()).To(Equal(endpoints[0]))

					setConnectionCount(endpoints, []int{1, 0, 1, 1, 1})
					Expect(iter.Next()).To(Equal(endpoints[1]))

					setConnectionCount(endpoints, []int{1, 1, 0, 1, 1})
					Expect(iter.Next()).To(Equal(endpoints[2]))

					setConnectionCount(endpoints, []int{1, 1, 1, 0, 1})
					Expect(iter.Next()).To(Equal(endpoints[3]))

					setConnectionCount(endpoints, []int{1, 1, 1, 1, 0})
					Expect(iter.Next()).To(Equal(endpoints[4]))

					setConnectionCount(endpoints, []int{1, 4, 15, 8, 3})
					Expect(iter.Next()).To(Equal(endpoints[0]))

					setConnectionCount(endpoints, []int{5, 4, 15, 8, 3})
					Expect(iter.Next()).To(Equal(endpoints[4]))

					setConnectionCount(endpoints, []int{5, 4, 15, 8, 7})
					Expect(iter.Next()).To(Equal(endpoints[1]))

					setConnectionCount(endpoints, []int{5, 5, 15, 2, 7})
					Expect(iter.Next()).To(Equal(endpoints[3]))
				})

				It("selects random endpoint from all with least connection", func() {
					iter := route.NewLeastConnection(pool, "")

					setConnectionCount(endpoints, []int{1, 0, 0, 0, 0})
					okRandoms := []string{
						"10.0.1.1:60000",
						"10.0.1.2:60000",
						"10.0.1.3:60000",
						"10.0.1.4:60000",
					}
					Expect(okRandoms).Should(ContainElement(iter.Next().CanonicalAddr()))

					setConnectionCount(endpoints, []int{10, 10, 15, 10, 11})
					okRandoms = []string{
						"10.0.1.0:60000",
						"10.0.1.1:60000",
						"10.0.1.3:60000",
					}
					Expect(okRandoms).Should(ContainElement(iter.Next().CanonicalAddr()))
				})
			})

			Context("when some endpoints are overloaded", func() {
				var (
					epOne, epTwo *route.Endpoint
				)

				BeforeEach(func() {
					pool = route.NewPool(&route.PoolOpts{
						Logger:             new(fakes.FakeLogger),
						RetryAfterFailure:  2 * time.Minute,
						Host:               "",
						ContextPath:        "",
						MaxConnsPerBackend: 2,
					})

					epOne = route.NewEndpoint(&route.EndpointOpts{Host: "5.5.5.5", Port: 5555, PrivateInstanceId: "private-label-1"})
					pool.Put(epOne)
					// epTwo is always overloaded
					epTwo = route.NewEndpoint(&route.EndpointOpts{Host: "2.2.2.2", Port: 2222, PrivateInstanceId: "private-label-2"})
					epTwo.Stats.NumberConnections.Increment()
					epTwo.Stats.NumberConnections.Increment()
					pool.Put(epTwo)
				})

				Context("when there is no initial endpoint", func() {
					Context("when all endpoints are overloaded", func() {
						It("returns nil", func() {
							epOne.Stats.NumberConnections.Increment()
							epOne.Stats.NumberConnections.Increment()
							iter := route.NewLeastConnection(pool, "")

							Consistently(func() *route.Endpoint {
								return iter.Next()
							}).Should(BeNil())
						})
					})

					Context("when there is only one endpoint", func() {
						Context("when that endpoint is overload", func() {
							It("returns no endpoint", func() {
								Expect(pool.Remove(epOne)).To(BeTrue())
								iter := route.NewLeastConnection(pool, "")

								Consistently(func() *route.Endpoint {
									return iter.Next()
								}).Should(BeNil())
							})
						})
					})
				})

				Context("when there is an initial endpoint", func() {
					var iter route.EndpointIterator
					BeforeEach(func() {
						iter = route.NewLeastConnection(pool, "private-label-2")
					})

					Context("when the initial endpoint is overloaded", func() {
						Context("when there is an unencumbered endpoint", func() {
							It("returns the unencumbered endpoint", func() {
								Expect(iter.Next()).To(Equal(epOne))
								Expect(iter.Next()).To(Equal(epOne))
							})
						})

						Context("when there isn't an unencumbered endpoint", func() {
							BeforeEach(func() {
								epOne.Stats.NumberConnections.Increment()
								epOne.Stats.NumberConnections.Increment()
							})
							It("returns nil", func() {
								Consistently(func() *route.Endpoint {
									return iter.Next()
								}).Should(BeNil())
							})
						})
					})
				})
			})
		})
	})

	Context("PreRequest", func() {
		It("increments the NumberConnections counter", func() {
			endpointFoo := route.NewEndpoint(&route.EndpointOpts{Host: "1.2.3.4"})

			Expect(endpointFoo.Stats.NumberConnections.Count()).To(Equal(int64(0)))
			pool.Put(endpointFoo)
			iter := route.NewLeastConnection(pool, "foo")
			iter.PreRequest(endpointFoo)
			Expect(endpointFoo.Stats.NumberConnections.Count()).To(Equal(int64(1)))
		})
	})

	Context("PostRequest", func() {
		It("decrements the NumberConnections counter", func() {
			endpointFoo := route.NewEndpoint(&route.EndpointOpts{Host: "1.2.3.4"})

			endpointFoo.Stats = &route.Stats{
				NumberConnections: route.NewCounter(int64(1)),
			}
			Expect(endpointFoo.Stats.NumberConnections.Count()).To(Equal(int64(1)))
			pool.Put(endpointFoo)
			iter := route.NewLeastConnection(pool, "foo")
			iter.PostRequest(endpointFoo)
			Expect(endpointFoo.Stats.NumberConnections.Count()).To(Equal(int64(0)))
		})
	})
})

func setConnectionCount(endpoints []*route.Endpoint, counts []int) {
	for i := 0; i < len(endpoints); i++ {
		endpoints[i].Stats = &route.Stats{
			NumberConnections: route.NewCounter(int64(counts[i])),
		}
	}
}
