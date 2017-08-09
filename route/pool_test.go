package route_test

import (
	"fmt"
	"time"

	"code.cloudfoundry.org/gorouter/route"
	"code.cloudfoundry.org/routing-api/models"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("Endpoint", func() {
	Context("Is TLS", func() {
		Context("when endpoint created is using TLS port", func() {
			var endpoint *route.Endpoint
			BeforeEach(func() {
				endpoint = route.NewEndpoint("", "1.2.3.4", 5678, "", "", nil, -1, "", models.ModificationTag{}, "", true)
			})
			It("should return false", func() {
				Expect(endpoint.IsTLS()).To(BeTrue())
			})
		})
		Context("when endpoint created is not using TLS port", func() {
			var endpoint *route.Endpoint
			BeforeEach(func() {
				endpoint = route.NewEndpoint("", "1.2.3.4", 5678, "", "", nil, -1, "", models.ModificationTag{}, "", false)
			})
			It("should return false", func() {
				Expect(endpoint.IsTLS()).To(BeFalse())
			})
		})
	})
})
var _ = Describe("Pool", func() {
	var pool *route.Pool
	var modTag models.ModificationTag

	BeforeEach(func() {
		pool = route.NewPool(2*time.Minute, "", "")
		modTag = models.ModificationTag{}
	})
	Context("PoolsMatch", func() {
		It("returns true if the hosts and paths on both pools are the same", func() {
			p1 := route.NewPool(2*time.Minute, "foo.com", "/path")
			p2 := route.NewPool(2*time.Minute, "foo.com", "/path")
			Expect(route.PoolsMatch(p1, p2)).To(BeTrue())
		})
		It("returns false if the hosts are the same but paths are different", func() {
			p1 := route.NewPool(2*time.Minute, "foo.com", "/path")
			p2 := route.NewPool(2*time.Minute, "foo.com", "/other")
			Expect(route.PoolsMatch(p1, p2)).To(BeFalse())
		})
		It("returns false if the paths are the same but hosts are different", func() {
			p1 := route.NewPool(2*time.Minute, "foo.com", "/path")
			p2 := route.NewPool(2*time.Minute, "bar.com", "/path")
			Expect(route.PoolsMatch(p1, p2)).To(BeFalse())
		})
		It("returns false if the both hosts and paths on the pools are different", func() {
			p1 := route.NewPool(2*time.Minute, "foo.com", "/path")
			p2 := route.NewPool(2*time.Minute, "bar.com", "/other")
			Expect(route.PoolsMatch(p1, p2)).To(BeFalse())
		})
	})

	Context("Put", func() {
		It("adds endpoints", func() {
			endpoint := &route.Endpoint{}

			b := pool.Put(endpoint)
			Expect(b).To(BeTrue())
		})

		It("handles duplicate endpoints", func() {
			endpoint := route.NewEndpoint("", "1.2.3.4", 5678, "", "", nil, 1, "", modTag, "", false)
			pool.Put(endpoint)
			pool.MarkUpdated(time.Now().Add(-(10 * time.Minute)))

			b := pool.Put(endpoint)
			Expect(b).To(BeTrue())

			prunedEndpoints := pool.PruneEndpoints(time.Second)
			Expect(prunedEndpoints).To(BeEmpty())
		})

		It("handles equivalent (duplicate) endpoints", func() {
			endpoint1 := route.NewEndpoint("", "1.2.3.4", 5678, "", "", nil, -1, "", modTag, "", false)
			endpoint2 := route.NewEndpoint("", "1.2.3.4", 5678, "", "", nil, -1, "", modTag, "", false)

			pool.Put(endpoint1)
			Expect(pool.Put(endpoint2)).To(BeTrue())
		})

		Context("with modification tags", func() {
			var modTag2 models.ModificationTag

			BeforeEach(func() {
				modTag2 = models.ModificationTag{Guid: "abc"}
				endpoint1 := route.NewEndpoint("", "1.2.3.4", 5678, "", "", nil, -1, "", modTag, "", false)
				Expect(pool.Put(endpoint1)).To(BeTrue())
			})

			It("updates an endpoint with modification tag", func() {
				endpoint := route.NewEndpoint("", "1.2.3.4", 5678, "", "", nil, -1, "", modTag2, "", false)
				Expect(pool.Put(endpoint)).To(BeTrue())
				Expect(pool.Endpoints("", "").Next().ModificationTag).To(Equal(modTag2))
			})

			Context("when modification_tag is older", func() {
				BeforeEach(func() {
					modTag.Increment()
					endpoint := route.NewEndpoint("", "1.2.3.4", 5678, "", "", nil, -1, "", modTag2, "", false)
					pool.Put(endpoint)
				})

				It("doesnt update an endpoint", func() {
					olderModTag := models.ModificationTag{Guid: "abc"}
					endpoint := route.NewEndpoint("", "1.2.3.4", 5678, "", "", nil, -1, "", olderModTag, "", false)

					Expect(pool.Put(endpoint)).To(BeFalse())
					Expect(pool.Endpoints("", "").Next().ModificationTag).To(Equal(modTag2))
				})
			})
		})
	})

	Context("RouteServiceUrl", func() {
		It("returns the route_service_url associated with the pool", func() {
			endpoint := &route.Endpoint{}
			endpointRS := &route.Endpoint{RouteServiceUrl: "my-url"}
			b := pool.Put(endpoint)
			Expect(b).To(BeTrue())

			url := pool.RouteServiceUrl()
			Expect(url).To(BeEmpty())

			b = pool.Put(endpointRS)
			Expect(b).To(BeTrue())
			url = pool.RouteServiceUrl()
			Expect(url).To(Equal("my-url"))
		})

		Context("when there are no endpoints in the pool", func() {
			It("returns the empty string", func() {
				url := pool.RouteServiceUrl()
				Expect(url).To(Equal(""))
			})
		})
	})

	Context("Remove", func() {
		It("removes endpoints", func() {
			endpoint := &route.Endpoint{}
			pool.Put(endpoint)

			b := pool.Remove(endpoint)
			Expect(b).To(BeTrue())
			Expect(pool.IsEmpty()).To(BeTrue())
		})

		It("fails to remove an endpoint that doesn't exist", func() {
			endpoint := &route.Endpoint{}

			b := pool.Remove(endpoint)
			Expect(b).To(BeFalse())
		})

		Context("with modification tags", func() {
			BeforeEach(func() {
				modTag = models.ModificationTag{Guid: "abc"}
				endpoint1 := route.NewEndpoint("", "1.2.3.4", 5678, "", "", nil, -1, "", modTag, "", false)
				Expect(pool.Put(endpoint1)).To(BeTrue())
			})

			It("removes an endpoint with modification tag", func() {
				endpoint := route.NewEndpoint("", "1.2.3.4", 5678, "", "", nil, -1, "", modTag, "", false)
				Expect(pool.Remove(endpoint)).To(BeTrue())
				Expect(pool.IsEmpty()).To(BeTrue())
			})

			Context("when modification_tag is the same", func() {
				BeforeEach(func() {
					endpoint := route.NewEndpoint("", "1.2.3.4", 5678, "", "", nil, -1, "", modTag, "", false)
					pool.Put(endpoint)
				})

				It("removes an endpoint", func() {
					endpoint := route.NewEndpoint("", "1.2.3.4", 5678, "", "", nil, -1, "", modTag, "", false)

					Expect(pool.Remove(endpoint)).To(BeTrue())
					Expect(pool.IsEmpty()).To(BeTrue())
				})
			})

			Context("when modification_tag is older", func() {
				BeforeEach(func() {
					modTag.Increment()
					endpoint := route.NewEndpoint("", "1.2.3.4", 5678, "", "", nil, -1, "", modTag, "", false)
					pool.Put(endpoint)
				})

				It("doesnt remove an endpoint", func() {
					olderModTag := models.ModificationTag{Guid: "abc"}
					endpoint := route.NewEndpoint("", "1.2.3.4", 5678, "", "", nil, -1, "", olderModTag, "", false)

					Expect(pool.Remove(endpoint)).To(BeFalse())
					Expect(pool.IsEmpty()).To(BeFalse())
				})
			})
		})

		Context("Filtered pool", func() {
			It("returns copy of the pool with non overloaded endpoints", func() {
				Expect(pool.IsEmpty()).To(BeTrue())
				endpoint1 := route.NewEndpoint("", "1.2.3.4", 5678, "", "", nil, -1, "", modTag, "", false)
				endpoint1.Stats.NumberConnections.Increment()
				endpoint1.Stats.NumberConnections.Increment()
				endpoint1.Stats.NumberConnections.Increment()
				Expect(pool.Put(endpoint1)).To(BeTrue())

				endpoint2 := route.NewEndpoint("", "1.3.5.6", 5679, "", "", nil, -1, "", modTag, "", false)
				Expect(pool.Put(endpoint2)).To(BeTrue())
				// verify the pool before filter has 2 endpoints
				var len int
				pool.Each(func(endpoint *route.Endpoint) {
					len++
				})
				Expect(len).To(Equal(2))

				newPool := pool.FilteredPool(1)
				Expect(newPool).NotTo(BeNil())

				// verify the original pool has both endpoints
				len = 0
				pool.Each(func(endpoint *route.Endpoint) {
					len++
				})
				Expect(len).To(Equal(2))

				// verify newpool has an endpoint
				newPoolLen := 0
				newPool.Each(func(endpoint *route.Endpoint) {
					newPoolLen++
				})
				Expect(newPoolLen).To(Equal(1))
			})
		})
	})

	Context("IsEmpty", func() {
		It("starts empty", func() {
			Expect(pool.IsEmpty()).To(BeTrue())
		})

		It("not empty after adding an endpoint", func() {
			endpoint := &route.Endpoint{}
			pool.Put(endpoint)

			Expect(pool.IsEmpty()).To(BeFalse())
		})

		It("is empty after removing everything", func() {
			endpoint := &route.Endpoint{}
			pool.Put(endpoint)
			pool.Remove(endpoint)

			Expect(pool.IsEmpty()).To(BeTrue())
		})
	})

	Context("PruneEndpoints", func() {
		defaultThreshold := 1 * time.Minute

		Context("when an endpoint has a custom stale time", func() {
			Context("when custom stale threshold is greater than default threshold", func() {
				It("prunes the endpoint", func() {
					customThreshold := int(defaultThreshold.Seconds()) + 20
					e1 := route.NewEndpoint("", "1.2.3.4", 5678, "", "", nil, customThreshold, "", modTag, "", false)
					pool.Put(e1)

					updateTime, _ := time.ParseDuration(fmt.Sprintf("%ds", customThreshold-10))
					pool.MarkUpdated(time.Now().Add(-updateTime))

					Expect(pool.IsEmpty()).To(Equal(false))
					prunedEndpoints := pool.PruneEndpoints(defaultThreshold)
					Expect(pool.IsEmpty()).To(Equal(true))
					Expect(prunedEndpoints).To(ConsistOf(e1))
				})
			})

			Context("and it has passed the stale threshold", func() {
				It("prunes the endpoint", func() {
					e1 := route.NewEndpoint("", "1.2.3.4", 5678, "", "", nil, 20, "", modTag, "", false)

					pool.Put(e1)
					pool.MarkUpdated(time.Now().Add(-25 * time.Second))

					Expect(pool.IsEmpty()).To(Equal(false))
					prunedEndpoints := pool.PruneEndpoints(defaultThreshold)
					Expect(pool.IsEmpty()).To(Equal(true))
					Expect(prunedEndpoints).To(ConsistOf(e1))
				})
			})

			Context("and it has not passed the stale threshold", func() {
				It("does NOT prune the endpoint", func() {
					e1 := route.NewEndpoint("", "1.2.3.4", 5678, "", "", nil, 20, "", modTag, "", false)

					pool.Put(e1)
					pool.MarkUpdated(time.Now())

					Expect(pool.IsEmpty()).To(Equal(false))
					prunedEndpoints := pool.PruneEndpoints(defaultThreshold)
					Expect(pool.IsEmpty()).To(Equal(false))
					Expect(prunedEndpoints).To(BeEmpty())
				})

			})
		})

		Context("when multiple endpoints are added to the pool", func() {
			Context("and they both pass the stale threshold", func() {
				It("prunes the endpoints", func() {
					customThreshold := int(30 * time.Second)
					e1 := route.NewEndpoint("", "1.2.3.4", 5678, "", "", nil, -1, "", modTag, "", false)
					e2 := route.NewEndpoint("", "1.2.3.4", 1234, "", "", nil, customThreshold, "", modTag, "", false)

					pool.Put(e1)
					pool.Put(e2)
					pool.MarkUpdated(time.Now().Add(-(defaultThreshold + 1)))

					Expect(pool.IsEmpty()).To(Equal(false))
					prunedEndpoints := pool.PruneEndpoints(defaultThreshold)
					Expect(pool.IsEmpty()).To(Equal(true))
					Expect(prunedEndpoints).To(ConsistOf(e1, e2))
				})
			})
			Context("and only one passes the stale threshold", func() {
				It("prunes the endpoints", func() {
					e1 := route.NewEndpoint("", "1.2.3.4", 5678, "", "", nil, -1, "", modTag, "", false)
					e2 := route.NewEndpoint("", "1.2.3.4", 1234, "", "", nil, 30, "", modTag, "", false)

					pool.Put(e1)
					pool.Put(e2)
					pool.MarkUpdated(time.Now().Add(-31 * time.Second))

					Expect(pool.IsEmpty()).To(Equal(false))
					prunedEndpoints := pool.PruneEndpoints(defaultThreshold)
					Expect(pool.IsEmpty()).To(Equal(false))
					Expect(prunedEndpoints).To(ConsistOf(e2))
				})
			})
		})

		Context("when an endpoint does NOT have a custom stale time", func() {
			Context("and it has passed the stale threshold", func() {
				It("prunes the endpoint", func() {
					e1 := route.NewEndpoint("", "1.2.3.4", 5678, "", "", nil, -1, "", modTag, "", false)

					pool.Put(e1)
					pool.MarkUpdated(time.Now().Add(-(defaultThreshold + 1)))

					Expect(pool.IsEmpty()).To(Equal(false))
					prunedEndpoints := pool.PruneEndpoints(defaultThreshold)
					Expect(pool.IsEmpty()).To(Equal(true))
					Expect(prunedEndpoints).To(ConsistOf(e1))
				})
			})

			Context("and it has not passed the stale threshold", func() {
				It("does NOT prune the endpoint", func() {
					e1 := route.NewEndpoint("", "1.2.3.4", 5678, "", "", nil, -1, "", modTag, "", false)

					pool.Put(e1)
					pool.MarkUpdated(time.Now())

					Expect(pool.IsEmpty()).To(Equal(false))
					prunedEndpoints := pool.PruneEndpoints(defaultThreshold)
					Expect(pool.IsEmpty()).To(Equal(false))
					Expect(prunedEndpoints).To(BeEmpty())
				})
			})
		})
	})

	Context("MarkUpdated", func() {
		It("updates all endpoints", func() {
			e1 := route.NewEndpoint("", "1.2.3.4", 5678, "", "", nil, -1, "", modTag, "", false)

			pool.Put(e1)

			threshold := 1 * time.Second
			pool.PruneEndpoints(threshold)
			Expect(pool.IsEmpty()).To(BeFalse())

			pool.MarkUpdated(time.Now())
			prunedEndpoints := pool.PruneEndpoints(threshold)
			Expect(pool.IsEmpty()).To(BeFalse())
			Expect(prunedEndpoints).To(BeEmpty())

			prunedEndpoints = pool.PruneEndpoints(0)
			Expect(pool.IsEmpty()).To(BeTrue())
			Expect(prunedEndpoints).To(ConsistOf(e1))
		})
	})

	Context("Each", func() {
		It("applies a function to each endpoint", func() {
			e1 := route.NewEndpoint("", "1.2.3.4", 5678, "", "", nil, -1, "", modTag, "", false)
			e2 := route.NewEndpoint("", "5.6.7.8", 1234, "", "", nil, -1, "", modTag, "", false)
			pool.Put(e1)
			pool.Put(e2)

			endpoints := make(map[string]*route.Endpoint)
			pool.Each(func(e *route.Endpoint) {
				endpoints[e.CanonicalAddr()] = e
			})
			Expect(endpoints).To(HaveLen(2))
			Expect(endpoints[e1.CanonicalAddr()]).To(Equal(e1))
			Expect(endpoints[e2.CanonicalAddr()]).To(Equal(e2))
		})
	})

	Context("Stats", func() {
		Context("NumberConnections", func() {
			It("increments number of connections", func() {
				e1 := route.NewEndpoint("", "1.2.3.4", 5678, "", "", nil, -1, "", modTag, "", false)
				e2 := route.NewEndpoint("", "5.6.7.8", 5678, "", "", nil, -1, "", modTag, "", false)

				// endpoint 1
				e1.Stats.NumberConnections.Increment()
				Expect(e1.Stats.NumberConnections.Count()).To(Equal(int64(1)))
				e1.Stats.NumberConnections.Increment()
				Expect(e1.Stats.NumberConnections.Count()).To(Equal(int64(2)))

				// endpoint 2
				for i := 0; i < 10; i++ {
					e2.Stats.NumberConnections.Increment()
					Expect(e2.Stats.NumberConnections.Count()).To(Equal(int64(i + 1)))
				}
			})

			It("decrements number of connections", func() {
				e1 := route.NewEndpoint("", "1.2.3.4", 5678, "", "", nil, -1, "", modTag, "", false)

				e1.Stats.NumberConnections.Increment()
				Expect(e1.Stats.NumberConnections.Count()).To(Equal(int64(1)))
				e1.Stats.NumberConnections.Decrement()
				Expect(e1.Stats.NumberConnections.Count()).To(Equal(int64(0)))
			})
		})
	})

	It("marshals json", func() {
		e := route.NewEndpoint("", "1.2.3.4", 5678, "", "", nil, -1, "https://my-rs.com", modTag, "", false)
		e2 := route.NewEndpoint("", "5.6.7.8", 5678, "", "", nil, -1, "", modTag, "", false)
		pool.Put(e)
		pool.Put(e2)

		json, err := pool.MarshalJSON()
		Expect(err).ToNot(HaveOccurred())

		Expect(string(json)).To(Equal(`[{"address":"1.2.3.4:5678","ttl":-1,"route_service_url":"https://my-rs.com","tags":null},{"address":"5.6.7.8:5678","ttl":-1,"tags":null}]`))
	})

	Context("when endpoints do not have empty tags", func() {
		var e *route.Endpoint
		BeforeEach(func() {
			sample_tags := map[string]string{
				"some-key": "some-value"}
			e = route.NewEndpoint("", "1.2.3.4", 5678, "", "", sample_tags, -1, "https://my-rs.com", modTag, "", false)
		})
		It("marshals json ", func() {
			pool.Put(e)
			json, err := pool.MarshalJSON()
			Expect(err).ToNot(HaveOccurred())
			Expect(string(json)).To(Equal(`[{"address":"1.2.3.4:5678","ttl":-1,"route_service_url":"https://my-rs.com","tags":{"some-key":"some-value"}}]`))
		})
	})

	Context("when endpoints have empty tags", func() {
		var e *route.Endpoint
		BeforeEach(func() {
			sample_tags := map[string]string{}
			e = route.NewEndpoint("", "1.2.3.4", 5678, "", "", sample_tags, -1, "https://my-rs.com", modTag, "", false)
		})
		It("marshals json ", func() {
			pool.Put(e)
			json, err := pool.MarshalJSON()
			Expect(err).ToNot(HaveOccurred())
			Expect(string(json)).To(Equal(`[{"address":"1.2.3.4:5678","ttl":-1,"route_service_url":"https://my-rs.com","tags":{}}]`))
		})
	})
})
