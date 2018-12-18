package route_test

import (
	"errors"
	"net"
	"time"

	"code.cloudfoundry.org/gorouter/route"
	"code.cloudfoundry.org/gorouter/test_util"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("RoundRobin", func() {
	var pool *route.Pool

	BeforeEach(func() {
		pool = route.NewPool(&route.PoolOpts{
			Logger:             test_util.NewTestZapLogger("test"),
			RetryAfterFailure:  2 * time.Minute,
			Host:               "",
			ContextPath:        "",
			MaxConnsPerBackend: 0,
		})
	})

	Describe("Next", func() {
		It("performs round-robin through the endpoints", func() {
			e1 := route.NewEndpoint(&route.EndpointOpts{Host: "1.2.3.4", Port: 5678})
			e2 := route.NewEndpoint(&route.EndpointOpts{Host: "5.6.7.8", Port: 1234})
			e3 := route.NewEndpoint(&route.EndpointOpts{Host: "1.2.7.8", Port: 1234})
			endpoints := []*route.Endpoint{e1, e2, e3}

			for _, e := range endpoints {
				pool.Put(e)
			}

			counts := make([]int, len(endpoints))

			iter := route.NewRoundRobin(pool, "")

			loops := 50
			for i := 0; i < len(endpoints)*loops; i += 1 {
				n := iter.Next()
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
		})

		It("returns nil when no endpoints exist", func() {
			iter := route.NewRoundRobin(pool, "")
			e := iter.Next()
			Expect(e).To(BeNil())
		})

		It("finds the initial endpoint by private id", func() {
			b := route.NewEndpoint(&route.EndpointOpts{Host: "1.2.3.4", Port: 1235})
			pool.Put(route.NewEndpoint(&route.EndpointOpts{Host: "1.2.3.4", Port: 1234}))
			pool.Put(b)
			pool.Put(route.NewEndpoint(&route.EndpointOpts{Host: "1.2.3.4", Port: 1236}))
			pool.Put(route.NewEndpoint(&route.EndpointOpts{Host: "1.2.3.4", Port: 1237}))

			for i := 0; i < 10; i++ {
				iter := route.NewRoundRobin(pool, b.PrivateInstanceId)
				e := iter.Next()
				Expect(e).ToNot(BeNil())
				Expect(e.PrivateInstanceId).To(Equal(b.PrivateInstanceId))
			}
		})

		It("finds the initial endpoint by canonical addr", func() {
			b := route.NewEndpoint(&route.EndpointOpts{Host: "1.2.3.4", Port: 1235})
			pool.Put(route.NewEndpoint(&route.EndpointOpts{Host: "1.2.3.4", Port: 1234}))
			pool.Put(b)
			pool.Put(route.NewEndpoint(&route.EndpointOpts{Host: "1.2.3.4", Port: 1236}))
			pool.Put(route.NewEndpoint(&route.EndpointOpts{Host: "1.2.3.4", Port: 1237}))

			for i := 0; i < 10; i++ {
				iter := route.NewRoundRobin(pool, b.CanonicalAddr())
				e := iter.Next()
				Expect(e).ToNot(BeNil())
				Expect(e.CanonicalAddr()).To(Equal(b.CanonicalAddr()))
			}
		})

		It("finds when there are multiple private ids", func() {
			endpointFoo := route.NewEndpoint(&route.EndpointOpts{Host: "1.2.3.4", Port: 1234, PrivateInstanceId: "foo"})
			endpointBar := route.NewEndpoint(&route.EndpointOpts{Host: "5.6.7.8", Port: 5678, PrivateInstanceId: "bar"})

			pool.Put(endpointFoo)
			pool.Put(endpointBar)

			iter := route.NewRoundRobin(pool, endpointFoo.PrivateInstanceId)
			foundEndpoint := iter.Next()
			Expect(foundEndpoint).ToNot(BeNil())
			Expect(foundEndpoint).To(Equal(endpointFoo))

			iter = route.NewRoundRobin(pool, endpointBar.PrivateInstanceId)
			foundEndpoint = iter.Next()
			Expect(foundEndpoint).ToNot(BeNil())
			Expect(foundEndpoint).To(Equal(endpointBar))
		})

		It("returns the next available endpoint when the initial is not found", func() {
			endpointFoo := route.NewEndpoint(&route.EndpointOpts{Host: "1.2.3.4", Port: 1234, PrivateInstanceId: "foo"})
			pool.Put(endpointFoo)

			iter := route.NewRoundRobin(pool, "bogus")
			e := iter.Next()
			Expect(e).ToNot(BeNil())
			Expect(e).To(Equal(endpointFoo))
		})

		It("finds the correct endpoint when private ids change", func() {
			endpointFoo := route.NewEndpoint(&route.EndpointOpts{Host: "1.2.3.4", Port: 1234, PrivateInstanceId: "foo"})
			pool.Put(endpointFoo)

			iter := route.NewRoundRobin(pool, endpointFoo.PrivateInstanceId)
			foundEndpoint := iter.Next()
			Expect(foundEndpoint).ToNot(BeNil())
			Expect(foundEndpoint).To(Equal(endpointFoo))

			endpointBar := route.NewEndpoint(&route.EndpointOpts{Host: "1.2.3.4", Port: 1234, PrivateInstanceId: "bar"})
			pool.Put(endpointBar)

			iter = route.NewRoundRobin(pool, "foo")
			foundEndpoint = iter.Next()
			Expect(foundEndpoint).ToNot(Equal(endpointFoo))

			iter = route.NewRoundRobin(pool, "bar")
			Expect(foundEndpoint).To(Equal(endpointBar))
		})

		Context("when some endpoints are overloaded", func() {
			var (
				epOne, epTwo *route.Endpoint
			)

			BeforeEach(func() {
				pool = route.NewPool(&route.PoolOpts{
					Logger:             test_util.NewTestZapLogger("test"),
					RetryAfterFailure:  2 * time.Minute,
					Host:               "",
					ContextPath:        "",
					MaxConnsPerBackend: 2,
				})

				epOne = route.NewEndpoint(&route.EndpointOpts{Host: "5.5.5.5", Port: 5555, PrivateInstanceId: "private-label-1"})
				pool.Put(epOne)
				epTwo = route.NewEndpoint(&route.EndpointOpts{Host: "2.2.2.2", Port: 2222, PrivateInstanceId: "private-label-2"})
				pool.Put(epTwo)
			})

			Context("when there is no initial endpoint", func() {
				It("returns an unencumbered endpoint", func() {
					epTwo.Stats.NumberConnections.Increment()
					epTwo.Stats.NumberConnections.Increment()
					iter := route.NewRoundRobin(pool, "")

					foundEndpoint := iter.Next()
					Expect(foundEndpoint).To(Equal(epOne))

					sameEndpoint := iter.Next()
					Expect(foundEndpoint).To(Equal(sameEndpoint))
				})

				Context("when all endpoints are overloaded", func() {
					It("returns nil", func() {
						epOne.Stats.NumberConnections.Increment()
						epOne.Stats.NumberConnections.Increment()
						epTwo.Stats.NumberConnections.Increment()
						epTwo.Stats.NumberConnections.Increment()
						iter := route.NewRoundRobin(pool, "")

						Consistently(func() *route.Endpoint {
							return iter.Next()
						}).Should(BeNil())
					})
				})
			})

			Context("when there is an initial endpoint", func() {
				var iter route.EndpointIterator
				BeforeEach(func() {
					iter = route.NewRoundRobin(pool, "private-label-1")
				})

				Context("when the initial endpoint is overloaded", func() {
					BeforeEach(func() {
						epOne.Stats.NumberConnections.Increment()
						epOne.Stats.NumberConnections.Increment()
					})

					Context("when there is an unencumbered endpoint", func() {
						It("returns the unencumbered endpoint", func() {
							Expect(iter.Next()).To(Equal(epTwo))
							Expect(iter.Next()).To(Equal(epTwo))
						})
					})

					Context("when there isn't an unencumbered endpoint", func() {
						BeforeEach(func() {
							epTwo.Stats.NumberConnections.Increment()
							epTwo.Stats.NumberConnections.Increment()
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

	Describe("Failed", func() {
		It("skips failed endpoints", func() {
			e1 := route.NewEndpoint(&route.EndpointOpts{Host: "1.2.3.4", Port: 1234})
			e2 := route.NewEndpoint(&route.EndpointOpts{Host: "5.6.7.8", Port: 5678})

			pool.Put(e1)
			pool.Put(e2)

			iter := route.NewRoundRobin(pool, "")
			n := iter.Next()
			Expect(n).ToNot(BeNil())

			iter.EndpointFailed(&net.OpError{Op: "dial"})

			nn1 := iter.Next()
			nn2 := iter.Next()
			Expect(nn1).ToNot(BeNil())
			Expect(nn2).ToNot(BeNil())
			Expect(nn1).ToNot(Equal(n))
			Expect(nn1).To(Equal(nn2))
		})

		It("resets when all endpoints are failed", func() {
			e1 := route.NewEndpoint(&route.EndpointOpts{Host: "1.2.3.4", Port: 1234})
			e2 := route.NewEndpoint(&route.EndpointOpts{Host: "5.6.7.8", Port: 5678})
			pool.Put(e1)
			pool.Put(e2)

			iter := route.NewRoundRobin(pool, "")
			n1 := iter.Next()
			iter.EndpointFailed(&net.OpError{Op: "dial"})
			n2 := iter.Next()
			iter.EndpointFailed(&net.OpError{Op: "remote error", Err: errors.New("tls: bad certificate")})
			Expect(n1).ToNot(Equal(n2))

			n1 = iter.Next()
			n2 = iter.Next()
			Expect(n1).ToNot(Equal(n2))
		})

		It("resets failed endpoints after exceeding failure duration", func() {
			pool = route.NewPool(&route.PoolOpts{
				Logger:             test_util.NewTestZapLogger("test"),
				RetryAfterFailure:  50 * time.Millisecond,
				Host:               "",
				ContextPath:        "",
				MaxConnsPerBackend: 0,
			})

			e1 := route.NewEndpoint(&route.EndpointOpts{Host: "1.2.3.4", Port: 1234})
			e2 := route.NewEndpoint(&route.EndpointOpts{Host: "5.6.7.8", Port: 5678})
			pool.Put(e1)
			pool.Put(e2)

			iter := route.NewRoundRobin(pool, "")
			n1 := iter.Next()
			n2 := iter.Next()
			Expect(n1).ToNot(Equal(n2))

			iter.EndpointFailed(&net.OpError{Op: "read", Err: errors.New("read: connection reset by peer")})

			n1 = iter.Next()
			n2 = iter.Next()
			Expect(n1).To(Equal(n2))

			time.Sleep(50 * time.Millisecond)

			n1 = iter.Next()
			n2 = iter.Next()
			Expect(n1).ToNot(Equal(n2))
		})
	})

	Context("PreRequest", func() {
		It("increments the NumberConnections counter", func() {
			endpointFoo := route.NewEndpoint(&route.EndpointOpts{Host: "1.2.3.4", Port: 1234, PrivateInstanceId: "foo"})
			Expect(endpointFoo.Stats.NumberConnections.Count()).To(Equal(int64(0)))
			pool.Put(endpointFoo)
			iter := route.NewRoundRobin(pool, "foo")
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
			iter := route.NewRoundRobin(pool, "foo")
			iter.PostRequest(endpointFoo)
			Expect(endpointFoo.Stats.NumberConnections.Count()).To(Equal(int64(0)))
		})
	})
})
