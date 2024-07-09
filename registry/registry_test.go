package registry_test

import (
	"fmt"

	"code.cloudfoundry.org/gorouter/logger"
	. "code.cloudfoundry.org/gorouter/registry"
	"code.cloudfoundry.org/gorouter/test_util"
	"code.cloudfoundry.org/routing-api/models"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gbytes"

	"code.cloudfoundry.org/gorouter/config"
	"code.cloudfoundry.org/gorouter/metrics/fakes"
	"code.cloudfoundry.org/gorouter/route"

	"encoding/json"
	"time"
)

var _ = Describe("RouteRegistry", func() {
	var r *RouteRegistry
	var reporter *fakes.FakeRouteRegistryReporter

	var fooEndpoint, barEndpoint, bar2Endpoint *route.Endpoint
	var configObj *config.Config
	var logger logger.Logger

	var azPreference, az string

	BeforeEach(func() {
		azPreference = "none"
		az = "meow-zone"

		logger = test_util.NewTestZapLogger("test")
		var err error
		configObj, err = config.DefaultConfig()
		Expect(err).ToNot(HaveOccurred())
		configObj.PruneStaleDropletsInterval = 50 * time.Millisecond
		configObj.DropletStaleThreshold = 24 * time.Millisecond
		configObj.IsolationSegments = []string{"foo", "bar"}
		configObj.EndpointDialTimeout = 10 * time.Millisecond

		reporter = new(fakes.FakeRouteRegistryReporter)

		r = NewRouteRegistry(logger, configObj, reporter)
		fooEndpoint = route.NewEndpoint(&route.EndpointOpts{
			Host: "192.168.1.1",
			Tags: map[string]string{
				"runtime":   "ruby18",
				"framework": "sinatra",
			}})

		barEndpoint = route.NewEndpoint(&route.EndpointOpts{
			Host:              "192.168.1.2",
			PrivateInstanceId: "id1",
			Tags: map[string]string{
				"runtime":   "javascript",
				"framework": "node",
			},
		})

		bar2Endpoint = route.NewEndpoint(&route.EndpointOpts{
			Host:              "192.168.1.3",
			PrivateInstanceId: "id3",
			Tags: map[string]string{
				"runtime":   "javascript",
				"framework": "node",
			},
		})
	})

	Context("Register", func() {
		It("emits message_count metrics", func() {
			r.Register("foo", fooEndpoint)
			Expect(reporter.CaptureRegistryMessageCallCount()).To(Equal(1))
		})

		Context("when the endpoint has an UpdatedAt timestamp", func() {
			BeforeEach(func() {
				fooEndpoint.UpdatedAt = time.Now().Add(-3 * time.Second)
			})
			It("emits a route registration latency metric", func() {
				r.Register("foo", fooEndpoint)
				Expect(reporter.CaptureRouteRegistrationLatencyCallCount()).To(Equal(1))
				latency := reporter.CaptureRouteRegistrationLatencyArgsForCall(0)
				Expect(latency).To(BeNumerically("~", 3*time.Second, 10*time.Millisecond))
			})
		})

		Context("when the endpoint has a zero UpdatedAt timestamp", func() {
			BeforeEach(func() {
				fooEndpoint.UpdatedAt = time.Time{}
			})
			It("emits a route registration latency metric", func() {
				r.Register("foo", fooEndpoint)
				Expect(reporter.CaptureRouteRegistrationLatencyCallCount()).To(Equal(0))
			})
		})

		Context("uri", func() {
			It("records and tracks time of last update", func() {
				r.Register("foo", fooEndpoint)
				r.Register("fooo", fooEndpoint)
				Expect(r.NumUris()).To(Equal(2))
				firstUpdateTime := r.TimeOfLastUpdate()

				r.Register("bar", barEndpoint)
				r.Register("baar", barEndpoint)
				Expect(r.NumUris()).To(Equal(4))
				secondUpdateTime := r.TimeOfLastUpdate()

				Expect(secondUpdateTime.After(firstUpdateTime)).To(BeTrue())
			})

			It("ignores duplicates", func() {
				r.Register("bar", barEndpoint)
				r.Register("baar", barEndpoint)

				Expect(r.NumUris()).To(Equal(2))
				Expect(r.NumEndpoints()).To(Equal(1))

				r.Register("bar", barEndpoint)
				r.Register("baar", barEndpoint)

				Expect(r.NumUris()).To(Equal(2))
				Expect(r.NumEndpoints()).To(Equal(1))
			})

			It("ignores case", func() {
				m1 := route.NewEndpoint(&route.EndpointOpts{})
				m2 := route.NewEndpoint(&route.EndpointOpts{})

				r.Register("foo", m1)
				r.Register("FOO", m2)

				Expect(r.NumUris()).To(Equal(1))
			})

			It("allows multiple uris for the same endpoint", func() {
				m1 := route.NewEndpoint(&route.EndpointOpts{})
				m2 := route.NewEndpoint(&route.EndpointOpts{})

				r.Register("foo", m1)
				r.Register("bar", m2)

				Expect(r.NumUris()).To(Equal(2))
				Expect(r.NumEndpoints()).To(Equal(1))
			})

			It("allows routes with paths", func() {
				m1 := route.NewEndpoint(&route.EndpointOpts{})

				r.Register("foo", m1)
				r.Register("foo/v1", m1)

				Expect(r.NumUris()).To(Equal(2))
				Expect(r.NumEndpoints()).To(Equal(1))

			})

			It("excludes query strings in routes without context path", func() {
				m1 := route.NewEndpoint(&route.EndpointOpts{})
				// discards query string
				r.Register("dora.app.com?foo=bar", m1)

				Expect(r.NumUris()).To(Equal(1))
				Expect(r.NumEndpoints()).To(Equal(1))

				p := r.Lookup("dora.app.com")
				Expect(p).ToNot(BeNil())
				Expect(p.ContextPath()).To(Equal("/"))
			})

			It("excludes query strings in routes with context path", func() {
				m1 := route.NewEndpoint(&route.EndpointOpts{})

				// discards query string
				r.Register("dora.app.com/snarf?foo=bar", m1)

				Expect(r.NumUris()).To(Equal(1))
				Expect(r.NumEndpoints()).To(Equal(1))

				p := r.Lookup("dora.app.com/snarf")
				Expect(p).ToNot(BeNil())
				Expect(p.ContextPath()).To(Equal("/snarf"))
			})

			It("remembers the context path properly with case (RFC 3986, Section 6.2.2.1)", func() {
				m1 := route.NewEndpoint(&route.EndpointOpts{})

				r.Register("dora.app.com/app/UP/we/Go", m1)

				Expect(r.NumUris()).To(Equal(1))
				Expect(r.NumEndpoints()).To(Equal(1))

				p := r.Lookup("dora.app.com/app/UP/we/Go")
				Expect(p).ToNot(BeNil())
				Expect(p.ContextPath()).To(Equal("/app/UP/we/Go"))
			})

			It("remembers host and path so that pools can be compared", func() {
				m1 := route.NewEndpoint(&route.EndpointOpts{})

				r.Register("dora.app.com/app", m1)
				r.Register("golang.app.com/app", m1)

				p1 := r.Lookup("dora.app.com/app/with/extra/stuff")
				p2 := r.Lookup("dora.app.com/app")
				p3 := r.Lookup("golang.app.com/app")

				Expect(route.PoolsMatch(p1, p2)).To(BeTrue())
				Expect(route.PoolsMatch(p1, p3)).To(BeFalse())
			})

			It("sets the route service URL on the pool", func() {
				m1 := route.NewEndpoint(&route.EndpointOpts{RouteServiceUrl: "https://www.neopets.com"})

				r.Register("dora.app.com/app", m1)

				p1 := r.Lookup("dora.app.com/app")

				Expect(p1.RouteSvcUrl).To(Equal("https://www.neopets.com"))
			})
		})

		Context("wildcard routes", func() {
			It("records a uri starting with a '*' ", func() {
				r.Register("*.a.route", fooEndpoint)

				Expect(r.NumUris()).To(Equal(1))
				Expect(r.NumEndpoints()).To(Equal(1))
			})
		})

		Context("when route registration message is received", func() {
			It("logs the route and endpoint registration at info level", func() {
				r.Register("a.route", fooEndpoint)

				Expect(logger).To(gbytes.Say(`"log_level":1.*route-registered.*a\.route`))
				Expect(logger).To(gbytes.Say(`"log_level":1.*endpoint-registered.*a\.route.*192\.168\.1\.1`))
			})

			It("logs 'uri-added' at debug level for backward compatibility", func() {
				r.Register("a.route", fooEndpoint)

				Expect(logger).To(gbytes.Say(`"log_level":0.*uri-added.*a\.route`))
			})

			It("logs register message only for new routes", func() {
				r.Register("a.route", fooEndpoint)
				Expect(logger).To(gbytes.Say(`uri-added.*.*a\.route`))
				r.Register("a.route", fooEndpoint)
				Expect(logger).NotTo(gbytes.Say(`uri-added.*.*a\.route`))
				By("not providing IsolationSegment property")
				r.Register("a.route", fooEndpoint)
				//TODO: use pattern matching to make sure we are asserting on the unregister line
				Expect(logger).To(gbytes.Say(`"isolation_segment":"-"`))
			})

			It("logs register message with IsolationSegment when it's provided", func() {
				isoSegEndpoint := route.NewEndpoint(&route.EndpointOpts{
					IsolationSegment: "is1",
				})

				r.Register("a.route", isoSegEndpoint)
				//TODO: use pattern matching to make sure we are asserting on the unregister line
				Expect(logger).To(gbytes.Say(`"isolation_segment":"is1"`))
			})

			It("logs register message with application_id,instance_id,domain_san when it's provided", func() {
				endpointWithAppId := route.NewEndpoint(&route.EndpointOpts{
					AppId:               "app_id1",
					PrivateInstanceId:   "instance_id1",
					ServerCertDomainSAN: "san1",
				})

				r.Register("b.route", endpointWithAppId)
				Expect(logger).To(gbytes.Say(`b\.route.*.*app_id1.*instance_id.*instance_id1.*server_cert_domain_san.*san1`))
			})

			Context("when routing table sharding mode is `segments`", func() {
				BeforeEach(func() {
					configObj.RoutingTableShardingMode = config.SHARD_SEGMENTS
					r = NewRouteRegistry(logger, configObj, reporter)
					fooEndpoint.IsolationSegment = "foo"
					barEndpoint.IsolationSegment = "bar"
					bar2Endpoint.IsolationSegment = "baz"
				})

				It("registers routes in the specified isolation segments, but not other isolation segments", func() {
					r.Register("a.route", fooEndpoint)
					Expect(r.NumUris()).To(Equal(1))
					Expect(r.NumEndpoints()).To(Equal(1))
					Expect(logger).To(gbytes.Say(`uri-added.*.*a\.route`))
					r.Register("b.route", barEndpoint)
					Expect(r.NumUris()).To(Equal(2))
					Expect(r.NumEndpoints()).To(Equal(2))
					Expect(logger).To(gbytes.Say(`uri-added.*.*b\.route`))
					r.Register("c.route", bar2Endpoint)
					Expect(r.NumUris()).To(Equal(2))
					Expect(r.NumEndpoints()).To(Equal(2))
					Expect(logger).ToNot(gbytes.Say(`uri-added.*.*c\.route`))
				})

				Context("with an endpoint in a shared isolation segment", func() {
					BeforeEach(func() {
						fooEndpoint.IsolationSegment = ""
					})
					It("does not log a register message", func() {
						r.Register("a.route", fooEndpoint)
						Expect(r.NumUris()).To(Equal(0))
						Expect(r.NumEndpoints()).To(Equal(0))
						Expect(logger).ToNot(gbytes.Say(`uri-added.*.*a\.route`))
					})
				})

			})

			Context("when routing table sharding mode is `shared-and-segments`", func() {
				BeforeEach(func() {
					configObj.RoutingTableShardingMode = config.SHARD_SHARED_AND_SEGMENTS
					r = NewRouteRegistry(logger, configObj, reporter)
					fooEndpoint.IsolationSegment = "foo"
					barEndpoint.IsolationSegment = "bar"
					bar2Endpoint.IsolationSegment = "baz"
				})

				It("registers routes in the specified isolation segments, but not other isolation segments", func() {
					r.Register("a.route", fooEndpoint)
					Expect(r.NumUris()).To(Equal(1))
					Expect(r.NumEndpoints()).To(Equal(1))
					Expect(logger).To(gbytes.Say(`uri-added.*.*a\.route`))
					r.Register("b.route", barEndpoint)
					Expect(r.NumUris()).To(Equal(2))
					Expect(r.NumEndpoints()).To(Equal(2))
					Expect(logger).To(gbytes.Say(`uri-added.*.*b\.route`))
					r.Register("c.route", bar2Endpoint)
					Expect(r.NumUris()).To(Equal(2))
					Expect(r.NumEndpoints()).To(Equal(2))
					Expect(logger).ToNot(gbytes.Say(`uri-added.*.*c\.route`))
				})

				Context("with an endpoint in a shared isolation segment", func() {
					BeforeEach(func() {
						fooEndpoint.IsolationSegment = ""
					})
					It("resgisters the route", func() {
						r.Register("a.route", fooEndpoint)
						Expect(r.NumUris()).To(Equal(1))
						Expect(r.NumEndpoints()).To(Equal(1))
						Expect(logger).To(gbytes.Say(`uri-added.*.*a\.route`))
					})
				})
			})
		})

		Context("Modification Tags", func() {
			var (
				endpoint *route.Endpoint
				modTag   models.ModificationTag
			)

			BeforeEach(func() {
				modTag = models.ModificationTag{Guid: "abc"}
				endpoint = route.NewEndpoint(&route.EndpointOpts{ModificationTag: modTag})
				r.Register("foo.com", endpoint)
			})

			Context("registering a new route", func() {
				It("adds a new entry to the routing table", func() {
					Expect(r.NumUris()).To(Equal(1))
					Expect(r.NumEndpoints()).To(Equal(1))

					p := r.Lookup("foo.com")
					Expect(p.Endpoints(logger, "", false, azPreference, az).Next(0).ModificationTag).To(Equal(modTag))
				})
			})

			Context("updating an existing route", func() {
				var (
					endpoint2 *route.Endpoint
				)

				Context("when modification tag index changes", func() {

					BeforeEach(func() {
						modTag.Increment()
						endpoint2 = route.NewEndpoint(&route.EndpointOpts{ModificationTag: modTag})
						r.Register("foo.com", endpoint2)
					})

					It("adds a new entry to the routing table", func() {
						Expect(r.NumUris()).To(Equal(1))
						Expect(r.NumEndpoints()).To(Equal(1))

						p := r.Lookup("foo.com")
						Expect(p.Endpoints(logger, "", false, azPreference, az).Next(0).ModificationTag).To(Equal(modTag))
					})

					Context("updating an existing route with an older modification tag", func() {
						var (
							endpoint3 *route.Endpoint
							modTag2   models.ModificationTag
						)

						BeforeEach(func() {
							modTag2 = models.ModificationTag{Guid: "abc", Index: 0}
							endpoint3 = route.NewEndpoint(&route.EndpointOpts{ModificationTag: modTag2})
							r.Register("foo.com", endpoint3)
						})

						It("doesn't update endpoint with older mod tag", func() {
							Expect(r.NumUris()).To(Equal(1))
							Expect(r.NumEndpoints()).To(Equal(1))

							p := r.Lookup("foo.com")
							ep := p.Endpoints(logger, "", false, azPreference, az).Next(0)
							Expect(ep.ModificationTag).To(Equal(modTag))
							Expect(ep).To(Equal(endpoint2))
						})
					})
				})

				Context("when modification tag guid changes", func() {
					BeforeEach(func() {
						modTag.Guid = "def"
						endpoint2 = route.NewEndpoint(&route.EndpointOpts{ModificationTag: modTag})
						r.Register("foo.com", endpoint2)
					})

					It("adds a new entry to the routing table", func() {
						Expect(r.NumUris()).To(Equal(1))
						Expect(r.NumEndpoints()).To(Equal(1))

						p := r.Lookup("foo.com")
						Expect(p.Endpoints(logger, "", false, azPreference, az).Next(0).ModificationTag).To(Equal(modTag))
					})
				})
			})

		})
	})

	Context("Unregister", func() {
		Context("when endpoint has component tagged", func() {
			BeforeEach(func() {
				fooEndpoint.Tags = map[string]string{"component": "oauth-server"}
			})
			It("emits counter metrics", func() {
				r.Unregister("foo", fooEndpoint)
				Expect(reporter.CaptureUnregistryMessageCallCount()).To(Equal(1))
				Expect(reporter.CaptureUnregistryMessageArgsForCall(0)).To(Equal(fooEndpoint))
			})
		})

		Context("when endpoint does not have component tag", func() {
			BeforeEach(func() {
				fooEndpoint.Tags = map[string]string{}
			})
			It("emits counter metrics", func() {
				r.Unregister("foo", fooEndpoint)
				Expect(reporter.CaptureUnregistryMessageCallCount()).To(Equal(1))
				Expect(reporter.CaptureUnregistryMessageArgsForCall(0)).To(Equal(fooEndpoint))
			})
		})

		It("handles unknown URIs", func() {
			r.Unregister("bar", barEndpoint)
			Expect(r.NumUris()).To(Equal(0))
			Expect(r.NumEndpoints()).To(Equal(0))
		})

		It("removes uris and endpoints", func() {
			r.Register("bar", barEndpoint)
			r.Register("baar", barEndpoint)
			Expect(r.NumUris()).To(Equal(2))
			Expect(r.NumEndpoints()).To(Equal(1))

			r.Register("bar", bar2Endpoint)
			r.Register("baar", bar2Endpoint)
			Expect(r.NumUris()).To(Equal(2))
			Expect(r.NumEndpoints()).To(Equal(2))

			r.Unregister("bar", barEndpoint)
			r.Unregister("baar", barEndpoint)
			Expect(r.NumUris()).To(Equal(2))
			Expect(r.NumEndpoints()).To(Equal(1))

			r.Unregister("bar", bar2Endpoint)
			r.Unregister("baar", bar2Endpoint)
			Expect(r.NumUris()).To(Equal(0))
			Expect(r.NumEndpoints()).To(Equal(0))
		})

		It("ignores uri case and matches endpoint", func() {
			m1 := route.NewEndpoint(&route.EndpointOpts{})
			m2 := route.NewEndpoint(&route.EndpointOpts{})

			r.Register("foo", m1)
			r.Unregister("FOO", m2)

			Expect(r.NumUris()).To(Equal(0))
		})

		It("removes the specific url/endpoint combo", func() {
			m1 := route.NewEndpoint(&route.EndpointOpts{})
			m2 := route.NewEndpoint(&route.EndpointOpts{})

			r.Register("foo", m1)
			r.Register("bar", m1)

			r.Unregister("foo", m2)

			Expect(r.NumUris()).To(Equal(1))
		})

		It("removes wildcard routes", func() {
			r.Register("*.bar", barEndpoint)
			r.Register("*.baar", barEndpoint)
			Expect(r.NumUris()).To(Equal(2))
			Expect(r.NumEndpoints()).To(Equal(1))

			r.Register("*.bar", bar2Endpoint)
			r.Register("*.baar", bar2Endpoint)
			Expect(r.NumUris()).To(Equal(2))
			Expect(r.NumEndpoints()).To(Equal(2))

			r.Unregister("*.bar", barEndpoint)
			r.Unregister("*.baar", barEndpoint)
			Expect(r.NumUris()).To(Equal(2))
			Expect(r.NumEndpoints()).To(Equal(1))

			r.Unregister("*.bar", bar2Endpoint)
			r.Unregister("*.baar", bar2Endpoint)
			Expect(r.NumUris()).To(Equal(0))
			Expect(r.NumEndpoints()).To(Equal(0))
		})

		Context("Unregister a route for a crashed app according to EmptyPoolResponseCode503 and EmptyPoolTimeout values", func() {
			Context("EmptyPoolResponseCode503 is true and EmptyPoolTimeout greater than 0", func() {
				JustBeforeEach(func() {
					r.EmptyPoolResponseCode503 = true
					r.EmptyPoolTimeout = 5 * time.Second
				})

				It("Removes the route after EmptyPoolTimeout period of time is passed", func() {
					r.Register("bar", barEndpoint)
					Expect(r.NumUris()).To(Equal(1))

					r.Unregister("bar", barEndpoint)
					Expect(r.NumUris()).To(Equal(1))
					time.Sleep(r.EmptyPoolTimeout)
					r.Unregister("bar", barEndpoint)
					Expect(r.NumUris()).To(Equal(0))

				})
			})

			Context("EmptyPoolResponseCode503 is true and EmptyPoolTimeout equals 0", func() {
				BeforeEach(func() {
					r.EmptyPoolResponseCode503 = true
					r.EmptyPoolTimeout = 0 * time.Second
				})

				It("Removes the route immediately", func() {
					r.Register("bar", barEndpoint)
					Expect(r.NumUris()).To(Equal(1))

					r.Unregister("bar", barEndpoint)
					Expect(r.NumUris()).To(Equal(0))

				})
			})

			Context("EmptyPoolResponseCode503 is false", func() {
				BeforeEach(func() {
					r.EmptyPoolResponseCode503 = false
					r.EmptyPoolTimeout = 1 * time.Second
				})

				It("Removes the route immediately", func() {
					r.Register("bar", barEndpoint)
					Expect(r.NumUris()).To(Equal(1))

					r.Unregister("bar", barEndpoint)
					Expect(r.NumUris()).To(Equal(0))

				})
			})

		})

		Context("when routing table sharding mode is `segments`", func() {
			BeforeEach(func() {
				configObj.RoutingTableShardingMode = config.SHARD_SEGMENTS
				r = NewRouteRegistry(logger, configObj, reporter)
				fooEndpoint.IsolationSegment = "foo"
				barEndpoint.IsolationSegment = "bar"
				bar2Endpoint.IsolationSegment = "bar"

				r.Register("a.route", fooEndpoint)
				r.Register("b.route", barEndpoint)
				r.Register("c.route", bar2Endpoint)
				Expect(r.NumUris()).To(Equal(3))
				Expect(r.NumEndpoints()).To(Equal(3))
			})

			It("unregisters only routes in the specified isolation segments", func() {
				r.Unregister("a.route", fooEndpoint)
				Expect(r.NumUris()).To(Equal(2))
				Expect(r.NumEndpoints()).To(Equal(2))
				Expect(logger).To(gbytes.Say(`endpoint-unregistered.*.*a\.route`))

				r.Unregister("b.route", barEndpoint)
				Expect(r.NumUris()).To(Equal(1))
				Expect(r.NumEndpoints()).To(Equal(1))
				Expect(logger).To(gbytes.Say(`endpoint-unregistered.*.*b\.route`))

				bar2Endpoint.IsolationSegment = "baz"
				r.Unregister("c.route", bar2Endpoint)
				Expect(r.NumUris()).To(Equal(1))
				Expect(r.NumEndpoints()).To(Equal(1))
				Expect(logger).ToNot(gbytes.Say(`endpoint-unregistered.*.*c\.route`))
			})

			Context("with an endpoint in a shared isolation segment", func() {
				BeforeEach(func() {
					fooEndpoint.IsolationSegment = ""
				})
				It("does not log an unregister message", func() {
					r.Unregister("a.route", fooEndpoint)
					Expect(r.NumUris()).To(Equal(3))
					Expect(r.NumEndpoints()).To(Equal(3))
					Expect(logger).ToNot(gbytes.Say(`endpoint-unregistered.*.*a\.route`))
				})
			})

		})
		Context("when routing table sharding mode is `shared-and-segments`", func() {
			BeforeEach(func() {
				configObj.RoutingTableShardingMode = config.SHARD_SHARED_AND_SEGMENTS
				r = NewRouteRegistry(logger, configObj, reporter)
				fooEndpoint.IsolationSegment = "foo"
				barEndpoint.IsolationSegment = "bar"
				bar2Endpoint.IsolationSegment = "bar"

				r.Register("a.route", fooEndpoint)
				r.Register("b.route", barEndpoint)
				r.Register("c.route", bar2Endpoint)
				Expect(r.NumUris()).To(Equal(3))
				Expect(r.NumEndpoints()).To(Equal(3))
			})

			It("unregisters routes in the specified isolation segments and not other isolation segments", func() {
				r.Unregister("a.route", fooEndpoint)
				Expect(r.NumUris()).To(Equal(2))
				Expect(r.NumEndpoints()).To(Equal(2))
				Expect(logger).To(gbytes.Say(`endpoint-unregistered.*.*a\.route`))

				r.Unregister("b.route", barEndpoint)
				Expect(r.NumUris()).To(Equal(1))
				Expect(r.NumEndpoints()).To(Equal(1))
				Expect(logger).To(gbytes.Say(`endpoint-unregistered.*.*b\.route`))

				bar2Endpoint.IsolationSegment = "baz"
				r.Unregister("c.route", bar2Endpoint)
				Expect(r.NumUris()).To(Equal(1))
				Expect(r.NumEndpoints()).To(Equal(1))
				Expect(logger).ToNot(gbytes.Say(`endpoint-unregistered.*.*c\.route`))
			})

			Context("with an endpoint in a shared isolation segment", func() {
				BeforeEach(func() {
					fooEndpoint.IsolationSegment = ""
				})
				It("unregisters the route", func() {
					r.Unregister("a.route", fooEndpoint)
					Expect(r.NumUris()).To(Equal(2))
					Expect(r.NumEndpoints()).To(Equal(2))
					Expect(logger).To(gbytes.Say(`endpoint-unregistered.*.*a\.route`))
				})
			})
		})

		It("removes a route with a path", func() {
			m1 := route.NewEndpoint(&route.EndpointOpts{})

			r.Register("foo/bar", m1)
			r.Unregister("foo/bar", m1)

			Expect(r.NumUris()).To(Equal(0))
		})

		It("only unregisters the exact uri", func() {
			m1 := route.NewEndpoint(&route.EndpointOpts{Host: "192.168.1.1", Port: 1234})

			r.Register("foo", m1)
			r.Register("foo/bar", m1)

			r.Unregister("foo", m1)
			Expect(r.NumUris()).To(Equal(1))

			p1 := r.Lookup("foo/bar")
			iter := p1.Endpoints(logger, "", false, azPreference, az)
			Expect(iter.Next(0).CanonicalAddr()).To(Equal("192.168.1.1:1234"))

			p2 := r.Lookup("foo")
			Expect(p2).To(BeNil())
		})

		It("excludes query strings in routes", func() {
			m1 := route.NewEndpoint(&route.EndpointOpts{})

			r.Register("dora.app.com", m1)

			Expect(r.NumUris()).To(Equal(1))
			Expect(r.NumEndpoints()).To(Equal(1))

			// discards query string
			r.Unregister("dora.app.com?foo=bar", m1)
			Expect(r.NumUris()).To(Equal(0))

		})

		Context("when route is unregistered", func() {
			BeforeEach(func() {
				r.Register("a.route", fooEndpoint)
				r.Unregister("a.route", fooEndpoint)
			})

			It("logs the route and endpoint unregistration at info level", func() {
				Expect(logger).To(gbytes.Say(`"log_level":1.*endpoint-unregistered.*a\.route.*192\.168\.1\.1`))
				Expect(logger).To(gbytes.Say(`"log_level":1.*route-unregistered.*a\.route`))
			})

			It("only logs unregistration for existing routes", func() {
				r.Unregister("non-existent-route", fooEndpoint)
				Expect(logger).NotTo(gbytes.Say(`unregister.*.*a\.non-existent-route`))

				By("not providing IsolationSegment property")
				r.Unregister("a.route", fooEndpoint)
				//TODO: use pattern matching to make sure we are asserting on the unregister line
				Expect(logger).To(gbytes.Say(`"isolation_segment":"-"`))
			})

			It("logs unregister message with IsolationSegment when it's provided", func() {
				isoSegEndpoint := route.NewEndpoint(&route.EndpointOpts{
					IsolationSegment: "is1",
				})
				r.Register("a.isoSegRoute", isoSegEndpoint)
				r.Unregister("a.isoSegRoute", isoSegEndpoint)
				//TODO: use pattern matching to make sure we are asserting on the unregister line
				Expect(logger).To(gbytes.Say(`"isolation_segment":"is1"`))
			})
		})

		Context("with modification tags", func() {
			var (
				endpoint *route.Endpoint
				modTag   models.ModificationTag
			)

			BeforeEach(func() {
				modTag = models.ModificationTag{
					Guid:  "abc",
					Index: 10,
				}
				endpoint = route.NewEndpoint(&route.EndpointOpts{ModificationTag: modTag})
				r.Register("foo.com", endpoint)
				Expect(r.NumEndpoints()).To(Equal(1))
			})

			It("unregisters route with same modification tag", func() {
				r.Unregister("foo.com", endpoint)
				Expect(r.NumEndpoints()).To(Equal(0))
			})

			It("does not unregister route if modification tag older", func() {
				modTag2 := models.ModificationTag{
					Guid:  "abc",
					Index: 8,
				}
				endpoint2 := route.NewEndpoint(&route.EndpointOpts{ModificationTag: modTag2})
				r.Unregister("foo.com", endpoint2)
				Expect(r.NumEndpoints()).To(Equal(1))
			})
		})
	})

	Context("Lookup", func() {
		It("case insensitive lookup", func() {
			m := route.NewEndpoint(&route.EndpointOpts{Host: "192.168.1.1", Port: 1234})

			r.Register("foo", m)

			p1 := r.Lookup("foo")
			p2 := r.Lookup("FOO")
			Expect(p1).To(Equal(p2))

			iter := p1.Endpoints(logger, "", false, azPreference, az)
			Expect(iter.Next(0).CanonicalAddr()).To(Equal("192.168.1.1:1234"))
		})

		It("selects one of the routes", func() {
			m1 := route.NewEndpoint(&route.EndpointOpts{Host: "192.168.1.1", Port: 1234})
			m2 := route.NewEndpoint(&route.EndpointOpts{Host: "192.168.1.1", Port: 1235})

			r.Register("bar", m1)
			r.Register("barr", m1)

			r.Register("bar", m2)
			r.Register("barr", m2)

			Expect(r.NumUris()).To(Equal(2))
			Expect(r.NumEndpoints()).To(Equal(2))

			p := r.Lookup("bar")
			Expect(p).ToNot(BeNil())
			e := p.Endpoints(logger, "", false, azPreference, az).Next(0)
			Expect(e).ToNot(BeNil())
			Expect(e.CanonicalAddr()).To(MatchRegexp("192.168.1.1:123[4|5]"))

		})

		It("selects the outer most wild card route if one exists", func() {
			app1 := route.NewEndpoint(&route.EndpointOpts{Host: "192.168.1.1", Port: 1234})
			app2 := route.NewEndpoint(&route.EndpointOpts{Host: "192.168.1.2", Port: 1234})

			r.Register("*.outer.wild.card", app1)
			r.Register("*.wild.card", app2)

			p := r.Lookup("foo.wild.card")
			Expect(p).ToNot(BeNil())
			e := p.Endpoints(logger, "", false, azPreference, az).Next(0)
			Expect(e).ToNot(BeNil())
			Expect(e.CanonicalAddr()).To(Equal("192.168.1.2:1234"))

			p = r.Lookup("foo.space.wild.card")
			Expect(p).ToNot(BeNil())
			e = p.Endpoints(logger, "", false, azPreference, az).Next(0)
			Expect(e).ToNot(BeNil())
			Expect(e.CanonicalAddr()).To(Equal("192.168.1.2:1234"))
		})

		It("prefers full URIs to wildcard routes", func() {
			app1 := route.NewEndpoint(&route.EndpointOpts{Host: "192.168.1.1", Port: 1234})
			app2 := route.NewEndpoint(&route.EndpointOpts{Host: "192.168.1.2", Port: 1234})

			r.Register("not.wild.card", app1)
			r.Register("*.wild.card", app2)

			p := r.Lookup("not.wild.card")
			Expect(p).ToNot(BeNil())
			e := p.Endpoints(logger, "", false, azPreference, az).Next(0)
			Expect(e).ToNot(BeNil())
			Expect(e.CanonicalAddr()).To(Equal("192.168.1.1:1234"))
		})

		It("sends lookup metrics to the reporter", func() {
			app1 := route.NewEndpoint(&route.EndpointOpts{})
			app2 := route.NewEndpoint(&route.EndpointOpts{})

			r.Register("not.wild.card", app1)
			r.Register("*.wild.card", app2)

			r.Lookup("not.wild.card")

			Expect(reporter.CaptureLookupTimeCallCount()).To(Equal(1))
			lookupTime := reporter.CaptureLookupTimeArgsForCall(0)
			Expect(lookupTime).To(BeNumerically(">", 0))
		})

		Context("has context path", func() {

			var m *route.Endpoint

			BeforeEach(func() {
				m = route.NewEndpoint(&route.EndpointOpts{Host: "192.168.1.1", Port: 1234})
			})

			It("using context path and query string", func() {
				r.Register("dora.app.com/env", m)
				p := r.Lookup("dora.app.com/env?foo=bar")

				Expect(p).ToNot(BeNil())
				iter := p.Endpoints(logger, "", false, azPreference, az)
				Expect(iter.Next(0).CanonicalAddr()).To(Equal("192.168.1.1:1234"))
			})

			It("using nested context path and query string", func() {
				r.Register("dora.app.com/env/abc", m)
				p := r.Lookup("dora.app.com/env/abc?foo=bar&baz=bing")

				Expect(p).ToNot(BeNil())
				iter := p.Endpoints(logger, "", false, azPreference, az)
				Expect(iter.Next(0).CanonicalAddr()).To(Equal("192.168.1.1:1234"))
			})
		})

		Context("when lookup fails to find any routes", func() {
			It("returns nil", func() {
				p := r.Lookup("non-existent")
				Expect(p).To(BeNil())
			})
		})

		It("selects a route even with extra paths in the lookup argument", func() {
			m := route.NewEndpoint(&route.EndpointOpts{Host: "192.168.1.1", Port: 1234})

			r.Register("foo", m)

			p1 := r.Lookup("foo/extra/paths")
			Expect(p1).ToNot(BeNil())

			iter := p1.Endpoints(logger, "", false, azPreference, az)
			Expect(iter.Next(0).CanonicalAddr()).To(Equal("192.168.1.1:1234"))
		})

		It("selects a route even with a query string in the lookup argument", func() {
			m := route.NewEndpoint(&route.EndpointOpts{Host: "192.168.1.1", Port: 1234})

			r.Register("foo", m)

			p1 := r.Lookup("foo?fields=foo,bar")
			Expect(p1).ToNot(BeNil())

			iter := p1.Endpoints(logger, "", false, azPreference, az)
			Expect(iter.Next(0).CanonicalAddr()).To(Equal("192.168.1.1:1234"))
		})

		It("fails to lookup when there is a percent without two hexadecimals following in the url", func() {
			m := route.NewEndpoint(&route.EndpointOpts{})

			r.Register("foo", m)

			p1 := r.Lookup("foo%")
			Expect(p1).To(BeNil())
		})
	})

	Context("LookupWithInstance", func() {
		var (
			appId    string
			appIndex string
		)

		BeforeEach(func() {
			m1 := route.NewEndpoint(&route.EndpointOpts{AppId: "app-1-ID", Host: "192.168.1.1", Port: 1234, PrivateInstanceIndex: "0"})
			m2 := route.NewEndpoint(&route.EndpointOpts{AppId: "app-2-ID", Host: "192.168.1.2", Port: 1235, PrivateInstanceIndex: "0"})

			r.Register("bar.com/foo", m1)
			r.Register("bar.com/foo", m2)

			appId = "app-1-ID"
			appIndex = "0"
		})

		It("selects the route with the matching instance id", func() {
			Expect(r.NumUris()).To(Equal(1))
			Expect(r.NumEndpoints()).To(Equal(2))

			p := r.LookupWithInstance("bar.com/foo", appId, appIndex)
			e := p.Endpoints(logger, "", false, azPreference, az).Next(0)

			Expect(e).ToNot(BeNil())
			Expect(e.CanonicalAddr()).To(MatchRegexp("192.168.1.1:1234"))

			Expect(r.NumUris()).To(Equal(1))
			Expect(r.NumEndpoints()).To(Equal(2))
		})

		It("returns a pool that matches the result of Lookup", func() {
			Expect(r.NumUris()).To(Equal(1))
			Expect(r.NumEndpoints()).To(Equal(2))

			p := r.LookupWithInstance("bar.com/foo", appId, appIndex)
			e := p.Endpoints(logger, "", false, azPreference, az).Next(0)

			Expect(e).ToNot(BeNil())
			Expect(e.CanonicalAddr()).To(MatchRegexp("192.168.1.1:1234"))

			Expect(r.NumUris()).To(Equal(1))
			Expect(r.NumEndpoints()).To(Equal(2))

			p2 := r.Lookup("bar.com/foo")
			Expect(route.PoolsMatch(p, p2)).To(BeTrue())
		})

		Context("when lookup fails to find any routes", func() {
			It("returns nil", func() {
				p := r.LookupWithInstance("foo", appId, appIndex)
				Expect(p).To(BeNil())
			})
		})

		Context("when given an incorrect app index", func() {
			BeforeEach(func() {
				appId = "app-2-ID"
				appIndex = "94"
			})

			It("returns a nil pool", func() {
				Expect(r.NumUris()).To(Equal(1))
				Expect(r.NumEndpoints()).To(Equal(2))
				p := r.LookupWithInstance("bar.com/foo", appId, appIndex)
				Expect(p).To(BeNil())
			})
		})

		Context("when given an incorrect app id", func() {
			BeforeEach(func() {
				appId = "app-3-ID"
				appIndex = "0"
			})

			It("returns a nil pool ", func() {
				Expect(r.NumUris()).To(Equal(1))
				Expect(r.NumEndpoints()).To(Equal(2))
				p := r.LookupWithInstance("bar.com/foo", appId, appIndex)
				Expect(p).To(BeNil())
			})
		})
	})

	Context("Prunes Stale Droplets", func() {
		AfterEach(func() {
			r.StopPruningCycle()
		})

		Context("when emptyPoolResponseCode503 is true and EmptyPoolTimeout greater than 0", func() {
			JustBeforeEach(func() {
				configObj.EmptyPoolResponseCode503 = true
				configObj.EmptyPoolTimeout = 100 * time.Millisecond
			})

			It("logs the route info for stale routes", func() {
				r.Register("bar.com/path1/path2/path3", barEndpoint)
				r.Register("bar.com/path1/path2/path3", fooEndpoint)

				Expect(r.NumUris()).To(Equal(1))

				r.StartPruningCycle()
				time.Sleep(2 * (configObj.PruneStaleDropletsInterval + configObj.EmptyPoolTimeout))

				Expect(r.NumUris()).To(Equal(0))
				Expect(logger).To(gbytes.Say(`"log_level":1.*prune.*bar.com/path1/path2/path3.*endpoints.*isolation_segment`))
			})
		})
		Context("when emptyPoolResponseCode503 is true and EmptyPoolTimeout equals 0", func() {
			JustBeforeEach(func() {
				configObj.EmptyPoolResponseCode503 = true
				configObj.EmptyPoolTimeout = 0
			})

			It("logs the route info for stale routes", func() {
				r.Register("bar.com/path1/path2/path3", barEndpoint)
				r.Register("bar.com/path1/path2/path3", fooEndpoint)

				Expect(r.NumUris()).To(Equal(1))

				r.StartPruningCycle()
				time.Sleep(2 * configObj.PruneStaleDropletsInterval)

				Expect(r.NumUris()).To(Equal(0))
				Expect(logger).To(gbytes.Say(`"log_level":1.*prune.*bar.com/path1/path2/path3.*endpoints.*isolation_segment`))
			})
		})
		Context("when emptyPoolResponseCode503 is false", func() {
			It("logs the route info for stale routes", func() {
				r.Register("bar.com/path1/path2/path3", barEndpoint)
				r.Register("bar.com/path1/path2/path3", fooEndpoint)

				Expect(r.NumUris()).To(Equal(1))

				r.StartPruningCycle()
				time.Sleep(2 * configObj.PruneStaleDropletsInterval)

				Expect(r.NumUris()).To(Equal(0))
				Expect(logger).To(gbytes.Say(`"log_level":1.*prune.*bar.com/path1/path2/path3.*endpoints.*isolation_segment`))
			})
		})

		It("removes stale droplets", func() {
			r.Register("foo", fooEndpoint)
			r.Register("fooo", fooEndpoint)

			r.Register("bar", barEndpoint)
			r.Register("baar", barEndpoint)

			Expect(r.NumUris()).To(Equal(4))
			Expect(r.NumEndpoints()).To(Equal(2))

			r.StartPruningCycle()
			time.Sleep(configObj.PruneStaleDropletsInterval + configObj.DropletStaleThreshold)

			Expect(r.NumUris()).To(Equal(0))
			Expect(r.NumEndpoints()).To(Equal(0))

			marshalled, err := json.Marshal(r)
			Expect(err).NotTo(HaveOccurred())
			Expect(string(marshalled)).To(Equal(`{}`))
		})

		It("emits a routes pruned metric when removing stale droplets", func() {
			r.Register("foo", fooEndpoint)
			r.Register("fooo", fooEndpoint)
			r.Register("fooo", barEndpoint)

			r.StartPruningCycle()
			time.Sleep(configObj.PruneStaleDropletsInterval + configObj.DropletStaleThreshold)
			Expect(reporter.CaptureRoutesPrunedCallCount()).To(Equal(2))
			prunedRoutes := reporter.CaptureRoutesPrunedArgsForCall(0) +
				reporter.CaptureRoutesPrunedArgsForCall(1)

			Expect(prunedRoutes).To(Equal(uint64(3)))
		})

		It("removes stale droplets that have children", func() {
			doneChan := make(chan struct{})
			defer close(doneChan)
			r.Register("foo/path", barEndpoint)
			r.Register("foo", fooEndpoint)

			Expect(r.NumUris()).To(Equal(2))
			Expect(r.NumEndpoints()).To(Equal(2))

			go func() {
				for {
					select {
					case <-doneChan:
						return
					default:
						r.Register("foo/path", barEndpoint)
						time.Sleep(2 * time.Millisecond)
					}
				}
			}()
			r.StartPruningCycle()
			time.Sleep(2*configObj.PruneStaleDropletsInterval + 5*time.Millisecond)

			Expect(r.NumUris()).To(Equal(1))
			Expect(r.NumEndpoints()).To(Equal(1))

			Expect(r.Lookup("foo")).To(BeNil())
			Expect(r.Lookup("foo/path")).NotTo(BeNil())
		})

		It("skips fresh droplets", func() {
			endpoint := route.NewEndpoint(&route.EndpointOpts{})

			r.Register("foo", endpoint)
			r.Register("bar", endpoint)

			r.Register("foo", endpoint)

			Expect(r.NumUris()).To(Equal(2))
			Expect(r.NumEndpoints()).To(Equal(1))

			r.StartPruningCycle()
			time.Sleep(configObj.PruneStaleDropletsInterval + configObj.DropletStaleThreshold)

			r.Register("foo", endpoint)

			r.StopPruningCycle()
			Eventually(r.NumUris).Should(Equal(1))
			Expect(r.NumEndpoints()).To(Equal(1))

			p := r.Lookup("foo")
			Expect(p).ToNot(BeNil())
			Expect(p.Endpoints(logger, "", false, azPreference, az).Next(0)).To(Equal(endpoint))

			p = r.Lookup("bar")
			Expect(p).To(BeNil())
		})

		It("does not block when pruning", func() {
			// when pruning stale droplets,
			// and the stale check takes a while,
			// and a read request comes in (i.e. from Lookup),
			// the read request completes before the stale check

			r.Register("foo", fooEndpoint)
			r.Register("fooo", fooEndpoint)

			r.StartPruningCycle()

			p := r.Lookup("foo")
			Expect(p).ToNot(BeNil())
		})

		Context("when stale threshold is less than pruning cycle", func() {
			BeforeEach(func() {
				var err error
				configObj, err = config.DefaultConfig()
				Expect(err).ToNot(HaveOccurred())
				configObj.PruneStaleDropletsInterval = 100 * time.Millisecond
				configObj.DropletStaleThreshold = 50 * time.Millisecond
				configObj.EndpointDialTimeout = 10 * time.Millisecond
				reporter = new(fakes.FakeRouteRegistryReporter)
				fooEndpoint.StaleThreshold = configObj.DropletStaleThreshold

				r = NewRouteRegistry(logger, configObj, reporter)
			})

			It("sends route metrics to the reporter", func() {
				r.StartPruningCycle()

				Eventually(func() int {
					r.Register("foo", fooEndpoint)
					r.Register("fooo", fooEndpoint)
					return reporter.CaptureRouteStatsCallCount()
				},
					2*configObj.PruneStaleDropletsInterval,
					10*time.Millisecond,
				).Should(Equal(1))

				totalRoutes, _ := reporter.CaptureRouteStatsArgsForCall(0)
				Expect(totalRoutes).To(Equal(2))
			})
		})

		Context("when stale threshold is greater than pruning cycle", func() {
			BeforeEach(func() {
				var err error
				configObj, err = config.DefaultConfig()
				Expect(err).ToNot(HaveOccurred())
				configObj.PruneStaleDropletsInterval = 50 * time.Millisecond
				configObj.DropletStaleThreshold = 1 * time.Second
				reporter = new(fakes.FakeRouteRegistryReporter)

				r = NewRouteRegistry(logger, configObj, reporter)
			})

			It("does not log the route info for fresh routes when pruning", func() {
				endpoint := route.NewEndpoint(&route.EndpointOpts{})
				r.Register("foo.com/bar", endpoint)
				Expect(r.NumUris()).To(Equal(1))

				r.StartPruningCycle()

				time.Sleep(configObj.PruneStaleDropletsInterval + 10*time.Millisecond)

				Expect(r.NumUris()).To(Equal(1))
				Expect(logger).ToNot(gbytes.Say(`prune.*"log_level":0.*foo.com/bar`))
			})
		})

		Context("when suspend pruning is triggered (i.e. nats offline)", func() {
			var totalRoutes int

			BeforeEach(func() {
				totalRoutes = 1000
				Expect(r.NumUris()).To(Equal(0))
				Expect(r.NumEndpoints()).To(Equal(0))

				// add endpoints
				for i := 0; i < totalRoutes; i++ {
					e := route.NewEndpoint(&route.EndpointOpts{
						Host: "192.168.1.1",
						Port: uint16(1024 + i),
					})
					r.Register(route.Uri(fmt.Sprintf("foo-%d", i)), e)
				}

				r.StartPruningCycle()
				r.SuspendPruning(func() bool { return true })
				time.Sleep(configObj.PruneStaleDropletsInterval + configObj.DropletStaleThreshold)
			})

			It("does not remove any routes", func() {
				Expect(r.NumUris()).To(Equal(totalRoutes))
				Expect(r.NumEndpoints()).To(Equal(totalRoutes))

				interval := configObj.PruneStaleDropletsInterval + 50*time.Millisecond
				Eventually(logger, interval).Should(gbytes.Say("prune-suspended"))

				Expect(r.NumUris()).To(Equal(totalRoutes))
				Expect(r.NumEndpoints()).To(Equal(totalRoutes))
			})

			Context("when suspend pruning is turned off (i.e. nats back online)", func() {
				It("marks all routes as updated and does not remove routes", func() {
					Expect(r.NumUris()).To(Equal(totalRoutes))
					Expect(r.NumEndpoints()).To(Equal(totalRoutes))

					r.SuspendPruning(func() bool { return false })

					time.Sleep(configObj.PruneStaleDropletsInterval)

					Eventually(r.NumUris).Should(Equal(0))
					Eventually(r.NumEndpoints).Should(Equal(0))
				})
			})
		})

	})

	Context("Varz data", func() {
		It("NumUris", func() {
			r.Register("bar", barEndpoint)
			r.Register("baar", barEndpoint)

			Expect(r.NumUris()).To(Equal(2))

			r.Register("foo", fooEndpoint)

			Expect(r.NumUris()).To(Equal(3))
		})

		It("NumEndpoints", func() {
			r.Register("bar", barEndpoint)
			r.Register("baar", barEndpoint)

			Expect(r.NumEndpoints()).To(Equal(1))

			r.Register("foo", fooEndpoint)

			Expect(r.NumEndpoints()).To(Equal(2))
		})

		It("TimeOfLastUpdate", func() {
			start := time.Now()
			r.Register("bar", barEndpoint)
			t := r.TimeOfLastUpdate()
			end := time.Now()

			Expect(t.Before(start)).To(BeFalse())
			Expect(t.After(end)).To(BeFalse())
		})

		Context("MSSinceLastUpdate", func() {
			It("returns a value numerically greater than 0", func() {
				r.Register("bar", barEndpoint)
				Eventually(func() int64 { return r.MSSinceLastUpdate() }).Should(BeNumerically(">", 0))
			})

			Context("when no routes have been registered", func() {
				It("returns a value numerically greater than 0", func() {
					Expect(r.MSSinceLastUpdate()).To(Equal(int64(-1)))
				})
			})
		})
	})

	It("marshals", func() {
		m := route.NewEndpoint(&route.EndpointOpts{
			Host:                    "192.168.1.1",
			Port:                    1234,
			Protocol:                "http2",
			RouteServiceUrl:         "https://my-routeService.com",
			StaleThresholdInSeconds: -1,
		})

		r.Register("foo", m)

		marshalled, err := json.Marshal(r)
		Expect(err).NotTo(HaveOccurred())
		Expect(string(marshalled)).To(Equal(`{"foo":[{"address":"192.168.1.1:1234","availability_zone":"","protocol":"http2","tls":false,"ttl":-1,"route_service_url":"https://my-routeService.com","tags":null}]}`))
		r.Unregister("foo", m)
		marshalled, err = json.Marshal(r)
		Expect(err).NotTo(HaveOccurred())
		Expect(string(marshalled)).To(Equal(`{}`))
	})
})
