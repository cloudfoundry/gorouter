package route_test

import (
	"fmt"
	"time"

	"code.cloudfoundry.org/gorouter/route"
	"code.cloudfoundry.org/routing-api/models"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("LeastConnection", func() {
	var pool *route.Pool

	BeforeEach(func() {
		pool = route.NewPool(2*time.Minute, "", "")
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
					e := route.NewEndpoint("", ip, 60000, "", "", nil, -1, "", models.ModificationTag{}, "", false)
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
		})
	})

	Context("PreRequest", func() {
		It("increments the NumberConnections counter", func() {
			endpointFoo := route.NewEndpoint("", "1.2.3.4", 1234, "foo", "", nil, -1, "", models.ModificationTag{}, "", false)
			Expect(endpointFoo.Stats.NumberConnections.Count()).To(Equal(int64(0)))
			pool.Put(endpointFoo)
			iter := route.NewLeastConnection(pool, "foo")
			iter.PreRequest(endpointFoo)
			Expect(endpointFoo.Stats.NumberConnections.Count()).To(Equal(int64(1)))
		})
	})

	Context("PostRequest", func() {
		It("decrements the NumberConnections counter", func() {
			endpointFoo := route.NewEndpoint("", "1.2.3.4", 1234, "foo", "", nil, -1, "", models.ModificationTag{}, "", false)
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
