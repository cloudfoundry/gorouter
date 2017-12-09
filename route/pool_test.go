package route_test

import (
	"net/http"
	"time"

	"crypto/tls"

	"crypto/x509"

	"net"

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
				endpoint = route.NewEndpoint(&route.EndpointOpts{UseTLS: true})
			})
			It("should return false", func() {
				Expect(endpoint.IsTLS()).To(BeTrue())
			})
		})
		Context("when endpoint created is not using TLS port", func() {
			var endpoint *route.Endpoint
			BeforeEach(func() {
				endpoint = route.NewEndpoint(&route.EndpointOpts{UseTLS: false})
			})
			It("should return false", func() {
				Expect(endpoint.IsTLS()).To(BeFalse())
			})
		})
	})
})
var _ = Describe("Pool", func() {
	var pool *route.Pool

	BeforeEach(func() {
		pool = route.NewPool(2*time.Minute, "", "")
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
			Expect(b).To(Equal(route.ADDED))
		})

		It("handles duplicate endpoints", func() {
			endpoint := route.NewEndpoint(&route.EndpointOpts{Host: "1.2.3.4", StaleThresholdInSeconds: 1})

			pool.Put(endpoint)
			pool.MarkUpdated(time.Now().Add(-(10 * time.Minute)))

			b := pool.Put(endpoint)
			Expect(b).To(Equal(route.UPDATED))

			prunedEndpoints := pool.PruneEndpoints()
			Expect(prunedEndpoints).To(BeEmpty())
		})

		It("handles equivalent (duplicate) endpoints", func() {
			endpoint1 := route.NewEndpoint(&route.EndpointOpts{Host: "1.2.3.4", Port: 5678})
			endpoint2 := route.NewEndpoint(&route.EndpointOpts{Host: "1.2.3.4", Port: 5678})

			pool.Put(endpoint1)
			Expect(pool.Put(endpoint2)).To(Equal(route.UPDATED))
		})

		Context("with modification tags", func() {
			var modTag models.ModificationTag
			var modTag2 models.ModificationTag

			BeforeEach(func() {
				modTag = models.ModificationTag{}
				modTag2 = models.ModificationTag{Guid: "abc"}
				endpoint1 := route.NewEndpoint(&route.EndpointOpts{Host: "1.2.3.4", Port: 5678, ModificationTag: modTag})

				Expect(pool.Put(endpoint1)).To(Equal(route.ADDED))
			})

			It("updates an endpoint with modification tag", func() {
				endpoint := route.NewEndpoint(&route.EndpointOpts{Host: "1.2.3.4", Port: 5678, ModificationTag: modTag2})

				Expect(pool.Put(endpoint)).To(Equal(route.UPDATED))
				Expect(pool.Endpoints("", "").Next().ModificationTag).To(Equal(modTag2))
			})

			Context("when modification_tag is older", func() {
				BeforeEach(func() {
					modTag.Increment()
					endpoint := route.NewEndpoint(&route.EndpointOpts{Host: "1.2.3.4", Port: 5678, ModificationTag: modTag2})
					pool.Put(endpoint)
				})

				It("doesnt update an endpoint", func() {
					olderModTag := models.ModificationTag{Guid: "abc"}
					endpoint := route.NewEndpoint(&route.EndpointOpts{Host: "1.2.3.4", Port: 5678, ModificationTag: olderModTag})

					Expect(pool.Put(endpoint)).To(Equal(route.UNMODIFIED))
					Expect(pool.Endpoints("", "").Next().ModificationTag).To(Equal(modTag2))
				})
			})
		})

		Context("RoundTrippers", func() {
			var (
				roundTripper *http.Transport
			)
			BeforeEach(func() {
				endpoint := route.NewEndpoint(&route.EndpointOpts{Host: "1.2.3.4", Port: 5678})
				pool.Put(endpoint)
				roundTripper = &http.Transport{TLSClientConfig: &tls.Config{ServerName: "server-cert-domain-san-1"}}
				pool.Each(func(e *route.Endpoint) {
					e.RoundTripper = roundTripper
				})
			})
			It("preserves roundTrippers on duplicate endpoints", func() {
				sameEndpointRegisteredTwice := route.NewEndpoint(&route.EndpointOpts{Host: "1.2.3.4", Port: 5678})
				pool.Put(sameEndpointRegisteredTwice)
				pool.Each(func(e *route.Endpoint) {
					Expect(e.RoundTripper).To(Equal(roundTripper))
				})
			})

			It("clears roundTrippers if the server cert domain SAN changes", func() {
				endpointWithSameAddressButDifferentId := route.NewEndpoint(&route.EndpointOpts{Host: "1.2.3.4", Port: 5678, ServerCertDomainSAN: "some-new-san"})
				pool.Put(endpointWithSameAddressButDifferentId)
				pool.Each(func(e *route.Endpoint) {
					Expect(e.RoundTripper).To(BeNil())
				})
			})

		})
	})

	Context("RouteServiceUrl", func() {
		It("returns the route_service_url associated with the pool", func() {
			endpoint := &route.Endpoint{}
			endpointRS := &route.Endpoint{RouteServiceUrl: "my-url"}
			b := pool.Put(endpoint)
			Expect(b).To(Equal(route.ADDED))

			url := pool.RouteServiceUrl()
			Expect(url).To(BeEmpty())

			b = pool.Put(endpointRS)
			Expect(b).To(Equal(route.UPDATED))
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

	Context("EndpointFailed", func() {
		It("prunes tls routes on hostname mismatch errors", func() {
			endpoint := route.NewEndpoint(&route.EndpointOpts{Host: "1.2.3.4", Port: 5678, UseTLS: true})
			pool.Put(endpoint)

			pool.MarkUpdated(time.Now().Add(-2 * time.Second))

			pool.EndpointFailed(endpoint, x509.HostnameError{})

			Expect(pool.IsEmpty()).To(BeTrue())
		})

		It("does not prune tls routes on connection errors", func() {
			endpoint := route.NewEndpoint(&route.EndpointOpts{Host: "1.2.3.4", Port: 5678, UseTLS: true})
			pool.Put(endpoint)

			pool.MarkUpdated(time.Now().Add(-2 * time.Second))

			pool.EndpointFailed(endpoint, &net.OpError{Op: "dial"})

			Expect(pool.IsEmpty()).To(BeFalse())
		})

		It("does not prune non-tls routes that have already expired", func() {
			endpoint := route.NewEndpoint(&route.EndpointOpts{Host: "1.2.3.4", Port: 5678, UseTLS: false})
			pool.Put(endpoint)

			pool.MarkUpdated(time.Now().Add(-2 * time.Second))

			pool.EndpointFailed(endpoint, x509.HostnameError{})

			Expect(pool.IsEmpty()).To(BeFalse())
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
			var modTag models.ModificationTag
			BeforeEach(func() {
				modTag = models.ModificationTag{Guid: "abc"}
				endpoint1 := route.NewEndpoint(&route.EndpointOpts{Host: "1.2.3.4", Port: 5678, ModificationTag: modTag})

				Expect(pool.Put(endpoint1)).To(Equal(route.ADDED))
			})

			It("removes an endpoint with modification tag", func() {
				endpoint := route.NewEndpoint(&route.EndpointOpts{Host: "1.2.3.4", Port: 5678, ModificationTag: modTag})
				Expect(pool.Remove(endpoint)).To(BeTrue())
				Expect(pool.IsEmpty()).To(BeTrue())
			})

			Context("when modification_tag is the same", func() {
				BeforeEach(func() {
					endpoint := route.NewEndpoint(&route.EndpointOpts{Host: "1.2.3.4", Port: 5678, ModificationTag: modTag})
					pool.Put(endpoint)
				})

				It("removes an endpoint", func() {
					endpoint := route.NewEndpoint(&route.EndpointOpts{Host: "1.2.3.4", Port: 5678, ModificationTag: modTag})

					Expect(pool.Remove(endpoint)).To(BeTrue())
					Expect(pool.IsEmpty()).To(BeTrue())
				})
			})

			Context("when modification_tag is older", func() {
				BeforeEach(func() {
					modTag.Increment()
					endpoint := route.NewEndpoint(&route.EndpointOpts{Host: "1.2.3.4", Port: 5678, ModificationTag: modTag})
					pool.Put(endpoint)
				})

				It("doesnt remove an endpoint", func() {
					olderModTag := models.ModificationTag{Guid: "abc"}
					endpoint := route.NewEndpoint(&route.EndpointOpts{Host: "1.2.3.4", Port: 5678, ModificationTag: olderModTag})

					Expect(pool.Remove(endpoint)).To(BeFalse())
					Expect(pool.IsEmpty()).To(BeFalse())
				})
			})
		})

		Context("Filtered pool", func() {
			It("returns copy of the pool with non overloaded endpoints", func() {
				Expect(pool.IsEmpty()).To(BeTrue())
				endpoint1 := route.NewEndpoint(&route.EndpointOpts{Port: 5678})
				endpoint1.Stats.NumberConnections.Increment()
				endpoint1.Stats.NumberConnections.Increment()
				endpoint1.Stats.NumberConnections.Increment()

				Expect(pool.Put(endpoint1)).To(Equal(route.ADDED))

				endpoint2 := route.NewEndpoint(&route.EndpointOpts{Port: 5679})

				Expect(pool.Put(endpoint2)).To(Equal(route.ADDED))
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

		Context("when the pool contains tls endpoints", func() {
			BeforeEach(func() {
				e1 := route.NewEndpoint(&route.EndpointOpts{UseTLS: true, StaleThresholdInSeconds: 60})
				pool.Put(e1)
			})
			It("does not prune the tls endpoints", func() {
				pool.MarkUpdated(time.Now().Add(-2 * defaultThreshold))
				Expect(pool.IsEmpty()).To(Equal(false))
				prunedEndpoints := pool.PruneEndpoints()
				Expect(pool.IsEmpty()).To(Equal(false))
				Expect(len(prunedEndpoints)).To(Equal(0))
			})
		})

		Context("when an endpoint has passed the stale threshold", func() {
			It("prunes the endpoint", func() {
				e1 := route.NewEndpoint(&route.EndpointOpts{UseTLS: false, StaleThresholdInSeconds: 20})

				pool.Put(e1)
				pool.MarkUpdated(time.Now().Add(-25 * time.Second))

				Expect(pool.IsEmpty()).To(Equal(false))
				prunedEndpoints := pool.PruneEndpoints()
				Expect(pool.IsEmpty()).To(Equal(true))
				Expect(prunedEndpoints).To(ConsistOf(e1))
			})
		})

		Context("when an endpoint has not passed the stale threshold", func() {
			It("does NOT prune the endpoint", func() {
				e1 := route.NewEndpoint(&route.EndpointOpts{UseTLS: false, StaleThresholdInSeconds: 20})

				pool.Put(e1)
				pool.MarkUpdated(time.Now())

				Expect(pool.IsEmpty()).To(Equal(false))
				prunedEndpoints := pool.PruneEndpoints()
				Expect(pool.IsEmpty()).To(Equal(false))
				Expect(prunedEndpoints).To(BeEmpty())
			})
		})

		Context("when multiple endpoints are added to the pool", func() {
			Context("and they both pass the stale threshold", func() {
				It("prunes the endpoints", func() {
					customThreshold := int(30 * time.Second)
					e1 := route.NewEndpoint(&route.EndpointOpts{Port: 5678, StaleThresholdInSeconds: -1})
					e2 := route.NewEndpoint(&route.EndpointOpts{Port: 1234, StaleThresholdInSeconds: customThreshold})

					pool.Put(e1)
					pool.Put(e2)
					pool.MarkUpdated(time.Now().Add(-(defaultThreshold + 1)))

					Expect(pool.IsEmpty()).To(Equal(false))
					prunedEndpoints := pool.PruneEndpoints()
					Expect(pool.IsEmpty()).To(Equal(true))
					Expect(prunedEndpoints).To(ConsistOf(e1, e2))
				})
			})
			Context("and only one passes the stale threshold", func() {
				It("prunes the endpoints", func() {
					e1 := route.NewEndpoint(&route.EndpointOpts{Port: 5678, StaleThresholdInSeconds: -1})
					e2 := route.NewEndpoint(&route.EndpointOpts{Port: 1234, StaleThresholdInSeconds: 60})

					pool.Put(e1)
					pool.Put(e2)
					pool.MarkUpdated(time.Now())

					Expect(pool.IsEmpty()).To(Equal(false))
					prunedEndpoints := pool.PruneEndpoints()
					Expect(pool.IsEmpty()).To(Equal(false))
					Expect(prunedEndpoints).To(ConsistOf(e1))
				})
			})
		})

		Context("when an endpoint does NOT have a custom stale time", func() {
			Context("and it has passed the stale threshold", func() {
				It("prunes the endpoint", func() {
					e1 := route.NewEndpoint(&route.EndpointOpts{Port: 5678})

					pool.Put(e1)
					pool.MarkUpdated(time.Now().Add(-(defaultThreshold + 1)))

					Expect(pool.IsEmpty()).To(Equal(false))
					prunedEndpoints := pool.PruneEndpoints()
					Expect(pool.IsEmpty()).To(Equal(true))
					Expect(prunedEndpoints).To(ConsistOf(e1))
				})
			})

			Context("and it has not passed the stale threshold", func() {
				It("does NOT prune the endpoint", func() {
					e1 := route.NewEndpoint(&route.EndpointOpts{Port: 5678, StaleThresholdInSeconds: 120})

					pool.Put(e1)
					pool.MarkUpdated(time.Now())

					Expect(pool.IsEmpty()).To(Equal(false))
					prunedEndpoints := pool.PruneEndpoints()
					Expect(pool.IsEmpty()).To(Equal(false))
					Expect(prunedEndpoints).To(BeEmpty())
				})
			})
		})
	})

	Context("MarkUpdated", func() {
		It("updates all endpoints", func() {
			e1 := route.NewEndpoint(&route.EndpointOpts{Port: 5678, StaleThresholdInSeconds: 120})

			pool.Put(e1)

			pool.PruneEndpoints()
			Expect(pool.IsEmpty()).To(BeFalse())

			pool.MarkUpdated(time.Now())
			prunedEndpoints := pool.PruneEndpoints()
			Expect(pool.IsEmpty()).To(BeFalse())
			Expect(prunedEndpoints).To(BeEmpty())

			pool.MarkUpdated(time.Now().Add(-120 * time.Second))
			prunedEndpoints = pool.PruneEndpoints()
			Expect(pool.IsEmpty()).To(BeTrue())
			Expect(prunedEndpoints).To(ConsistOf(e1))
		})
	})

	Context("Each", func() {
		It("applies a function to each endpoint", func() {
			e1 := route.NewEndpoint(&route.EndpointOpts{Port: 5678})
			e2 := route.NewEndpoint(&route.EndpointOpts{Port: 1234})
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
				e1 := route.NewEndpoint(&route.EndpointOpts{Port: 5678})
				e2 := route.NewEndpoint(&route.EndpointOpts{Port: 1234})

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
				e1 := route.NewEndpoint(&route.EndpointOpts{Port: 5678})
				e1.Stats.NumberConnections.Increment()
				Expect(e1.Stats.NumberConnections.Count()).To(Equal(int64(1)))
				e1.Stats.NumberConnections.Decrement()
				Expect(e1.Stats.NumberConnections.Count()).To(Equal(int64(0)))
			})
		})
	})

	It("marshals json", func() {
		e := route.NewEndpoint(&route.EndpointOpts{
			Host:                    "1.2.3.4",
			Port:                    5678,
			RouteServiceUrl:         "https://my-rs.com",
			StaleThresholdInSeconds: -1,
		})

		e2 := route.NewEndpoint(&route.EndpointOpts{
			Host: "5.6.7.8",
			Port: 5678,
			StaleThresholdInSeconds: -1,
			ServerCertDomainSAN:     "pvt_test_san",
			PrivateInstanceId:       "pvt_test_instance_id",
			UseTLS:                  true,
		})

		pool.Put(e)
		pool.Put(e2)

		json, err := pool.MarshalJSON()
		Expect(err).ToNot(HaveOccurred())

		Expect(string(json)).To(Equal(`[{"address":"1.2.3.4:5678","tls":false,"ttl":-1,"route_service_url":"https://my-rs.com","tags":null},{"address":"5.6.7.8:5678","tls":true,"ttl":-1,"tags":null,"private_instance_id":"pvt_test_instance_id","server_cert_domain_san":"pvt_test_san"}]`))
	})

	Context("when endpoints do not have empty tags", func() {
		var e *route.Endpoint
		BeforeEach(func() {
			sample_tags := map[string]string{
				"some-key": "some-value"}
			e = route.NewEndpoint(&route.EndpointOpts{
				Host:                    "1.2.3.4",
				Port:                    5678,
				RouteServiceUrl:         "https://my-rs.com",
				StaleThresholdInSeconds: -1,
				Tags: sample_tags,
			})
		})
		It("marshals json ", func() {
			pool.Put(e)
			json, err := pool.MarshalJSON()
			Expect(err).ToNot(HaveOccurred())
			Expect(string(json)).To(Equal(`[{"address":"1.2.3.4:5678","tls":false,"ttl":-1,"route_service_url":"https://my-rs.com","tags":{"some-key":"some-value"}}]`))
		})
	})

	Context("when endpoints have empty tags", func() {
		var e *route.Endpoint
		BeforeEach(func() {
			sample_tags := map[string]string{}
			e = route.NewEndpoint(&route.EndpointOpts{
				Host:                    "1.2.3.4",
				Port:                    5678,
				RouteServiceUrl:         "https://my-rs.com",
				StaleThresholdInSeconds: -1,
				Tags: sample_tags,
			})

		})
		It("marshals json ", func() {
			pool.Put(e)
			json, err := pool.MarshalJSON()
			Expect(err).ToNot(HaveOccurred())
			Expect(string(json)).To(Equal(`[{"address":"1.2.3.4:5678","tls":false,"ttl":-1,"route_service_url":"https://my-rs.com","tags":{}}]`))
		})
	})
})
