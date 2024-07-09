package route_test

import (
	"errors"
	"net/http"
	"time"

	"crypto/tls"

	"crypto/x509"

	"net"

	"code.cloudfoundry.org/gorouter/route"
	"code.cloudfoundry.org/gorouter/test_util"
	"code.cloudfoundry.org/routing-api/models"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gbytes"
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

var _ = Describe("EndpointPool", func() {
	var (
		pool   *route.EndpointPool
		logger *test_util.TestZapLogger
	)

	BeforeEach(func() {
		logger = test_util.NewTestZapLogger("test")
		pool = route.NewPool(&route.PoolOpts{
			Logger:             logger,
			RetryAfterFailure:  2 * time.Minute,
			Host:               "",
			ContextPath:        "",
			MaxConnsPerBackend: 0,
		})
	})

	Context("PoolsMatch", func() {
		It("returns true if the hosts and paths on both pools are the same", func() {
			p1 := route.NewPool(&route.PoolOpts{
				Logger:             logger,
				RetryAfterFailure:  2 * time.Minute,
				Host:               "foo.com",
				ContextPath:        "/path",
				MaxConnsPerBackend: 0,
			})
			p2 := route.NewPool(&route.PoolOpts{
				Logger:             logger,
				RetryAfterFailure:  2 * time.Minute,
				Host:               "foo.com",
				ContextPath:        "/path",
				MaxConnsPerBackend: 0,
			})
			Expect(route.PoolsMatch(p1, p2)).To(BeTrue())
		})

		It("returns false if the hosts are the same but paths are different", func() {
			p1 := route.NewPool(&route.PoolOpts{
				Logger:             logger,
				RetryAfterFailure:  2 * time.Minute,
				Host:               "foo.com",
				ContextPath:        "/path",
				MaxConnsPerBackend: 0,
			})
			p2 := route.NewPool(&route.PoolOpts{
				Logger:             logger,
				RetryAfterFailure:  2 * time.Minute,
				Host:               "foo.com",
				ContextPath:        "/other",
				MaxConnsPerBackend: 0,
			})
			Expect(route.PoolsMatch(p1, p2)).To(BeFalse())
		})

		It("returns false if the paths are the same but hosts are different", func() {
			p1 := route.NewPool(&route.PoolOpts{
				Logger:             logger,
				RetryAfterFailure:  2 * time.Minute,
				Host:               "foo.com",
				ContextPath:        "/path",
				MaxConnsPerBackend: 0,
			})
			p2 := route.NewPool(&route.PoolOpts{
				Logger:             logger,
				RetryAfterFailure:  2 * time.Minute,
				Host:               "bar.com",
				ContextPath:        "/path",
				MaxConnsPerBackend: 0,
			})
			Expect(route.PoolsMatch(p1, p2)).To(BeFalse())
		})

		It("returns false if the both hosts and paths on the pools are different", func() {
			p1 := route.NewPool(&route.PoolOpts{
				Logger:             logger,
				RetryAfterFailure:  2 * time.Minute,
				Host:               "foo.com",
				ContextPath:        "/path",
				MaxConnsPerBackend: 0,
			})
			p2 := route.NewPool(&route.PoolOpts{
				Logger:             logger,
				RetryAfterFailure:  2 * time.Minute,
				Host:               "bar.com",
				ContextPath:        "/other",
				MaxConnsPerBackend: 0,
			})
			Expect(route.PoolsMatch(p1, p2)).To(BeFalse())
		})
	})

	Context("Put", func() {
		var az = "meow-zone"
		var azPreference = "none"

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
				Expect(pool.Endpoints(logger, "", false, azPreference, az).Next(0).ModificationTag).To(Equal(modTag2))
			})

			Context("when modification_tag is older", func() {
				BeforeEach(func() {
					modTag2.Increment()
					endpoint := route.NewEndpoint(&route.EndpointOpts{Host: "1.2.3.4", Port: 5678, ModificationTag: modTag2})
					pool.Put(endpoint)
				})

				It("doesnt update an endpoint", func() {
					olderModTag := models.ModificationTag{Guid: "abc"}
					endpoint := route.NewEndpoint(&route.EndpointOpts{Host: "1.2.3.4", Port: 5678, ModificationTag: olderModTag})

					Expect(pool.Put(endpoint)).To(Equal(route.UNMODIFIED))
					Expect(pool.Endpoints(logger, "", false, azPreference, az).Next(0).ModificationTag).To(Equal(modTag2))
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
					e.SetRoundTripper(roundTripper)
				})
			})
			It("preserves roundTrippers on duplicate endpoints", func() {
				sameEndpointRegisteredTwice := route.NewEndpoint(&route.EndpointOpts{Host: "1.2.3.4", Port: 5678})
				pool.Put(sameEndpointRegisteredTwice)
				pool.Each(func(e *route.Endpoint) {
					Expect(e.RoundTripper()).To(Equal(roundTripper))
				})
			})

			It("clears roundTrippers if the server cert domain SAN changes", func() {
				endpointWithSameAddressButDifferentId := route.NewEndpoint(&route.EndpointOpts{Host: "1.2.3.4", Port: 5678, ServerCertDomainSAN: "some-new-san"})
				pool.Put(endpointWithSameAddressButDifferentId)
				pool.Each(func(e *route.Endpoint) {
					Expect(e.RoundTripper()).To(BeNil())
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
		Context("when any endpoint updates its route_service_url", func() {
			It("returns the route_service_url most recently updated in the pool", func() {
				endpointRS1 := route.NewEndpoint(&route.EndpointOpts{Host: "host-1", Port: 1234, RouteServiceUrl: "first-url"})
				endpointRS2 := route.NewEndpoint(&route.EndpointOpts{Host: "host-2", Port: 2234, RouteServiceUrl: "second-url"})
				b := pool.Put(endpointRS1)
				Expect(b).To(Equal(route.ADDED))

				url := pool.RouteServiceUrl()
				Expect(url).To(Equal("first-url"))

				b = pool.Put(endpointRS2)
				Expect(b).To(Equal(route.ADDED))
				url = pool.RouteServiceUrl()
				Expect(url).To(Equal("second-url"))

				endpointRS1.RouteServiceUrl = "third-url"
				b = pool.Put(endpointRS1)
				Expect(b).To(Equal(route.UPDATED))
				url = pool.RouteServiceUrl()
				Expect(url).To(Equal("third-url"))

				endpointRS2.RouteServiceUrl = "fourth-url"
				b = pool.Put(endpointRS2)
				Expect(b).To(Equal(route.UPDATED))
				url = pool.RouteServiceUrl()
				Expect(url).To(Equal("fourth-url"))
			})
		})
	})

	Context("EndpointFailed", func() {
		Context("non-tls endpoints", func() {
			var failedEndpoint, fineEndpoint *route.Endpoint

			BeforeEach(func() {
				failedEndpoint = route.NewEndpoint(&route.EndpointOpts{Host: "1.1.1.1", Port: 8443, UseTLS: false})
				fineEndpoint = route.NewEndpoint(&route.EndpointOpts{Host: "2.2.2.2", Port: 8080, UseTLS: false})
				pool.Put(failedEndpoint)
				pool.Put(fineEndpoint)
				pool.MarkUpdated(time.Now().Add(-2 * time.Second))
			})

			Context("when a read connection is reset", func() {
				It("marks the endpoint as failed", func() {
					az := "meow-zone"
					azPreference := "none"
					connectionResetError := &net.OpError{Op: "read", Err: errors.New("read: connection reset by peer")}
					pool.EndpointFailed(failedEndpoint, connectionResetError)
					i := pool.Endpoints(logger, "", false, azPreference, az)
					epOne := i.Next(0)
					epTwo := i.Next(1)
					Expect(epOne).To(Equal(epTwo))
					Expect(epOne).To(Equal(fineEndpoint))
				})
			})
		})

		Context("tls endpoints", func() {
			It("prunes on hostname mismatch errors", func() {
				endpoint := route.NewEndpoint(&route.EndpointOpts{Host: "1.2.3.4", Port: 5678, UseTLS: true})
				pool.Put(endpoint)
				pool.MarkUpdated(time.Now().Add(-2 * time.Second))
				pool.EndpointFailed(endpoint, x509.HostnameError{})

				Expect(pool.IsEmpty()).To(BeTrue())
			})

			It("prunes on attempting non-TLS backend errors", func() {
				endpoint := route.NewEndpoint(&route.EndpointOpts{Host: "1.2.3.4", Port: 5678, UseTLS: true})
				pool.Put(endpoint)
				pool.MarkUpdated(time.Now().Add(-2 * time.Second))
				pool.EndpointFailed(endpoint, tls.RecordHeaderError{})

				Expect(pool.IsEmpty()).To(BeTrue())
			})

			It("prunes on TCP dial error", func() {
				endpoint := route.NewEndpoint(&route.EndpointOpts{Host: "1.2.3.4", Port: 5678, UseTLS: true, StaleThresholdInSeconds: 1})
				pool.Put(endpoint)
				pool.MarkUpdated(time.Now())
				pool.EndpointFailed(endpoint, &net.OpError{Op: "dial"})

				Expect(pool.IsEmpty()).To(BeTrue())
			})

			It("logs the endpoint that is pruned", func() {
				endpoint := route.NewEndpoint(&route.EndpointOpts{Host: "1.2.3.4", Port: 5678, UseTLS: true, StaleThresholdInSeconds: 1})
				pool.Put(endpoint)
				pool.MarkUpdated(time.Now())
				pool.EndpointFailed(endpoint, &net.OpError{Op: "dial"})

				Expect(logger.Buffer()).To(gbytes.Say(`prune-failed-endpoint`))
			})

			It("does not prune connection reset errors", func() {
				endpoint := route.NewEndpoint(&route.EndpointOpts{Host: "1.2.3.4", Port: 5678, UseTLS: true, StaleThresholdInSeconds: 1})
				pool.Put(endpoint)
				pool.MarkUpdated(time.Now().Add(-2 * time.Second))
				connectionResetError := &net.OpError{Op: "read", Err: errors.New("read: connection reset by peer")}
				pool.EndpointFailed(endpoint, connectionResetError)

				Expect(pool.IsEmpty()).To(BeFalse())
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
	})

	Context("IsOverloaded", func() {
		Context("when MaxConnsPerBackend is not set (unlimited)", func() {
			BeforeEach(func() {
				pool = route.NewPool(&route.PoolOpts{
					Logger:             logger,
					RetryAfterFailure:  2 * time.Minute,
					Host:               "",
					ContextPath:        "",
					MaxConnsPerBackend: 0,
				})
			})

			Context("when all endpoints are overloaded", func() {
				It("returns false", func() {
					endpoint := route.NewEndpoint(&route.EndpointOpts{Port: 5678})
					endpoint.Stats.NumberConnections.Increment()
					endpoint.Stats.NumberConnections.Increment()
					pool.Put(endpoint)

					Expect(pool.IsOverloaded()).To(BeFalse())
				})
			})

			Context("when all endpoints are not overloaded", func() {
				It("returns false", func() {
					endpoint := route.NewEndpoint(&route.EndpointOpts{Port: 5678})
					endpoint.Stats.NumberConnections.Increment()
					pool.Put(endpoint)
					Expect(pool.IsOverloaded()).To(BeFalse())
				})
			})
		})

		BeforeEach(func() {
			pool = route.NewPool(&route.PoolOpts{
				Logger:             logger,
				RetryAfterFailure:  2 * time.Minute,
				Host:               "",
				ContextPath:        "",
				MaxConnsPerBackend: 2,
			})
		})

		Context("when pool is empty", func() {
			It("returns true", func() {
				Expect(pool.IsOverloaded()).To(BeFalse())
			})
		})

		Context("when MaxConnsPerBackend is set", func() {
			Context("when all endpoints are overloaded", func() {
				It("returns true", func() {
					endpoint := route.NewEndpoint(&route.EndpointOpts{Port: 5678})
					endpoint.Stats.NumberConnections.Increment()
					endpoint.Stats.NumberConnections.Increment()
					pool.Put(endpoint)

					Expect(pool.IsOverloaded()).To(BeTrue())

					newEndpoint := route.NewEndpoint(&route.EndpointOpts{Port: 5678})
					pool.Put(newEndpoint)

					Expect(pool.IsOverloaded()).To(BeTrue())
				})
			})

			Context("when all endpoints are not overloaded", func() {
				It("returns false", func() {
					endpoint := route.NewEndpoint(&route.EndpointOpts{Port: 5678})
					endpoint.Stats.NumberConnections.Increment()
					pool.Put(endpoint)
					Expect(pool.IsOverloaded()).To(BeFalse())
				})
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
			AvailabilityZone:        "az-meow",
			Host:                    "1.2.3.4",
			Port:                    5678,
			Protocol:                "http1",
			RouteServiceUrl:         "https://my-rs.com",
			StaleThresholdInSeconds: -1,
		})

		e2 := route.NewEndpoint(&route.EndpointOpts{
			Host:                    "5.6.7.8",
			Port:                    5678,
			Protocol:                "http2",
			StaleThresholdInSeconds: -1,
			ServerCertDomainSAN:     "pvt_test_san",
			PrivateInstanceId:       "pvt_test_instance_id",
			UseTLS:                  true,
		})

		pool.Put(e)
		pool.Put(e2)

		json, err := pool.MarshalJSON()
		Expect(err).ToNot(HaveOccurred())

		Expect(string(json)).To(Equal(`[{"address":"1.2.3.4:5678","availability_zone":"az-meow","protocol":"http1","tls":false,"ttl":-1,"route_service_url":"https://my-rs.com","tags":null},{"address":"5.6.7.8:5678","availability_zone":"","protocol":"http2","tls":true,"ttl":-1,"tags":null,"private_instance_id":"pvt_test_instance_id","server_cert_domain_san":"pvt_test_san"}]`))
	})

	Context("when endpoints do not have empty tags", func() {
		var e *route.Endpoint
		BeforeEach(func() {
			sample_tags := map[string]string{
				"some-key": "some-value"}
			e = route.NewEndpoint(&route.EndpointOpts{
				AvailabilityZone:        "az-meow",
				Host:                    "1.2.3.4",
				Port:                    5678,
				Protocol:                "http2",
				RouteServiceUrl:         "https://my-rs.com",
				StaleThresholdInSeconds: -1,
				Tags:                    sample_tags,
			})
		})

		It("marshals json ", func() {
			pool.Put(e)
			json, err := pool.MarshalJSON()
			Expect(err).ToNot(HaveOccurred())
			Expect(string(json)).To(Equal(`[{"address":"1.2.3.4:5678","availability_zone":"az-meow","protocol":"http2","tls":false,"ttl":-1,"route_service_url":"https://my-rs.com","tags":{"some-key":"some-value"}}]`))
		})
	})

	Context("when endpoints have empty tags", func() {
		var e *route.Endpoint
		BeforeEach(func() {
			sample_tags := map[string]string{}
			e = route.NewEndpoint(&route.EndpointOpts{
				AvailabilityZone:        "az-meow",
				Host:                    "1.2.3.4",
				Port:                    5678,
				Protocol:                "http2",
				RouteServiceUrl:         "https://my-rs.com",
				StaleThresholdInSeconds: -1,
				Tags:                    sample_tags,
			})

		})

		It("marshals json ", func() {
			pool.Put(e)
			json, err := pool.MarshalJSON()
			Expect(err).ToNot(HaveOccurred())
			Expect(string(json)).To(Equal(`[{"address":"1.2.3.4:5678","availability_zone":"az-meow","protocol":"http2","tls":false,"ttl":-1,"route_service_url":"https://my-rs.com","tags":{}}]`))
		})
	})
})
