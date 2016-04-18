package route_test

import (
	"time"

	"github.com/cloudfoundry/gorouter/route"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("EndpointIterator", func() {
	var pool *route.Pool

	BeforeEach(func() {
		pool = route.NewPool(2*time.Minute, "")
	})

	Describe("Next", func() {
		It("performs round-robin through the endpoints", func() {
			e1 := route.NewEndpoint("", "1.2.3.4", 5678, "", nil, -1, "")
			e2 := route.NewEndpoint("", "5.6.7.8", 1234, "", nil, -1, "")
			e3 := route.NewEndpoint("", "1.2.7.8", 1234, "", nil, -1, "")
			endpoints := []*route.Endpoint{e1, e2, e3}

			for _, e := range endpoints {
				pool.Put(e)
			}

			counts := make([]int, len(endpoints))

			iter := pool.Endpoints("")

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
			iter := pool.Endpoints("")
			e := iter.Next()
			Expect(e).To(BeNil())
		})

		It("finds the initial endpoint by private id", func() {
			b := route.NewEndpoint("", "1.2.3.4", 1235, "b", nil, -1, "")
			pool.Put(route.NewEndpoint("", "1.2.3.4", 1234, "a", nil, -1, ""))
			pool.Put(b)
			pool.Put(route.NewEndpoint("", "1.2.3.4", 1236, "c", nil, -1, ""))
			pool.Put(route.NewEndpoint("", "1.2.3.4", 1237, "d", nil, -1, ""))

			for i := 0; i < 10; i++ {
				iter := pool.Endpoints(b.PrivateInstanceId)
				e := iter.Next()
				Expect(e).ToNot(BeNil())
				Expect(e.PrivateInstanceId).To(Equal(b.PrivateInstanceId))
			}
		})

		It("finds the initial endpoint by canonical addr", func() {
			b := route.NewEndpoint("", "1.2.3.4", 1235, "b", nil, -1, "")
			pool.Put(route.NewEndpoint("", "1.2.3.4", 1234, "a", nil, -1, ""))
			pool.Put(b)
			pool.Put(route.NewEndpoint("", "1.2.3.4", 1236, "c", nil, -1, ""))
			pool.Put(route.NewEndpoint("", "1.2.3.4", 1237, "d", nil, -1, ""))

			for i := 0; i < 10; i++ {
				iter := pool.Endpoints(b.CanonicalAddr())
				e := iter.Next()
				Expect(e).ToNot(BeNil())
				Expect(e.CanonicalAddr()).To(Equal(b.CanonicalAddr()))
			}
		})

		It("finds when there are multiple private ids", func() {
			endpointFoo := route.NewEndpoint("", "1.2.3.4", 1234, "foo", nil, -1, "")
			endpointBar := route.NewEndpoint("", "5.6.7.8", 5678, "bar", nil, -1, "")

			pool.Put(endpointFoo)
			pool.Put(endpointBar)

			iter := pool.Endpoints(endpointFoo.PrivateInstanceId)
			foundEndpoint := iter.Next()
			Expect(foundEndpoint).ToNot(BeNil())
			Expect(foundEndpoint).To(Equal(endpointFoo))

			iter = pool.Endpoints(endpointBar.PrivateInstanceId)
			foundEndpoint = iter.Next()
			Expect(foundEndpoint).ToNot(BeNil())
			Expect(foundEndpoint).To(Equal(endpointBar))
		})

		It("returns the next available endpoint when the initial is not found", func() {
			eFoo := route.NewEndpoint("", "1.2.3.4", 1234, "foo", nil, -1, "")
			pool.Put(eFoo)

			iter := pool.Endpoints("bogus")
			e := iter.Next()
			Expect(e).ToNot(BeNil())
			Expect(e).To(Equal(eFoo))
		})

		It("finds the correct endpoint when private ids change", func() {
			endpointFoo := route.NewEndpoint("", "1.2.3.4", 1234, "foo", nil, -1, "")
			pool.Put(endpointFoo)

			iter := pool.Endpoints(endpointFoo.PrivateInstanceId)
			foundEndpoint := iter.Next()
			Expect(foundEndpoint).ToNot(BeNil())
			Expect(foundEndpoint).To(Equal(endpointFoo))

			endpointBar := route.NewEndpoint("", "1.2.3.4", 1234, "bar", nil, -1, "")
			pool.Put(endpointBar)

			iter = pool.Endpoints("foo")
			foundEndpoint = iter.Next()
			Expect(foundEndpoint).ToNot(Equal(endpointFoo))

			iter = pool.Endpoints("bar")
			Expect(foundEndpoint).To(Equal(endpointBar))
		})
	})

	Describe("Failed", func() {
		It("skips failed endpoints", func() {
			e1 := route.NewEndpoint("", "1.2.3.4", 5678, "", nil, -1, "")
			e2 := route.NewEndpoint("", "5.6.7.8", 1234, "", nil, -1, "")
			pool.Put(e1)
			pool.Put(e2)

			iter := pool.Endpoints("")
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
			e1 := route.NewEndpoint("", "1.2.3.4", 5678, "", nil, -1, "")
			e2 := route.NewEndpoint("", "5.6.7.8", 1234, "", nil, -1, "")
			pool.Put(e1)
			pool.Put(e2)

			iter := pool.Endpoints("")
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
			pool = route.NewPool(50*time.Millisecond, "")

			e1 := route.NewEndpoint("", "1.2.3.4", 5678, "", nil, -1, "")
			e2 := route.NewEndpoint("", "5.6.7.8", 1234, "", nil, -1, "")
			pool.Put(e1)
			pool.Put(e2)

			iter := pool.Endpoints("")
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
})
