package route_test

import (
	"time"

	"code.cloudfoundry.org/gorouter/route"
	"code.cloudfoundry.org/routing-api/models"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("RoundRobin", func() {
	var pool *route.Pool
	var modTag models.ModificationTag

	BeforeEach(func() {
		pool = route.NewPool(2*time.Minute, "", "")
		modTag = models.ModificationTag{}
	})

	Describe("Next", func() {
		It("performs round-robin through the endpoints", func() {
			e1 := route.NewEndpoint("", "1.2.3.4", 5678, "", "", nil, -1, "", modTag, "", false)
			e2 := route.NewEndpoint("", "5.6.7.8", 1234, "", "", nil, -1, "", modTag, "", false)
			e3 := route.NewEndpoint("", "1.2.7.8", 1234, "", "", nil, -1, "", modTag, "", false)
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
			b := route.NewEndpoint("", "1.2.3.4", 1235, "b", "", nil, -1, "", modTag, "", false)
			pool.Put(route.NewEndpoint("", "1.2.3.4", 1234, "a", "", nil, -1, "", modTag, "", false))
			pool.Put(b)
			pool.Put(route.NewEndpoint("", "1.2.3.4", 1236, "c", "", nil, -1, "", modTag, "", false))
			pool.Put(route.NewEndpoint("", "1.2.3.4", 1237, "d", "", nil, -1, "", modTag, "", false))

			for i := 0; i < 10; i++ {
				iter := route.NewRoundRobin(pool, b.PrivateInstanceId)
				e := iter.Next()
				Expect(e).ToNot(BeNil())
				Expect(e.PrivateInstanceId).To(Equal(b.PrivateInstanceId))
			}
		})

		It("finds the initial endpoint by canonical addr", func() {
			b := route.NewEndpoint("", "1.2.3.4", 1235, "b", "", nil, -1, "", modTag, "", false)
			pool.Put(route.NewEndpoint("", "1.2.3.4", 1234, "a", "", nil, -1, "", modTag, "", false))
			pool.Put(b)
			pool.Put(route.NewEndpoint("", "1.2.3.4", 1236, "c", "", nil, -1, "", modTag, "", false))
			pool.Put(route.NewEndpoint("", "1.2.3.4", 1237, "d", "", nil, -1, "", modTag, "", false))

			for i := 0; i < 10; i++ {
				iter := route.NewRoundRobin(pool, b.CanonicalAddr())
				e := iter.Next()
				Expect(e).ToNot(BeNil())
				Expect(e.CanonicalAddr()).To(Equal(b.CanonicalAddr()))
			}
		})

		It("finds when there are multiple private ids", func() {
			endpointFoo := route.NewEndpoint("", "1.2.3.4", 1234, "foo", "", nil, -1, "", modTag, "", false)
			endpointBar := route.NewEndpoint("", "5.6.7.8", 5678, "bar", "", nil, -1, "", modTag, "", false)

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
			eFoo := route.NewEndpoint("", "1.2.3.4", 1234, "foo", "", nil, -1, "", modTag, "", false)
			pool.Put(eFoo)

			iter := route.NewRoundRobin(pool, "bogus")
			e := iter.Next()
			Expect(e).ToNot(BeNil())
			Expect(e).To(Equal(eFoo))
		})

		It("finds the correct endpoint when private ids change", func() {
			endpointFoo := route.NewEndpoint("", "1.2.3.4", 1234, "foo", "", nil, -1, "", modTag, "", false)
			pool.Put(endpointFoo)

			iter := route.NewRoundRobin(pool, endpointFoo.PrivateInstanceId)
			foundEndpoint := iter.Next()
			Expect(foundEndpoint).ToNot(BeNil())
			Expect(foundEndpoint).To(Equal(endpointFoo))

			endpointBar := route.NewEndpoint("", "1.2.3.4", 1234, "bar", "", nil, -1, "", modTag, "", false)
			pool.Put(endpointBar)

			iter = route.NewRoundRobin(pool, "foo")
			foundEndpoint = iter.Next()
			Expect(foundEndpoint).ToNot(Equal(endpointFoo))

			iter = route.NewRoundRobin(pool, "bar")
			Expect(foundEndpoint).To(Equal(endpointBar))
		})

	})

	Describe("Failed", func() {
		It("skips failed endpoints", func() {
			e1 := route.NewEndpoint("", "1.2.3.4", 5678, "", "", nil, -1, "", modTag, "", false)
			e2 := route.NewEndpoint("", "5.6.7.8", 1234, "", "", nil, -1, "", modTag, "", false)
			pool.Put(e1)
			pool.Put(e2)

			iter := route.NewRoundRobin(pool, "")
			n := iter.Next()
			Expect(n).ToNot(BeNil())

			iter.EndpointFailed()

			nn1 := iter.Next()
			nn2 := iter.Next()
			Expect(nn1).ToNot(BeNil())
			Expect(nn2).ToNot(BeNil())
			Expect(nn1).ToNot(Equal(n))
			Expect(nn1).To(Equal(nn2))
		})

		It("resets when all endpoints are failed", func() {
			e1 := route.NewEndpoint("", "1.2.3.4", 5678, "", "", nil, -1, "", modTag, "", false)
			e2 := route.NewEndpoint("", "5.6.7.8", 1234, "", "", nil, -1, "", modTag, "", false)
			pool.Put(e1)
			pool.Put(e2)

			iter := route.NewRoundRobin(pool, "")
			n1 := iter.Next()
			iter.EndpointFailed()
			n2 := iter.Next()
			iter.EndpointFailed()
			Expect(n1).ToNot(Equal(n2))

			n1 = iter.Next()
			n2 = iter.Next()
			Expect(n1).ToNot(Equal(n2))
		})

		It("resets failed endpoints after exceeding failure duration", func() {
			pool = route.NewPool(50*time.Millisecond, "", "")

			e1 := route.NewEndpoint("", "1.2.3.4", 5678, "", "", nil, -1, "", modTag, "", false)
			e2 := route.NewEndpoint("", "5.6.7.8", 1234, "", "", nil, -1, "", modTag, "", false)
			pool.Put(e1)
			pool.Put(e2)

			iter := route.NewRoundRobin(pool, "")
			n1 := iter.Next()
			n2 := iter.Next()
			Expect(n1).ToNot(Equal(n2))

			iter.EndpointFailed()

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
			endpointFoo := route.NewEndpoint("", "1.2.3.4", 1234, "foo", "", nil, -1, "", modTag, "", false)
			Expect(endpointFoo.Stats.NumberConnections.Count()).To(Equal(int64(0)))
			pool.Put(endpointFoo)
			iter := route.NewRoundRobin(pool, "foo")
			iter.PreRequest(endpointFoo)
			Expect(endpointFoo.Stats.NumberConnections.Count()).To(Equal(int64(1)))
		})
	})

	Context("PostRequest", func() {
		It("decrements the NumberConnections counter", func() {
			endpointFoo := route.NewEndpoint("", "1.2.3.4", 1234, "foo", "", nil, -1, "", modTag, "", false)
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
