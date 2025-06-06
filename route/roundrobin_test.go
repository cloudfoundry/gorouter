package route_test

import (
	"errors"
	"net"
	"sync"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gbytes"

	"code.cloudfoundry.org/gorouter/route"
	"code.cloudfoundry.org/gorouter/test_util"
)

var _ = Describe("RoundRobin", func() {
	var (
		pool   *route.EndpointPool
		logger *test_util.TestLogger
	)

	BeforeEach(func() {
		logger = test_util.NewTestLogger("test")
		pool = route.NewPool(&route.PoolOpts{
			Logger:             logger.Logger,
			RetryAfterFailure:  2 * time.Minute,
			Host:               "",
			ContextPath:        "",
			MaxConnsPerBackend: 0,
		})
	})

	Describe("Next", func() {
		DescribeTable("it performs round-robin through the endpoints",
			func(nextIdx int) {
				pool.NextIdx = nextIdx
				e1 := route.NewEndpoint(&route.EndpointOpts{Host: "1.2.3.4", Port: 5678})
				e2 := route.NewEndpoint(&route.EndpointOpts{Host: "5.6.7.8", Port: 1234})
				e3 := route.NewEndpoint(&route.EndpointOpts{Host: "1.2.7.8", Port: 1234})
				endpoints := []*route.Endpoint{e1, e2, e3}

				for _, e := range endpoints {
					pool.Put(e)
				}

				counts := make([]int, len(endpoints))

				iter := route.NewRoundRobin(logger.Logger, pool, "", false, false, "meow-az")

				loops := 50
				for i := 0; i < len(endpoints)*loops; i += 1 {
					n := iter.Next(i)
					for j, e := range endpoints {
						if e == n {
							counts[j]++
							break
						}
					}
				}

				for i := 0; i < len(endpoints); i++ {
					Expect(counts[i]).To(Equal(loops))
				}
			},
			Entry("When the next index is -1", -1),
			Entry("When the next index is 0", 0),
			Entry("When the next index is 1", 1),
			Entry("When the next index is 2", 2),
		)

		DescribeTable("it returns nil when no endpoints exist",
			func(nextIdx int) {
				pool.NextIdx = nextIdx
				iter := route.NewRoundRobin(logger.Logger, pool, "", false, false, "meow-az")
				e := iter.Next(0)
				Expect(e).To(BeNil())
			},
			Entry("When the next index is -1", -1),
			Entry("When the next index is 0", 0),
		)

		DescribeTable("it performs round-robin through the endpoints for two parallel-running iterators",
			func(nextIdx int) {
				pool.NextIdx = nextIdx
				e1 := route.NewEndpoint(&route.EndpointOpts{Host: "1.2.3.4", Port: 5678})
				e2 := route.NewEndpoint(&route.EndpointOpts{Host: "5.6.7.8", Port: 1234})
				endpoints := []*route.Endpoint{e1, e2}

				for _, e := range endpoints {
					pool.Put(e)
				}

				By("Create two iterators running over the same endpoint pool")
				iter1 := route.NewRoundRobin(logger.Logger, pool, "", false, false, "meow-az")
				iter2 := route.NewRoundRobin(logger.Logger, pool, "", false, false, "meow-az")

				iteratedEndpoints1 := make(map[*route.Endpoint]int)
				iteratedEndpoints2 := make(map[*route.Endpoint]int)

				By("Simulate retrying with attempts = endpoint number and count how many times both iterators iterate over every endpoint")
				for i := 0; i < len(endpoints); i += 1 {
					n := iter1.Next(i)
					k := iter2.Next(i)
					for _, e := range endpoints {
						if e == n {
							iteratedEndpoints1[e]++
						}
						if e == k {
							iteratedEndpoints2[e]++
						}
					}
				}
				By("Expect that first round robin iterator iterates over every endpoint exactly one time")
				for e := range iteratedEndpoints1 {
					Expect(iteratedEndpoints1[e]).To(Equal(1))
				}

				By("Expect that second round robin iterator iterates over every endpoint exactly one time")
				for e := range iteratedEndpoints2 {
					Expect(iteratedEndpoints2[e]).To(Equal(1))
				}
			},
			Entry("When the next index is -1", -1),
			Entry("When the next index is 0", 0),
			Entry("When the next index is 1", 1),
		)

		DescribeTable("it finds the initial endpoint by private id",
			func(nextIdx int) {
				pool.NextIdx = nextIdx
				b := route.NewEndpoint(&route.EndpointOpts{Host: "1.2.3.4", Port: 1235})
				pool.Put(route.NewEndpoint(&route.EndpointOpts{Host: "1.2.3.4", Port: 1234}))
				pool.Put(b)
				pool.Put(route.NewEndpoint(&route.EndpointOpts{Host: "1.2.3.4", Port: 1236}))
				pool.Put(route.NewEndpoint(&route.EndpointOpts{Host: "1.2.3.4", Port: 1237}))

				for i := 0; i < 10; i++ {
					iter := route.NewRoundRobin(logger.Logger, pool, b.PrivateInstanceId, false, false, "meow-az")
					e := iter.Next(i)
					Expect(e).ToNot(BeNil())
					Expect(e.PrivateInstanceId).To(Equal(b.PrivateInstanceId))
				}
			},
			Entry("When the next index is -1", -1),
			Entry("When the next index is 0", 0),
			Entry("When the next index is 1", 1),
			Entry("When the next index is 2", 2),
			Entry("When the next index is 3", 3),
		)

		DescribeTable("it finds the initial endpoint by canonical addr",
			func(nextIdx int) {
				pool.NextIdx = nextIdx
				b := route.NewEndpoint(&route.EndpointOpts{Host: "1.2.3.4", Port: 1235})
				pool.Put(route.NewEndpoint(&route.EndpointOpts{Host: "1.2.3.4", Port: 1234}))
				pool.Put(b)
				pool.Put(route.NewEndpoint(&route.EndpointOpts{Host: "1.2.3.4", Port: 1236}))
				pool.Put(route.NewEndpoint(&route.EndpointOpts{Host: "1.2.3.4", Port: 1237}))

				for i := 0; i < 10; i++ {
					iter := route.NewRoundRobin(logger.Logger, pool, b.CanonicalAddr(), false, false, "meow-az")
					e := iter.Next(i)
					Expect(e).ToNot(BeNil())
					Expect(e.CanonicalAddr()).To(Equal(b.CanonicalAddr()))
				}
			},
			Entry("When the next index is -1", -1),
			Entry("When the next index is 0", 0),
			Entry("When the next index is 1", 1),
			Entry("When the next index is 2", 2),
			Entry("When the next index is 3", 3),
		)

		DescribeTable("it finds when there are multiple private ids",
			func(nextIdx int) {
				pool.NextIdx = nextIdx
				endpointFoo := route.NewEndpoint(&route.EndpointOpts{Host: "1.2.3.4", Port: 1234, PrivateInstanceId: "foo"})
				endpointBar := route.NewEndpoint(&route.EndpointOpts{Host: "5.6.7.8", Port: 5678, PrivateInstanceId: "bar"})

				pool.Put(endpointFoo)
				pool.Put(endpointBar)

				iter := route.NewRoundRobin(logger.Logger, pool, endpointFoo.PrivateInstanceId, false, false, "meow-az")
				foundEndpoint := iter.Next(0)
				Expect(foundEndpoint).ToNot(BeNil())
				Expect(foundEndpoint).To(Equal(endpointFoo))

				iter = route.NewRoundRobin(logger.Logger, pool, endpointBar.PrivateInstanceId, false, false, "meow-az")
				foundEndpoint = iter.Next(1)
				Expect(foundEndpoint).ToNot(BeNil())
				Expect(foundEndpoint).To(Equal(endpointBar))
			},
			Entry("When the next index is -1", -1),
			Entry("When the next index is 0", 0),
			Entry("When the next index is 1", 1),
		)

		Context("when endpoint is not required to be sticky", func() {
			Context("when the initial endpoint is not found", func() {
				DescribeTable("it returns the next available endpoint",
					func(nextIdx int) {
						pool.NextIdx = nextIdx
						endpointFoo := route.NewEndpoint(&route.EndpointOpts{Host: "1.2.3.4", Port: 1234, PrivateInstanceId: "foo"})
						pool.Put(endpointFoo)

						iter := route.NewRoundRobin(logger.Logger, pool, "bogus", false, false, "meow-az")
						e := iter.Next(0)
						Expect(e).ToNot(BeNil())
						Expect(e).To(Equal(endpointFoo))
					},
					Entry("When the next index is -1", -1),
					Entry("When the next index is 0", 0),
				)
				It("logs that it chose another endpoint", func() {
					iter := route.NewRoundRobin(logger.Logger, pool, "bogus", false, false, "meow-az")
					iter.Next(0)
					Expect(logger).Should(gbytes.Say("endpoint-missing-choosing-alternate"))
				})
			})
		})

		Context("when endpoint must be sticky", func() {
			Context("when the initial endpoint is not found", func() {
				DescribeTable("it returns nil",
					func(nextIdx int) {
						pool.NextIdx = nextIdx
						endpointFoo := route.NewEndpoint(&route.EndpointOpts{Host: "1.2.3.4", Port: 1234, PrivateInstanceId: "foo"})
						pool.Put(endpointFoo)

						iter := route.NewRoundRobin(logger.Logger, pool, "bogus", true, false, "meow-az")
						e := iter.Next(0)
						Expect(e).To(BeNil())
					},
					Entry("When the next index is -1", -1),
					Entry("When the next index is 0", 0),
				)
				It("logs that it could not choose another endpoint", func() {
					iter := route.NewRoundRobin(logger.Logger, pool, "bogus", true, false, "meow-az")
					iter.Next(0)
					Expect(logger).Should(gbytes.Say("endpoint-missing-but-request-must-be-sticky"))
				})
			})
		})

		DescribeTable("it finds the correct endpoint when private ids change",
			func(nextIdx int) {
				pool.NextIdx = nextIdx
				endpointFoo := route.NewEndpoint(&route.EndpointOpts{Host: "1.2.3.4", Port: 1234, PrivateInstanceId: "foo"})
				pool.Put(endpointFoo)

				iter := route.NewRoundRobin(logger.Logger, pool, endpointFoo.PrivateInstanceId, false, false, "meow-az")
				foundEndpoint := iter.Next(0)
				Expect(foundEndpoint).ToNot(BeNil())
				Expect(foundEndpoint).To(Equal(endpointFoo))

				endpointBar := route.NewEndpoint(&route.EndpointOpts{Host: "1.2.3.4", Port: 1234, PrivateInstanceId: "bar"})
				pool.Put(endpointBar)

				iter = route.NewRoundRobin(logger.Logger, pool, "foo", false, false, "meow-az")
				foundEndpoint = iter.Next(0)
				Expect(foundEndpoint).ToNot(Equal(endpointFoo))

				iter = route.NewRoundRobin(logger.Logger, pool, "bar", false, false, "meow-az")
				foundEndpoint = iter.Next(0)
				Expect(foundEndpoint).To(Equal(endpointBar))
			},
			Entry("When the next index is -1", -1),
			Entry("When the next index is 0", 0),
		)

		It("is safe for concurrent use", func() {
			var wg sync.WaitGroup

			// these numbers need to be high in order to drive out the race condition
			const numReaders = 100
			const numEndpoints = 100
			const numGoroutines = 5

			iterateLoop := func(pool *route.EndpointPool) {
				defer GinkgoRecover()
				for j := 0; j < numReaders; j++ {
					iter := route.NewRoundRobin(logger.Logger, pool, "", false, false, "meow-az")
					Expect(iter.Next(j)).NotTo(BeNil())
				}
				wg.Done()
			}

			e1 := route.NewEndpoint(&route.EndpointOpts{Host: "1.2.3.4", Port: 5678})
			pool.Put(e1)

			for i := 0; i < numGoroutines; i++ {
				wg.Add(1)
				go func() {
					iterateLoop(pool)
				}()
			}

			for i := 0; i < numEndpoints; i++ {
				e1 := route.NewEndpoint(&route.EndpointOpts{Host: "1.2.3.4", Port: 5678})
				pool.Put(e1)
			}

			wg.Wait()
		})

		Context("when some endpoints are overloaded", func() {
			var (
				epOne, epTwo *route.Endpoint
			)

			BeforeEach(func() {
				pool = route.NewPool(&route.PoolOpts{
					Logger:             logger.Logger,
					RetryAfterFailure:  2 * time.Minute,
					Host:               "",
					ContextPath:        "",
					MaxConnsPerBackend: 2,
				})

				epOne = route.NewEndpoint(&route.EndpointOpts{Host: "5.5.5.5", Port: 5555, PrivateInstanceId: "private-label-1", UseTLS: true})
				pool.Put(epOne)
				epTwo = route.NewEndpoint(&route.EndpointOpts{Host: "2.2.2.2", Port: 2222, PrivateInstanceId: "private-label-2"})
				pool.Put(epTwo)
			})

			Context("when there is no initial endpoint", func() {
				DescribeTable("it returns an unencumbered endpoint",
					func(nextIdx int) {
						pool.NextIdx = nextIdx
						epTwo.Stats.NumberConnections.Increment()
						epTwo.Stats.NumberConnections.Increment()
						iter := route.NewRoundRobin(logger.Logger, pool, "", false, false, "meow-az")

						foundEndpoint := iter.Next(0)
						Expect(foundEndpoint).To(Equal(epOne))

						sameEndpoint := iter.Next(1)
						Expect(foundEndpoint).To(Equal(sameEndpoint))
					},
					Entry("When the next index is -1", -1),
					Entry("When the next index is 0", 0),
					Entry("When the next index is 1", 1),
				)

				Context("when all endpoints are overloaded", func() {
					DescribeTable("it returns nil",
						func(nextIdx int) {
							pool.NextIdx = nextIdx
							epOne.Stats.NumberConnections.Increment()
							epOne.Stats.NumberConnections.Increment()
							epTwo.Stats.NumberConnections.Increment()
							epTwo.Stats.NumberConnections.Increment()
							iter := route.NewRoundRobin(logger.Logger, pool, "", false, false, "meow-az")

							Consistently(func() *route.Endpoint {
								return iter.Next(0)
							}).Should(BeNil())
						},
						Entry("When the next index is -1", -1),
						Entry("When the next index is 0", 0),
						Entry("When the next index is 1", 1),
					)
				})

				Context("when only the last endpoint is overloaded, but the others have failed", func() {
					It("resets the errors on the non-overloaded enpoints and returns one of them", func() {
						pool.NextIdx = 0

						epThree := route.NewEndpoint(&route.EndpointOpts{Host: "3.3.3.3", Port: 2222, PrivateInstanceId: "private-label-2"})
						pool.Put(epThree)

						iter := route.NewRoundRobin(logger.Logger, pool, "", false, false, "meow-az")

						Expect(iter.Next(0)).To(Equal(epOne))
						iter.EndpointFailed(&net.OpError{Op: "dial"})

						Expect(iter.Next(0)).To(Equal(epThree))
						epThree.Stats.NumberConnections.Increment()

						Expect(iter.Next(0)).To(Equal(epTwo))
						iter.EndpointFailed(&net.OpError{Op: "dial"})

						Expect(iter.Next(0)).To(Equal(epThree))
						epThree.Stats.NumberConnections.Increment()

						Consistently(func() *route.Endpoint {
							selectedEndpoint := iter.Next(0)
							Expect(selectedEndpoint).NotTo(BeNil())

							return selectedEndpoint
						}).ShouldNot(Equal(epThree))
					})
				})
			})

			Context("when there is an initial endpoint", func() {
				var iter route.EndpointIterator

				Context("when the endpoint is not required to be sticky", func() {
					BeforeEach(func() {
						iter = route.NewRoundRobin(logger.Logger, pool, "private-label-1", false, false, "meow-az")
					})

					Context("when the initial endpoint is overloaded", func() {
						BeforeEach(func() {
							epOne.Stats.NumberConnections.Increment()
							epOne.Stats.NumberConnections.Increment()
						})

						Context("when there is an unencumbered endpoint", func() {
							DescribeTable("it returns the unencumbered endpoint",
								func(nextIdx int) {
									pool.NextIdx = nextIdx
									Expect(iter.Next(0)).To(Equal(epTwo))
									Expect(iter.Next(1)).To(Equal(epTwo))
								},
								Entry("When the next index is -1", -1),
								Entry("When the next index is 0", 0),
								Entry("When the next index is 1", 1),
							)
						})

						Context("when there isn't an unencumbered endpoint", func() {
							BeforeEach(func() {
								epTwo.Stats.NumberConnections.Increment()
								epTwo.Stats.NumberConnections.Increment()
							})

							DescribeTable("it returns nil",
								func(nextIdx int) {
									pool.NextIdx = nextIdx
									Consistently(func() *route.Endpoint {
										return iter.Next(0)
									}).Should(BeNil())
								},
								Entry("When the next index is -1", -1),
								Entry("When the next index is 0", 0),
								Entry("When the next index is 1", 1),
							)
						})
					})
				})

				Context("when the endpoint must be sticky", func() {
					BeforeEach(func() {
						iter = route.NewRoundRobin(logger.Logger, pool, "private-label-1", true, false, "meow-az")
					})

					Context("when the initial endpoint is overloaded", func() {
						BeforeEach(func() {
							epOne.Stats.NumberConnections.Increment()
							epOne.Stats.NumberConnections.Increment()
						})

						Context("when there is an unencumbered endpoint", func() {
							DescribeTable("it returns nil",
								func(nextIdx int) {
									pool.NextIdx = nextIdx
									Consistently(func() *route.Endpoint {
										return iter.Next(0)
									}).Should(BeNil())
								},
								Entry("When the next index is -1", -1),
								Entry("When the next index is 0", 0),
								Entry("When the next index is 1", 1),
							)

							It("logs that it could not choose another endpoint", func() {
								iter.Next(0)
								Expect(logger).Should(gbytes.Say("endpoint-overloaded-but-request-must-be-sticky"))
							})
						})

						Context("when there isn't an unencumbered endpoint", func() {
							BeforeEach(func() {
								epTwo.Stats.NumberConnections.Increment()
								epTwo.Stats.NumberConnections.Increment()
							})

							DescribeTable("it returns nil",
								func(nextIdx int) {
									pool.NextIdx = nextIdx
									Consistently(func() *route.Endpoint {
										return iter.Next(0)
									}).Should(BeNil())
								},
								Entry("When the next index is -1", -1),
								Entry("When the next index is 0", 0),
								Entry("When the next index is 1", 1),
							)
							It("logs that it could not choose another endpoint", func() {
								iter.Next(0)
								Expect(logger).Should(gbytes.Say("endpoint-overloaded-but-request-must-be-sticky"))
							})
						})
					})

					Context("when initial endpoint becomes overloaded", func() {
						It("doesn't mark endpoint as failed", func() {
							Expect(pool.NumEndpoints()).To(Equal(2))
							Expect(iter.Next(0)).To(Equal(epOne))

							epOne.Stats.NumberConnections.Increment()
							epOne.Stats.NumberConnections.Increment()

							Expect(iter.Next(0)).To(BeNil())

							Expect(pool.NumEndpoints()).To(Equal(2))

							epOne.Stats.NumberConnections.Decrement()
							epOne.Stats.NumberConnections.Decrement()

							Expect(iter.Next(0)).To(Equal(epOne))
						})
					})
				})
			})
		})

		Describe("when locally-optimistic mode", func() {
			var (
				iter                                                         route.EndpointIterator
				localAZ                                                      = "az-2"
				otherAZEndpointOne, otherAZEndpointTwo, otherAZEndpointThree *route.Endpoint
				localAZEndpointOne, localAZEndpointTwo, localAZEndpointThree *route.Endpoint
			)

			BeforeEach(func() {
				pool = route.NewPool(&route.PoolOpts{
					Logger:             logger.Logger,
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
				iter = route.NewRoundRobin(logger.Logger, pool, "", false, true, localAZ)
			})

			Context("on the first attempt", func() {
				Context("when the pool is empty", func() {
					DescribeTable("it does not select an endpoint",
						func(nextIdx int) {
							pool.NextIdx = nextIdx
							Expect(iter.Next(0)).To(BeNil())
						},
						Entry("When the next index is -1", -1),
						Entry("When the next index is 0", 0),
						Entry("When the next index is 1", 1),
						Entry("When the next index is 2", 2),
						Entry("When the next index is 3", 3),
					)
				})

				Context("when the pool has one endpoint in the same AZ as the router", func() {
					BeforeEach(func() {
						pool.Put(otherAZEndpointOne)
						pool.Put(otherAZEndpointTwo)
						pool.Put(otherAZEndpointThree)
						pool.Put(localAZEndpointOne)
					})

					DescribeTable("it returns that endpoint",
						func(nextIdx int) {
							pool.NextIdx = nextIdx
							chosen := iter.Next(0)
							Expect(chosen.AvailabilityZone).To(Equal(localAZ))
							Expect(chosen).To(Equal(localAZEndpointOne))
						},
						Entry("When the next index is -1", -1),
						Entry("When the next index is 0", 0),
						Entry("When the next index is 1", 1),
						Entry("When the next index is 2", 2),
						Entry("When the next index is 3", 3),
					)

					Context("and it is overloaded", func() {
						BeforeEach(func() {
							localAZEndpointOne.Stats.NumberConnections.Increment()
							localAZEndpointOne.Stats.NumberConnections.Increment()
						})

						DescribeTable("it selects the next non-overloaded endpoint in a different az",
							func(nextIdx int) {
								pool.NextIdx = nextIdx
								chosen := iter.Next(0)
								Expect(chosen).ToNot(BeNil())
								Expect(chosen.AvailabilityZone).ToNot(Equal(localAZ))
							},
							Entry("When the next index is -1", -1),
							Entry("When the next index is 0", 0),
							Entry("When the next index is 1", 1),
							Entry("When the next index is 2", 2),
							Entry("When the next index is 3", 3),
						)
					})
				})

				Context("when the pool has multiple in the same AZ as the router", func() {
					BeforeEach(func() {
						pool.Put(otherAZEndpointOne)
						pool.Put(otherAZEndpointTwo)
						pool.Put(otherAZEndpointThree)

						pool.Put(localAZEndpointOne)
						pool.Put(localAZEndpointTwo)
						pool.Put(localAZEndpointThree)
					})

					DescribeTable("it selects the next endpoint in the same AZ",
						func(nextIdx int) {
							pool.NextIdx = nextIdx
							okRandoms := []string{
								"10.0.1.3:60000",
								"10.0.1.4:60000",
								"10.0.1.5:60000",
							}

							chosen := iter.Next(0)
							Expect(chosen.AvailabilityZone).To(Equal(localAZ))
							Expect(okRandoms).Should(ContainElement(chosen.CanonicalAddr()))
						},
						Entry("When the next index is -1", -1),
						Entry("When the next index is 0", 0),
						Entry("When the next index is 1", 1),
						Entry("When the next index is 2", 2),
						Entry("When the next index is 3", 3),
						Entry("When the next index is 4", 4),
						Entry("When the next index is 5", 5),
					)

					Context("and one is overloaded but the other is not overloaded", func() {
						BeforeEach(func() {
							localAZEndpointOne.Stats.NumberConnections.Increment()
							localAZEndpointOne.Stats.NumberConnections.Increment() // overloaded
						})

						DescribeTable("it selects the local endpoint that is not overloaded",
							func(nextIdx int) {
								pool.NextIdx = nextIdx
								okRandoms := []string{
									"10.0.1.4:60000",
									"10.0.1.5:60000",
								}

								chosen := iter.Next(0)
								Expect(chosen.AvailabilityZone).To(Equal(localAZ))
								Expect(okRandoms).Should(ContainElement(chosen.CanonicalAddr()))
							},
							Entry("When the next index is -1", -1),
							Entry("When the next index is 0", 0),
							Entry("When the next index is 1", 1),
							Entry("When the next index is 2", 2),
							Entry("When the next index is 3", 3),
							Entry("When the next index is 4", 4),
							Entry("When the next index is 5", 5),
						)
					})

					Context("when one has an error and the other does not", func() {
						JustBeforeEach(func() {
							pool.NextIdx = 3
							Expect(iter.Next(0)).To(Equal(localAZEndpointOne))
							iter.EndpointFailed(&net.OpError{Op: "dial"})
						})

						DescribeTable("it returns the endpoint without an error in the same AZ",
							func(nextIdx int) {
								pool.NextIdx = nextIdx
								okRandoms := []string{
									"10.0.1.4:60000",
									"10.0.1.5:60000",
								}

								chosen := iter.Next(0)
								Expect(chosen.AvailabilityZone).To(Equal(localAZ))
								Expect(okRandoms).Should(ContainElement(chosen.CanonicalAddr()))
							},
							Entry("When the next index is -1", -1),
							Entry("When the next index is 0", 0),
							Entry("When the next index is 1", 1),
							Entry("When the next index is 2", 2),
							Entry("When the next index is 3", 3),
							Entry("When the next index is 4", 4),
							Entry("When the next index is 5", 5),
						)
					})

					Context("when all AZ-local endpoints have errors", func() {
						JustBeforeEach(func() {
							pool.NextIdx = 3
							Expect(iter.Next(0)).To(Equal(localAZEndpointOne))
							iter.EndpointFailed(&net.OpError{Op: "dial"})

							Expect(iter.Next(0)).To(Equal(localAZEndpointTwo))
							iter.EndpointFailed(&net.OpError{Op: "dial"})

							Expect(iter.Next(0)).To(Equal(localAZEndpointThree))
							iter.EndpointFailed(&net.OpError{Op: "dial"})
						})

						DescribeTable("it resets the errors and returns one of the endpoints regardless of AZ",
							func(nextIdx int) {
								pool.NextIdx = nextIdx
								nonLocalEndpoints := []string{
									otherAZEndpointOne.CanonicalAddr(),
									otherAZEndpointTwo.CanonicalAddr(),
									otherAZEndpointThree.CanonicalAddr(),
								}
								chosen := iter.Next(0)
								Expect(nonLocalEndpoints).Should(ContainElement(chosen.CanonicalAddr()))
							},
							Entry("When the next index is -1", -1),
							Entry("When the next index is 0", 0),
						)

					})
				})

				Context("when the pool has one endpoint, and it is not in the same AZ as the router", func() {
					BeforeEach(func() {
						pool.Put(otherAZEndpointOne)
					})

					DescribeTable("it selects a non-local endpoint",
						func(nextIdx int) {
							pool.NextIdx = nextIdx
							chosen := iter.Next(0)
							Expect(chosen).ToNot(BeNil())
							Expect(chosen.AvailabilityZone).ToNot(Equal(localAZ))
							Expect(chosen).To(Equal(otherAZEndpointOne))
						},
						Entry("When the next index is -1", -1),
						Entry("When the next index is 0", 0),
					)
				})

				Context("when the pool has multiple endpoints, none in the same AZ as the router", func() {
					BeforeEach(func() {
						pool.Put(otherAZEndpointOne)
						pool.Put(otherAZEndpointTwo)
						pool.Put(otherAZEndpointThree)
					})

					DescribeTable("it selects a non-local endpoint",
						func(nextIdx int) {
							pool.NextIdx = nextIdx
							chosen := iter.Next(0)
							Expect(chosen).ToNot(BeNil())
							Expect(chosen.AvailabilityZone).ToNot(Equal(localAZ))
						},
						Entry("When the next index is -1", -1),
						Entry("When the next index is 0", 0),
						Entry("When the next index is 1", 1),
						Entry("When the next index is 2", 2),
					)
				})
			})

			Context("on a retry", func() {
				var attempt = 1
				Context("when the pool is empty", func() {
					It("does not select an endpoint", func() {
						Expect(iter.Next(attempt)).To(BeNil())
					})
				})

				Context("when the pool has some endpoints in the same AZ as the router", func() {
					DescribeTable("it selects a non-local endpoint",
						func(nextIdx int) {
							pool.NextIdx = nextIdx
							endpoints := []*route.Endpoint{
								otherAZEndpointOne, otherAZEndpointTwo, otherAZEndpointThree,
								localAZEndpointOne, localAZEndpointTwo, localAZEndpointThree,
							}

							for _, e := range endpoints {
								pool.Put(e)
							}

							counts := make([]int, len(endpoints))

							iter := route.NewRoundRobin(logger.Logger, pool, "", false, true, localAZ)

							loops := 50
							for i := 0; i < len(endpoints)*loops; i += 1 {
								n := iter.Next(attempt)
								for j, e := range endpoints {
									if e == n {
										counts[j]++
										break
									}
								}
							}

							for i := 0; i < len(endpoints); i++ {
								Expect(counts[i]).To(Equal(loops))
							}
						},
						Entry("When the next index is -1", -1),
						Entry("When the next index is 0", 0),
						Entry("When the next index is 1", 1),
						Entry("When the next index is 2", 2),
						Entry("When the next index is 3", 3),
						Entry("When the next index is 4", 4),
						Entry("When the next index is 5", 5),
					)
				})
			})
		})
	})

	Describe("Failed", func() {
		DescribeTable("it skips failed endpoints",
			func(nextIdx int) {
				pool.NextIdx = nextIdx
				e1 := route.NewEndpoint(&route.EndpointOpts{Host: "1.2.3.4", Port: 1234})
				e2 := route.NewEndpoint(&route.EndpointOpts{Host: "5.6.7.8", Port: 5678})

				pool.Put(e1)
				pool.Put(e2)

				iter := route.NewRoundRobin(logger.Logger, pool, "", false, false, "meow-az")
				n := iter.Next(0)
				Expect(n).ToNot(BeNil())

				iter.EndpointFailed(&net.OpError{Op: "dial"})

				nn1 := iter.Next(1)
				nn2 := iter.Next(2)
				Expect(nn1).ToNot(BeNil())
				Expect(nn2).ToNot(BeNil())
				Expect(nn1).ToNot(Equal(n))
				Expect(nn1).To(Equal(nn2))
			},
			Entry("When the next index is -1", -1),
			Entry("When the next index is 0", 0),
			Entry("When the next index is 1", 1),
		)

		DescribeTable("it resets when all endpoints are failed",
			func(nextIdx int) {
				pool.NextIdx = nextIdx
				e1 := route.NewEndpoint(&route.EndpointOpts{Host: "1.2.3.4", Port: 1234})
				e2 := route.NewEndpoint(&route.EndpointOpts{Host: "5.6.7.8", Port: 5678})
				pool.Put(e1)
				pool.Put(e2)

				iter := route.NewRoundRobin(logger.Logger, pool, "", false, false, "meow-az")
				n1 := iter.Next(0)
				iter.EndpointFailed(&net.OpError{Op: "dial"})
				n2 := iter.Next(1)
				iter.EndpointFailed(&net.OpError{Op: "remote error", Err: errors.New("tls: bad certificate")})
				Expect(n1).ToNot(Equal(n2))

				n1 = iter.Next(2)
				n2 = iter.Next(3)
				Expect(n1).ToNot(Equal(n2))
			},
			Entry("When the next index is -1", -1),
			Entry("When the next index is 0", 0),
			Entry("When the next index is 1", 1),
		)

		DescribeTable("it resets failed endpoints after exceeding failure duration",
			func(nextIdx int) {
				pool = route.NewPool(&route.PoolOpts{
					Logger:             logger.Logger,
					RetryAfterFailure:  50 * time.Millisecond,
					Host:               "",
					ContextPath:        "",
					MaxConnsPerBackend: 0,
				})
				pool.NextIdx = nextIdx

				e1 := route.NewEndpoint(&route.EndpointOpts{Host: "1.2.3.4", Port: 1234})
				e2 := route.NewEndpoint(&route.EndpointOpts{Host: "5.6.7.8", Port: 5678})
				pool.Put(e1)
				pool.Put(e2)

				iter := route.NewRoundRobin(logger.Logger, pool, "", false, false, "meow-az")
				n1 := iter.Next(0)
				n2 := iter.Next(1)
				Expect(n1).ToNot(Equal(n2))

				iter.EndpointFailed(&net.OpError{Op: "read", Err: errors.New("read: connection reset by peer")})

				n1 = iter.Next(2)
				n2 = iter.Next(3)
				Expect(n1).To(Equal(n2))

				time.Sleep(50 * time.Millisecond)

				n1 = iter.Next(4)
				n2 = iter.Next(5)
				Expect(n1).ToNot(Equal(n2))
			},
			Entry("When the next index is -1", -1),
			Entry("When the next index is 0", 0),
			Entry("When the next index is 1", 1),
		)
	})

	Context("PreRequest", func() {
		It("increments the NumberConnections counter", func() {
			endpointFoo := route.NewEndpoint(&route.EndpointOpts{Host: "1.2.3.4", Port: 1234, PrivateInstanceId: "foo"})
			Expect(endpointFoo.Stats.NumberConnections.Count()).To(Equal(int64(0)))
			pool.Put(endpointFoo)
			iter := route.NewRoundRobin(logger.Logger, pool, "foo", false, false, "meow-az")
			iter.PreRequest(endpointFoo)
			Expect(endpointFoo.Stats.NumberConnections.Count()).To(Equal(int64(1)))
		})
	})

	Context("PostRequest", func() {
		It("decrements the NumberConnections counter", func() {
			endpointFoo := route.NewEndpoint(&route.EndpointOpts{Host: "1.2.3.4", Port: 1234, PrivateInstanceId: "foo"})
			endpointFoo.Stats = &route.Stats{
				NumberConnections: route.NewCounter(int64(1)),
			}
			Expect(endpointFoo.Stats.NumberConnections.Count()).To(Equal(int64(1)))
			pool.Put(endpointFoo)
			iter := route.NewRoundRobin(logger.Logger, pool, "foo", false, false, "meow-az")
			iter.PostRequest(endpointFoo)
			Expect(endpointFoo.Stats.NumberConnections.Count()).To(Equal(int64(0)))
		})
	})
})
