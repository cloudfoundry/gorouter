package route_test

import (
	"fmt"
	"sync"
	"time"

	"code.cloudfoundry.org/gorouter/logger"
	"code.cloudfoundry.org/gorouter/route"
	"code.cloudfoundry.org/gorouter/test_util"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gbytes"
)

var _ = Describe("LeastConnection", func() {
	var (
		pool   *route.EndpointPool
		logger logger.Logger
	)

	BeforeEach(func() {
		logger = test_util.NewTestZapLogger("test")
		pool = route.NewPool(
			&route.PoolOpts{
				Logger:             logger,
				RetryAfterFailure:  2 * time.Minute,
				Host:               "",
				ContextPath:        "",
				MaxConnsPerBackend: 0})
	})

	Describe("Next", func() {
		Context("when pool is empty", func() {
			It("does not select an endpoint", func() {
				iter := route.NewLeastConnection(logger, pool, "", false, false, "meow-az")
				Expect(iter.Next(0)).To(BeNil())
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
					iter := route.NewLeastConnection(logger, pool, "", false, false, "meow-az")
					n := iter.Next(0)
					Expect(n).NotTo(BeNil())
				})
			})

			Context("when all endpoints have zero connections", func() {
				BeforeEach(func() {
					// set all to zero
					setConnectionCount(endpoints, []int{0, 0, 0, 0, 0})
				})

				It("selects a random endpoint", func() {
					var wg sync.WaitGroup
					for i := 0; i < 100; i++ {
						wg.Add(1)
						go func(attempt int) {
							iter := route.NewLeastConnection(logger, pool, "", false, false, "meow-az")
							n1 := iter.Next(attempt)
							Expect(n1).NotTo(BeNil())

							Eventually(func() bool {
								n2 := iter.Next(attempt)
								return n1.Equal(n2)
							}).Should(BeFalse())
							wg.Done()
						}(i)
					}
					wg.Wait()
				})
			})

			Context("when endpoints have varying number of connections", func() {
				It("selects endpoint with least connection", func() {
					setConnectionCount(endpoints, []int{0, 1, 1, 1, 1})
					iter := route.NewLeastConnection(logger, pool, "", false, false, "meow-az")
					Expect(iter.Next(0)).To(Equal(endpoints[0]))

					setConnectionCount(endpoints, []int{1, 0, 1, 1, 1})
					Expect(iter.Next(1)).To(Equal(endpoints[1]))

					setConnectionCount(endpoints, []int{1, 1, 0, 1, 1})
					Expect(iter.Next(2)).To(Equal(endpoints[2]))

					setConnectionCount(endpoints, []int{1, 1, 1, 0, 1})
					Expect(iter.Next(3)).To(Equal(endpoints[3]))

					setConnectionCount(endpoints, []int{1, 1, 1, 1, 0})
					Expect(iter.Next(4)).To(Equal(endpoints[4]))

					setConnectionCount(endpoints, []int{1, 4, 15, 8, 3})
					Expect(iter.Next(5)).To(Equal(endpoints[0]))

					setConnectionCount(endpoints, []int{5, 4, 15, 8, 3})
					Expect(iter.Next(6)).To(Equal(endpoints[4]))

					setConnectionCount(endpoints, []int{5, 4, 15, 8, 7})
					Expect(iter.Next(7)).To(Equal(endpoints[1]))

					setConnectionCount(endpoints, []int{5, 5, 15, 2, 7})
					Expect(iter.Next(8)).To(Equal(endpoints[3]))
				})

				It("selects random endpoint from all with least connection", func() {
					iter := route.NewLeastConnection(logger, pool, "", false, false, "meow-az")

					setConnectionCount(endpoints, []int{1, 0, 0, 0, 0})
					okRandoms := []string{
						"10.0.1.1:60000",
						"10.0.1.2:60000",
						"10.0.1.3:60000",
						"10.0.1.4:60000",
					}
					Expect(okRandoms).Should(ContainElement(iter.Next(0).CanonicalAddr()))

					setConnectionCount(endpoints, []int{10, 10, 15, 10, 11})
					okRandoms = []string{
						"10.0.1.0:60000",
						"10.0.1.1:60000",
						"10.0.1.3:60000",
					}
					Expect(okRandoms).Should(ContainElement(iter.Next(1).CanonicalAddr()))
				})
			})

			Context("when some endpoints are overloaded", func() {
				var (
					epOne, epTwo *route.Endpoint
				)

				BeforeEach(func() {
					pool = route.NewPool(&route.PoolOpts{
						Logger:             logger,
						RetryAfterFailure:  2 * time.Minute,
						Host:               "",
						ContextPath:        "",
						MaxConnsPerBackend: 2,
					})

					epOne = route.NewEndpoint(&route.EndpointOpts{Host: "5.5.5.5", Port: 5555, PrivateInstanceId: "private-label-1"})
					pool.Put(epOne)
					// epTwo is always overloaded
					epTwo = route.NewEndpoint(&route.EndpointOpts{Host: "2.2.2.2", Port: 2222, PrivateInstanceId: "private-label-2"})
					pool.Put(epTwo)
				})

				Context("when there is no initial endpoint", func() {
					Context("when all endpoints are overloaded", func() {
						BeforeEach(func() {
							epOne.Stats.NumberConnections.Increment()
							epOne.Stats.NumberConnections.Increment()
							epTwo.Stats.NumberConnections.Increment()
							epTwo.Stats.NumberConnections.Increment()
						})

						It("returns nil", func() {
							iter := route.NewLeastConnection(logger, pool, "", false, false, "meow-az")
							Consistently(func() *route.Endpoint {
								return iter.Next(0)
							}).Should(BeNil())
						})
					})

					Context("when there is only one endpoint", func() {
						BeforeEach(func() {
							Expect(pool.Remove(epOne)).To(BeTrue())
							epTwo.Stats.NumberConnections.Increment()
							epTwo.Stats.NumberConnections.Increment()
						})

						Context("when that endpoint is overload", func() {
							It("returns no endpoint", func() {
								iter := route.NewLeastConnection(logger, pool, "", false, false, "meow-az")
								Consistently(func() *route.Endpoint {
									return iter.Next(0)
								}).Should(BeNil())
							})
						})
					})
				})

				Context("when there is an initial endpoint", func() {
					var iter route.EndpointIterator

					Context("when the initial endpoint is overloaded", func() {
						BeforeEach(func() {
							epOne.Stats.NumberConnections.Increment()
							epOne.Stats.NumberConnections.Increment()
						})

						Context("when the endpoint is not required to be sticky", func() {
							BeforeEach(func() {
								iter = route.NewLeastConnection(logger, pool, "private-label-1", false, false, "meow-az")
							})

							Context("when there is an unencumbered endpoint", func() {
								It("returns the unencumbered endpoint", func() {
									Expect(iter.Next(0)).To(Equal(epTwo))
									Expect(iter.Next(1)).To(Equal(epTwo))
								})
							})

							Context("when there isn't an unencumbered endpoint", func() {
								BeforeEach(func() {
									epTwo.Stats.NumberConnections.Increment()
									epTwo.Stats.NumberConnections.Increment()
								})

								It("returns nil", func() {
									Consistently(func() *route.Endpoint {
										return iter.Next(0)
									}).Should(BeNil())
								})
							})
						})

						Context("when the endpoint must be be sticky", func() {
							BeforeEach(func() {
								iter = route.NewLeastConnection(logger, pool, "private-label-1", true, false, "meow-az")
							})

							It("returns nil", func() {
								Consistently(func() *route.Endpoint {
									return iter.Next(0)
								}).Should(BeNil())
							})
							It("logs that it could not choose another endpoint", func() {
								iter.Next(0)
								Expect(logger).Should(gbytes.Say("endpoint-overloaded-but-request-must-be-sticky"))
							})
						})
					})
				})
				Context("when an endpoint was requested but doesn't exist", func() {
					var iter route.EndpointIterator
					var pool *route.EndpointPool

					BeforeEach(func() {
						pool = route.NewPool(&route.PoolOpts{
							Logger:             logger,
							RetryAfterFailure:  2 * time.Minute,
							Host:               "",
							ContextPath:        "",
							MaxConnsPerBackend: 2,
						})

						epOne := route.NewEndpoint(&route.EndpointOpts{Host: "5.5.5.5", Port: 5555, PrivateInstanceId: "private-label-1"})
						pool.Put(epOne)
						// epTwo 'private-label-2' does not exist
					})

					Context("when the endpoint is not required to be sticky", func() {
						BeforeEach(func() {
							iter = route.NewLeastConnection(logger, pool, "private-label-2", false, false, "meow-az")
						})

						It("Returns the next available endpoint", func() {
							Consistently(func() *route.Endpoint {
								return iter.Next(0)
							}).Should(Equal(epOne))
						})
						It("logs that it chose another endpoint", func() {
							iter.Next(0)
							Expect(logger).Should(gbytes.Say("endpoint-missing-choosing-alternate"))
						})

					})
					Context("when the endpoint is required to be sticky", func() {
						BeforeEach(func() {
							iter = route.NewLeastConnection(logger, pool, "private-label-2", true, false, "meow-az")
						})

						It("returns nil", func() {
							Consistently(func() *route.Endpoint {
								return iter.Next(0)
							}).Should(BeNil())
						})
						It("logs that it could not choose another endpoint", func() {
							iter.Next(0)
							Expect(logger).Should(gbytes.Say("endpoint-missing-but-request-must-be-sticky"))
						})
					})
				})
			})
		})

		Describe("when in locally-optimistic mode", func() {
			var (
				iter                                                         route.EndpointIterator
				localAZ                                                      = "az-2"
				otherAZEndpointOne, otherAZEndpointTwo, otherAZEndpointThree *route.Endpoint
				localAZEndpointOne, localAZEndpointTwo, localAZEndpointThree *route.Endpoint
			)

			BeforeEach(func() {
				pool = route.NewPool(&route.PoolOpts{
					Logger:             logger,
					RetryAfterFailure:  2 * time.Minute,
					Host:               "",
					ContextPath:        "",
					MaxConnsPerBackend: 2,
				})

				otherAZEndpointOne = route.NewEndpoint(&route.EndpointOpts{Host: "10.0.1.0", Port: 60000, AvailabilityZone: "meow-az"})
				otherAZEndpointTwo = route.NewEndpoint(&route.EndpointOpts{Host: "10.0.1.1", Port: 60000, AvailabilityZone: "potato-az"})
				otherAZEndpointThree = route.NewEndpoint(&route.EndpointOpts{Host: "10.0.1.2", Port: 60000, AvailabilityZone: ""})
				localAZEndpointOne = route.NewEndpoint(&route.EndpointOpts{Host: "10.0.1.3", Port: 60000, AvailabilityZone: localAZ})
				localAZEndpointTwo = route.NewEndpoint(&route.EndpointOpts{Host: "10.0.1.4", Port: 60000, AvailabilityZone: localAZ})
				localAZEndpointThree = route.NewEndpoint(&route.EndpointOpts{Host: "10.0.1.5", Port: 60000, AvailabilityZone: localAZ})
			})

			JustBeforeEach(func() {
				iter = route.NewLeastConnection(logger, pool, "", false, true, localAZ)
			})

			Context("on the first attempt", func() {

				Context("when the pool is empty", func() {
					It("does not select an endpoint", func() {
						Expect(iter.Next(0)).To(BeNil())
					})
				})

				Context("when the pool has one endpoint in the same AZ as the router", func() {
					BeforeEach(func() {
						pool.Put(otherAZEndpointOne)
						pool.Put(otherAZEndpointTwo)
						pool.Put(otherAZEndpointThree)
						pool.Put(localAZEndpointOne)
					})

					It("selects the endpoint in the same az", func() {
						chosen := iter.Next(0)
						Expect(chosen.AvailabilityZone).To(Equal(localAZ))
						Expect(chosen).To(Equal(localAZEndpointOne))
					})

					Context("and it is overloaded", func() {
						BeforeEach(func() {
							localAZEndpointOne.Stats.NumberConnections.Increment()
							localAZEndpointOne.Stats.NumberConnections.Increment()
						})

						It("selects a non-overloaded endpoint in a different az", func() {
							chosen := iter.Next(0)
							Expect(chosen.AvailabilityZone).ToNot(Equal(localAZ))
						})
					})
				})

				Context("when the pool has multiple in the same AZ as the router", func() {
					Context("and the endpoints have the same number of connections", func() {
						BeforeEach(func() {
							pool.Put(otherAZEndpointOne)
							pool.Put(otherAZEndpointTwo)
							pool.Put(otherAZEndpointThree)

							localAZEndpointOne.Stats.NumberConnections.Increment()
							pool.Put(localAZEndpointOne)

							localAZEndpointTwo.Stats.NumberConnections.Increment()
							pool.Put(localAZEndpointTwo)

							localAZEndpointThree.Stats.NumberConnections.Increment()
							pool.Put(localAZEndpointThree)
						})

						It("selects a random one of the endpoints in the same AZ", func() {
							okRandoms := []string{
								"10.0.1.3:60000",
								"10.0.1.4:60000",
								"10.0.1.5:60000",
							}
							chosen := iter.Next(0)
							Expect(chosen.AvailabilityZone).To(Equal(localAZ))
							Expect(okRandoms).Should(ContainElement(chosen.CanonicalAddr()))
						})
					})

					Context("and the endpoints have different number of connections", func() {
						BeforeEach(func() {
							pool.Put(otherAZEndpointOne)
							pool.Put(otherAZEndpointTwo)
							pool.Put(otherAZEndpointThree)

							localAZEndpointOne.Stats.NumberConnections.Increment() // 1 connection <-- this one
							pool.Put(localAZEndpointOne)

							pool.Put(localAZEndpointTwo) // 0 connections <-- or this one 10.0.1.4

							localAZEndpointThree.Stats.NumberConnections.Increment() // 1 connection <-- thisone2
							pool.Put(localAZEndpointThree)
						})

						It("selects the local endpoint with the lowest connections", func() {
							Expect(iter.Next(0)).To(Equal(localAZEndpointTwo)) // FLAKEY
						})
					})

					Context("and one is overloaded but the other is not overloaded", func() {
						BeforeEach(func() {
							pool.Put(otherAZEndpointOne)
							pool.Put(otherAZEndpointTwo)

							localAZEndpointOne.Stats.NumberConnections.Increment()
							localAZEndpointOne.Stats.NumberConnections.Increment() // overloaded
							pool.Put(localAZEndpointOne)

							pool.Put(localAZEndpointTwo) // 0 connections
						})

						It("selects the local endpoint with the lowest connections", func() {
							Expect(iter.Next(0)).To(Equal(localAZEndpointTwo))
						})
					})
				})

				Context("when the pool has one endpoint, and it is not in the same AZ as the router", func() {
					BeforeEach(func() {
						pool.Put(otherAZEndpointOne)
					})

					It("selects the non-local endpoint", func() {
						chosen := iter.Next(0)
						Expect(chosen.AvailabilityZone).ToNot(Equal(localAZ))
						Expect(chosen).To(Equal(otherAZEndpointOne))
					})
				})

				Context("when the pool has multiple endpoints, none in the same AZ as the router", func() {
					BeforeEach(func() {
						otherAZEndpointOne.Stats.NumberConnections.Increment()
						otherAZEndpointOne.Stats.NumberConnections.Increment()
						pool.Put(otherAZEndpointOne) // 2 connections

						pool.Put(otherAZEndpointTwo) // 0 connections

						otherAZEndpointThree.Stats.NumberConnections.Increment()
						pool.Put(otherAZEndpointThree) // 1 connections
					})

					It("selects the non-local endpoint", func() {
						Expect(iter.Next(0)).To(Equal(otherAZEndpointTwo))
					})
				})
			})

			Context("on a retry", func() {
				var attempt = 2
				Context("when the pool is empty", func() {
					It("does not select an endpoint", func() {
						Expect(iter.Next(attempt)).To(BeNil())
					})
				})

				Context("when the pool has some in the same AZ as the router", func() {
					BeforeEach(func() {
						otherAZEndpointOne.Stats.NumberConnections.Increment()
						pool.Put(otherAZEndpointOne) // 1 connection

						pool.Put(otherAZEndpointTwo)   // 0 connections
						pool.Put(otherAZEndpointThree) // 0 connections

						localAZEndpointOne.Stats.NumberConnections.Increment()
						pool.Put(localAZEndpointOne) // 1 connection
					})

					It("selects the endpoint with the least connections regardless of AZ", func() {
						chosen := iter.Next(attempt)
						okRandoms := []string{
							"10.0.1.1:60000",
							"10.0.1.2:60000",
						}
						Expect(chosen.AvailabilityZone).ToNot(Equal(localAZ))
						Expect(okRandoms).Should(ContainElement(chosen.CanonicalAddr()))
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
			iter := route.NewLeastConnection(logger, pool, "foo", false, false, "meow-az")
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
			iter := route.NewLeastConnection(logger, pool, "foo", false, false, "meow-az")
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
