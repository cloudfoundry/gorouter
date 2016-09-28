package registry_test

import (
	"fmt"

	. "code.cloudfoundry.org/gorouter/registry"
	"code.cloudfoundry.org/lager"
	"code.cloudfoundry.org/lager/lagertest"
	"code.cloudfoundry.org/routing-api/models"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gbytes"

	"code.cloudfoundry.org/gorouter/config"
	"code.cloudfoundry.org/gorouter/metrics/reporter/fakes"
	"code.cloudfoundry.org/gorouter/route"

	"encoding/json"
	"time"
)

var _ = Describe("RouteRegistry", func() {
	var r *RouteRegistry
	var reporter *fakes.FakeRouteRegistryReporter

	var fooEndpoint, barEndpoint, bar2Endpoint *route.Endpoint
	var configObj *config.Config
	var logger lager.Logger
	var modTag models.ModificationTag

	BeforeEach(func() {

		logger = lagertest.NewTestLogger("test")
		configObj = config.DefaultConfig()
		configObj.PruneStaleDropletsInterval = 50 * time.Millisecond
		configObj.DropletStaleThreshold = 24 * time.Millisecond

		reporter = new(fakes.FakeRouteRegistryReporter)

		r = NewRouteRegistry(logger, configObj, reporter)
		modTag = models.ModificationTag{}
		fooEndpoint = route.NewEndpoint("12345", "192.168.1.1", 1234,
			"id1", "0",
			map[string]string{
				"runtime":   "ruby18",
				"framework": "sinatra",
			}, -1, "", modTag)

		barEndpoint = route.NewEndpoint("54321", "192.168.1.2", 4321,
			"id2", "0", map[string]string{
				"runtime":   "javascript",
				"framework": "node",
			}, -1, "https://my-rs.com", modTag)

		bar2Endpoint = route.NewEndpoint("54321", "192.168.1.3", 1234,
			"id3", "0", map[string]string{
				"runtime":   "javascript",
				"framework": "node",
			}, -1, "", modTag)
	})

	Context("Register", func() {
		It("emits message_count metrics", func() {
			r.Register("foo", fooEndpoint)
			Expect(reporter.CaptureRegistryMessageCallCount()).To(Equal(1))
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
				m1 := route.NewEndpoint("", "192.168.1.1", 1234, "", "", nil, -1, "", modTag)
				m2 := route.NewEndpoint("", "192.168.1.1", 1235, "", "", nil, -1, "", modTag)

				r.Register("foo", m1)
				r.Register("FOO", m2)

				Expect(r.NumUris()).To(Equal(1))
			})

			It("allows multiple uris for the same endpoint", func() {
				m1 := route.NewEndpoint("", "192.168.1.1", 1234, "", "", nil, -1, "", modTag)
				m2 := route.NewEndpoint("", "192.168.1.1", 1234, "", "", nil, -1, "", modTag)

				r.Register("foo", m1)
				r.Register("bar", m2)

				Expect(r.NumUris()).To(Equal(2))
				Expect(r.NumEndpoints()).To(Equal(1))
			})

			It("allows routes with paths", func() {
				m1 := route.NewEndpoint("", "192.168.1.1", 1234, "", "", nil, -1, "", modTag)

				r.Register("foo", m1)
				r.Register("foo/v1", m1)

				Expect(r.NumUris()).To(Equal(2))
				Expect(r.NumEndpoints()).To(Equal(1))

			})

			It("excludes query strings in routes", func() {
				m1 := route.NewEndpoint("", "192.168.1.1", 1234, "", "", nil, -1, "", modTag)

				// discards query string
				r.Register("dora.app.com/snarf?foo=bar", m1)

				Expect(r.NumUris()).To(Equal(1))
				Expect(r.NumEndpoints()).To(Equal(1))

				p := r.Lookup("dora.app.com/snarf")
				Expect(p).ToNot(BeNil())
				Expect(p.ContextPath()).To(Equal("/snarf"))
			})

			It("remembers the context path properly with case (RFC 3986, Section 6.2.2.1)", func() {
				m1 := route.NewEndpoint("", "192.168.1.1", 1234, "", nil, -1, "", modTag)

				r.Register("dora.app.com/app/UP/we/Go", m1)

				Expect(r.NumUris()).To(Equal(1))
				Expect(r.NumEndpoints()).To(Equal(1))

				p := r.Lookup("dora.app.com/app/UP/we/Go")
				Expect(p).ToNot(BeNil())
				Expect(p.ContextPath()).To(Equal("/app/UP/we/Go"))
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
			BeforeEach(func() {
				r.Register("a.route", fooEndpoint)
			})

			It("logs at debug level", func() {
				Expect(logger).To(gbytes.Say(`uri-added.*"log_level":0.*a\.route`))
			})

			It("logs register message only for new routes", func() {
				Expect(logger).To(gbytes.Say(`uri-added.*.*a\.route`))
				r.Register("a.route", fooEndpoint)
				Expect(logger).NotTo(gbytes.Say(`uri-added.*.*a\.route`))
			})
		})

		Context("Modification Tags", func() {
			var (
				endpoint *route.Endpoint
			)

			BeforeEach(func() {
				modTag = models.ModificationTag{Guid: "abc"}
				endpoint = route.NewEndpoint("", "1.1.1.1", 1234, "", "", nil, -1, "", modTag)
				r.Register("foo.com", endpoint)
			})

			Context("registering a new route", func() {
				It("adds a new entry to the routing table", func() {
					Expect(r.NumUris()).To(Equal(1))
					Expect(r.NumEndpoints()).To(Equal(1))

					p := r.Lookup("foo.com")
					Expect(p.Endpoints("", "").Next().ModificationTag).To(Equal(modTag))
				})
			})

			Context("updating an existing route", func() {
				var (
					endpoint2 *route.Endpoint
				)

				Context("when modification tag index changes", func() {

					BeforeEach(func() {
						modTag.Increment()
						endpoint2 = route.NewEndpoint("", "1.1.1.1", 1234, "", "", nil, -1, "", modTag)
						r.Register("foo.com", endpoint2)
					})

					It("adds a new entry to the routing table", func() {
						Expect(r.NumUris()).To(Equal(1))
						Expect(r.NumEndpoints()).To(Equal(1))

						p := r.Lookup("foo.com")
						Expect(p.Endpoints("", "").Next().ModificationTag).To(Equal(modTag))
					})

					Context("updating an existing route with an older modification tag", func() {
						var (
							endpoint3 *route.Endpoint
							modTag2   models.ModificationTag
						)

						BeforeEach(func() {
							modTag2 = models.ModificationTag{Guid: "abc", Index: 0}
							endpoint3 = route.NewEndpoint("", "1.1.1.1", 1234, "", "", nil, -1, "", modTag2)
							r.Register("foo.com", endpoint3)
						})

						It("doesn't update endpoint with older mod tag", func() {
							Expect(r.NumUris()).To(Equal(1))
							Expect(r.NumEndpoints()).To(Equal(1))

							p := r.Lookup("foo.com")
							ep := p.Endpoints("", "").Next()
							Expect(ep.ModificationTag).To(Equal(modTag))
							Expect(ep).To(Equal(endpoint2))
						})
					})
				})

				Context("when modification tag guid changes", func() {
					BeforeEach(func() {
						modTag.Guid = "def"
						endpoint2 = route.NewEndpoint("", "1.1.1.1", 1234, "", "", nil, -1, "", modTag)
						r.Register("foo.com", endpoint2)
					})

					It("adds a new entry to the routing table", func() {
						Expect(r.NumUris()).To(Equal(1))
						Expect(r.NumEndpoints()).To(Equal(1))

						p := r.Lookup("foo.com")
						Expect(p.Endpoints("", "").Next().ModificationTag).To(Equal(modTag))
					})
				})
			})

		})
	})

	Context("Unregister", func() {
		It("emits message_count metrics", func() {
			r.Unregister("foo", fooEndpoint)
			Expect(reporter.CaptureRegistryMessageCallCount()).To(Equal(1))
		})

		It("Handles unknown URIs", func() {
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
			m1 := route.NewEndpoint("", "192.168.1.1", 1234, "", "", nil, -1, "", modTag)
			m2 := route.NewEndpoint("", "192.168.1.1", 1234, "", "", nil, -1, "", modTag)

			r.Register("foo", m1)
			r.Unregister("FOO", m2)

			Expect(r.NumUris()).To(Equal(0))
		})

		It("removes the specific url/endpoint combo", func() {
			m1 := route.NewEndpoint("", "192.168.1.1", 1234, "", "", nil, -1, "", modTag)
			m2 := route.NewEndpoint("", "192.168.1.1", 1234, "", "", nil, -1, "", modTag)

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

		It("removes a route with a path", func() {
			m1 := route.NewEndpoint("", "192.168.1.1", 1234, "", "", nil, -1, "", modTag)

			r.Register("foo/bar", m1)
			r.Unregister("foo/bar", m1)

			Expect(r.NumUris()).To(Equal(0))
		})

		It("only unregisters the exact uri", func() {
			m1 := route.NewEndpoint("", "192.168.1.1", 1234, "", "", nil, -1, "", modTag)

			r.Register("foo", m1)
			r.Register("foo/bar", m1)

			r.Unregister("foo", m1)
			Expect(r.NumUris()).To(Equal(1))

			p1 := r.Lookup("foo/bar")
			iter := p1.Endpoints("", "")
			Expect(iter.Next().CanonicalAddr()).To(Equal("192.168.1.1:1234"))

			p2 := r.Lookup("foo")
			Expect(p2).To(BeNil())
		})

		It("excludes query strings in routes", func() {
			m1 := route.NewEndpoint("", "192.168.1.1", 1234, "", "", nil, -1, "", modTag)

			r.Register("dora.app.com", m1)

			Expect(r.NumUris()).To(Equal(1))
			Expect(r.NumEndpoints()).To(Equal(1))

			// discards query string
			r.Unregister("dora.app.com?foo=bar", m1)
			Expect(r.NumUris()).To(Equal(0))

		})

		Context("when route unregistration message is received", func() {
			BeforeEach(func() {
				r.Register("a.route", fooEndpoint)
				r.Unregister("a.route", fooEndpoint)
			})

			It("logs at debug level", func() {
				Expect(logger).To(gbytes.Say(`unregister.*"log_level":0.*a\.route`))
			})

			It("only logs unregistration for existing routes", func() {
				r.Unregister("non-existent-route", fooEndpoint)
				Expect(logger).NotTo(gbytes.Say(`unregister.*.*a\.non-existent-route`))
			})
		})

		Context("with modification tags", func() {
			var (
				endpoint *route.Endpoint
			)

			BeforeEach(func() {
				modTag = models.ModificationTag{
					Guid:  "abc",
					Index: 10,
				}
				endpoint = route.NewEndpoint("", "192.168.1.1", 1234, "", "", nil, -1, "", modTag)
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
				endpoint2 := route.NewEndpoint("", "192.168.1.1", 1234, "", "", nil, -1, "", modTag2)
				r.Unregister("foo.com", endpoint2)
				Expect(r.NumEndpoints()).To(Equal(1))
			})
		})
	})

	Context("Lookup", func() {
		It("case insensitive lookup", func() {
			m := route.NewEndpoint("", "192.168.1.1", 1234, "", "", nil, -1, "", modTag)

			r.Register("foo", m)

			p1 := r.Lookup("foo")
			p2 := r.Lookup("FOO")
			Expect(p1).To(Equal(p2))

			iter := p1.Endpoints("", "")
			Expect(iter.Next().CanonicalAddr()).To(Equal("192.168.1.1:1234"))
		})

		It("selects one of the routes", func() {
			m1 := route.NewEndpoint("", "192.168.1.1", 1234, "", "", nil, -1, "", modTag)
			m2 := route.NewEndpoint("", "192.168.1.1", 1235, "", "", nil, -1, "", modTag)

			r.Register("bar", m1)
			r.Register("barr", m1)

			r.Register("bar", m2)
			r.Register("barr", m2)

			Expect(r.NumUris()).To(Equal(2))
			Expect(r.NumEndpoints()).To(Equal(2))

			p := r.Lookup("bar")
			Expect(p).ToNot(BeNil())
			e := p.Endpoints("", "").Next()
			Expect(e).ToNot(BeNil())
			Expect(e.CanonicalAddr()).To(MatchRegexp("192.168.1.1:123[4|5]"))
		})

		It("selects the outer most wild card route if one exists", func() {
			app1 := route.NewEndpoint("", "192.168.1.1", 1234, "", "", nil, -1, "", modTag)
			app2 := route.NewEndpoint("", "192.168.1.2", 1234, "", "", nil, -1, "", modTag)

			r.Register("*.outer.wild.card", app1)
			r.Register("*.wild.card", app2)

			p := r.Lookup("foo.wild.card")
			Expect(p).ToNot(BeNil())
			e := p.Endpoints("", "").Next()
			Expect(e).ToNot(BeNil())
			Expect(e.CanonicalAddr()).To(Equal("192.168.1.2:1234"))

			p = r.Lookup("foo.space.wild.card")
			Expect(p).ToNot(BeNil())
			e = p.Endpoints("", "").Next()
			Expect(e).ToNot(BeNil())
			Expect(e.CanonicalAddr()).To(Equal("192.168.1.2:1234"))
		})

		It("prefers full URIs to wildcard routes", func() {
			app1 := route.NewEndpoint("", "192.168.1.1", 1234, "", "", nil, -1, "", modTag)
			app2 := route.NewEndpoint("", "192.168.1.2", 1234, "", "", nil, -1, "", modTag)

			r.Register("not.wild.card", app1)
			r.Register("*.wild.card", app2)

			p := r.Lookup("not.wild.card")
			Expect(p).ToNot(BeNil())
			e := p.Endpoints("", "").Next()
			Expect(e).ToNot(BeNil())
			Expect(e.CanonicalAddr()).To(Equal("192.168.1.1:1234"))
		})

		It("sends lookup metrics to the reporter", func() {
			app1 := route.NewEndpoint("", "192.168.1.1", 1234, "", "", nil, -1, "", modTag)
			app2 := route.NewEndpoint("", "192.168.1.2", 1234, "", "", nil, -1, "", modTag)

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
				m = route.NewEndpoint("", "192.168.1.1", 1234, "", "", nil, -1, "", modTag)
			})

			It("using context path and query string", func() {
				r.Register("dora.app.com/env", m)
				p := r.Lookup("dora.app.com/env?foo=bar")

				Expect(p).ToNot(BeNil())
				iter := p.Endpoints("", "")
				Expect(iter.Next().CanonicalAddr()).To(Equal("192.168.1.1:1234"))
			})

			It("using nested context path and query string", func() {
				r.Register("dora.app.com/env/abc", m)
				p := r.Lookup("dora.app.com/env/abc?foo=bar&baz=bing")

				Expect(p).ToNot(BeNil())
				iter := p.Endpoints("", "")
				Expect(iter.Next().CanonicalAddr()).To(Equal("192.168.1.1:1234"))
			})
		})
	})

	Context("LookupWithInstance", func() {
		var (
			appId    string
			appIndex string
		)

		BeforeEach(func() {
			m1 := route.NewEndpoint("app-1-ID", "192.168.1.1", 1234, "", "0", nil, -1, "", modTag)
			m2 := route.NewEndpoint("app-2-ID", "192.168.1.2", 1235, "", "0", nil, -1, "", modTag)

			r.Register("bar", m1)
			r.Register("bar", m2)

			appId = "app-1-ID"
			appIndex = "0"
		})

		It("selects the route with the matching instance id", func() {
			Expect(r.NumUris()).To(Equal(1))
			Expect(r.NumEndpoints()).To(Equal(2))

			p := r.LookupWithInstance("bar", appId, appIndex)
			e := p.Endpoints("", "").Next()

			Expect(e).ToNot(BeNil())
			Expect(e.CanonicalAddr()).To(MatchRegexp("192.168.1.1:1234"))

			Expect(r.NumUris()).To(Equal(1))
			Expect(r.NumEndpoints()).To(Equal(2))
		})

		Context("when given an incorrect app index", func() {
			BeforeEach(func() {
				appId = "app-2-ID"
				appIndex = "94"
			})

			It("returns a nil pool", func() {
				Expect(r.NumUris()).To(Equal(1))
				Expect(r.NumEndpoints()).To(Equal(2))
				p := r.LookupWithInstance("bar", appId, appIndex)
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
				p := r.LookupWithInstance("bar", appId, appIndex)
				Expect(p).To(BeNil())
			})
		})
	})

	Context("Prunes Stale Droplets", func() {
		AfterEach(func() {
			r.StopPruningCycle()
		})

		It("logs the route info for stale routes", func() {
			r.Register("bar.com/path1/path2/path3", barEndpoint)
			r.Register("bar.com/path1/path2/path3", fooEndpoint)

			Expect(r.NumUris()).To(Equal(1))

			r.StartPruningCycle()
			time.Sleep(2 * configObj.PruneStaleDropletsInterval)

			Expect(r.NumUris()).To(Equal(0))
			r.MarshalJSON()
			Expect(logger).To(gbytes.Say(`prune.*"log_level":1.*endpoints.*bar.com/path1/path2/path3`))
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
			endpoint := route.NewEndpoint("", "192.168.1.1", 1234, "", "", nil, -1, "", modTag)

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
			Expect(p.Endpoints("", "").Next()).To(Equal(endpoint))

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
				configObj = config.DefaultConfig()
				configObj.PruneStaleDropletsInterval = 50 * time.Millisecond
				configObj.DropletStaleThreshold = 45 * time.Millisecond
				reporter = new(fakes.FakeRouteRegistryReporter)

				r = NewRouteRegistry(logger, configObj, reporter)
			})

			It("sends route metrics to the reporter", func() {
				r.StartPruningCycle()

				Eventually(func() int {
					e := *fooEndpoint
					r.Register("foo", &e)
					r.Register("fooo", &e)
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
				configObj = config.DefaultConfig()
				configObj.PruneStaleDropletsInterval = 50 * time.Millisecond
				configObj.DropletStaleThreshold = 100 * time.Millisecond
				reporter = new(fakes.FakeRouteRegistryReporter)

				r = NewRouteRegistry(logger, configObj, reporter)
			})

			It("does not log the route info for fresh routes when pruning", func() {
				endpoint := route.NewEndpoint("", "192.168.1.1", 1234, "", "", nil, 60, "", modTag)
				r.Register("foo.com/bar", endpoint)
				Expect(r.NumUris()).To(Equal(1))

				r.StartPruningCycle()

				time.Sleep(configObj.PruneStaleDropletsInterval + 10*time.Millisecond)

				Expect(r.NumUris()).To(Equal(1))
				r.MarshalJSON()
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
					e := route.NewEndpoint("12345", "192.168.1.1", uint16(1024+i), "id1", "", nil, -1, "", modTag)
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
	})

	It("marshals", func() {
		m := route.NewEndpoint("", "192.168.1.1", 1234, "", "", nil, -1, "https://my-routeService.com", modTag)
		r.Register("foo", m)

		marshalled, err := json.Marshal(r)
		Expect(err).NotTo(HaveOccurred())
		Expect(string(marshalled)).To(Equal(`{"foo":[{"address":"192.168.1.1:1234","ttl":-1,"route_service_url":"https://my-routeService.com"}]}`))
		r.Unregister("foo", m)
		marshalled, err = json.Marshal(r)
		Expect(err).NotTo(HaveOccurred())
		Expect(string(marshalled)).To(Equal(`{}`))
	})
})
