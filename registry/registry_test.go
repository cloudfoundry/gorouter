package registry_test

import (
	. "github.com/cloudfoundry/gorouter/registry"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	"github.com/cloudfoundry/gorouter/config"
	"github.com/cloudfoundry/gorouter/route"
	"github.com/cloudfoundry/yagnats/fakeyagnats"

	"encoding/json"
	"time"
)

var _ = Describe("Registry", func() {
	var r *CFRegistry
	var messageBus *fakeyagnats.FakeYagnats

	var fooEndpoint, barEndpoint, bar2Endpoint *route.Endpoint
	var configObj *config.Config

	BeforeEach(func() {
		configObj = config.DefaultConfig()
		configObj.DropletStaleThreshold = 10 * time.Millisecond

		messageBus = fakeyagnats.New()
		r = NewCFRegistry(configObj, messageBus)
		fooEndpoint = &route.Endpoint{
			Host: "192.168.1.1",
			Port: 1234,

			ApplicationId: "12345",
			Tags: map[string]string{
				"runtime":   "ruby18",
				"framework": "sinatra",
			},
		}

		barEndpoint = &route.Endpoint{
			Host: "192.168.1.2",
			Port: 4321,

			ApplicationId: "54321",
			Tags: map[string]string{
				"runtime":   "javascript",
				"framework": "node",
			},
		}

		bar2Endpoint = &route.Endpoint{
			Host: "192.168.1.3",
			Port: 1234,

			ApplicationId: "54321",
			Tags: map[string]string{
				"runtime":   "javascript",
				"framework": "node",
			},
		}
	})
	Context("Register", func() {
		It("records and tracks time of last update", func() {
			r.Register("foo", fooEndpoint)
			r.Register("fooo", fooEndpoint)
			Ω(r.NumUris()).To(Equal(2))
			firstUpdateTime := r.TimeOfLastUpdate()

			r.Register("bar", barEndpoint)
			r.Register("baar", barEndpoint)
			Ω(r.NumUris()).To(Equal(4))
			secondUpdateTime := r.TimeOfLastUpdate()

			Ω(secondUpdateTime.After(firstUpdateTime)).To(BeTrue())
		})

		It("ignores duplicates", func() {
			r.Register("bar", barEndpoint)
			r.Register("baar", barEndpoint)

			Ω(r.NumUris()).To(Equal(2))
			Ω(r.NumEndpoints()).To(Equal(1))

			r.Register("bar", barEndpoint)
			r.Register("baar", barEndpoint)

			Ω(r.NumUris()).To(Equal(2))
			Ω(r.NumEndpoints()).To(Equal(1))
		})

		It("ignores case", func() {
			m1 := &route.Endpoint{
				Host: "192.168.1.1",
				Port: 1234,
			}

			m2 := &route.Endpoint{
				Host: "192.168.1.1",
				Port: 1235,
			}

			r.Register("foo", m1)
			r.Register("FOO", m2)

			Ω(r.NumUris()).To(Equal(1))
		})

		It("allows multiple uris for the same endpoint", func() {
			m1 := &route.Endpoint{
				Host: "192.168.1.1",
				Port: 1234,
			}

			m2 := &route.Endpoint{
				Host: "192.168.1.1",
				Port: 1234,
			}

			r.Register("foo", m1)
			r.Register("bar", m2)

			Ω(r.NumUris()).To(Equal(2))
			Ω(r.NumEndpoints()).To(Equal(1))
		})
	})
	Context("Unregister", func() {

		It("removes uris and endpoints", func() {
			r.Register("bar", barEndpoint)
			r.Register("baar", barEndpoint)
			Ω(r.NumUris()).To(Equal(2))
			Ω(r.NumEndpoints()).To(Equal(1))

			r.Register("bar", bar2Endpoint)
			r.Register("baar", bar2Endpoint)
			Ω(r.NumUris()).To(Equal(2))
			Ω(r.NumEndpoints()).To(Equal(2))

			r.Unregister("bar", barEndpoint)
			r.Unregister("baar", barEndpoint)
			Ω(r.NumUris()).To(Equal(2))
			Ω(r.NumEndpoints()).To(Equal(1))

			r.Unregister("bar", bar2Endpoint)
			r.Unregister("baar", bar2Endpoint)
			Ω(r.NumUris()).To(Equal(0))
			Ω(r.NumEndpoints()).To(Equal(0))
		})

		It("ignores uri case and matches endpoint", func() {
			m1 := &route.Endpoint{
				Host: "192.168.1.1",
				Port: 1234,
			}

			m2 := &route.Endpoint{
				Host: "192.168.1.1",
				Port: 1234,
			}

			r.Register("foo", m1)
			r.Unregister("FOO", m2)

			Ω(r.NumUris()).To(Equal(0))
		})

		It("removes the specific url/endpoint combo", func() {
			m1 := &route.Endpoint{
				Host: "192.168.1.1",
				Port: 1234,
			}

			m2 := &route.Endpoint{
				Host: "192.168.1.1",
				Port: 1234,
			}

			r.Register("foo", m1)
			r.Register("bar", m1)

			r.Unregister("foo", m2)

			Ω(r.NumUris()).To(Equal(1))
		})
	})

	Context("Lookup", func() {
		It("case insensitive lookup", func() {
			m := &route.Endpoint{
				Host: "192.168.1.1",
				Port: 1234,
			}

			r.Register("foo", m)

			b, ok := r.Lookup("foo")
			Ω(ok).To(BeTrue())
			Ω(b.CanonicalAddr()).To(Equal("192.168.1.1:1234"))

			b, ok = r.Lookup("FOO")
			Ω(ok).To(BeTrue())
			Ω(b.CanonicalAddr()).To(Equal("192.168.1.1:1234"))
		})

		It("selects one of the routes", func() {
			m1 := &route.Endpoint{
				Host: "192.168.1.2",
				Port: 1234,
			}

			m2 := &route.Endpoint{
				Host: "192.168.1.2",
				Port: 1235,
			}

			r.Register("bar", m1)
			r.Register("barr", m1)

			r.Register("bar", m2)
			r.Register("barr", m2)

			Ω(r.NumUris()).To(Equal(2))
			Ω(r.NumEndpoints()).To(Equal(2))

			b, ok := r.Lookup("bar")
			Ω(ok).To(BeTrue())
			Ω(b.Host).To(Equal("192.168.1.2"))
			Ω(b.Port == m1.Port || b.Port == m2.Port).To(BeTrue())
		})
	})
	Context("PruneStaleDropelts", func() {
		It("removes stale droplets", func() {
			r.Register("foo", fooEndpoint)
			r.Register("fooo", fooEndpoint)

			r.Register("bar", barEndpoint)
			r.Register("baar", barEndpoint)

			Ω(r.NumUris()).To(Equal(4))
			Ω(r.NumEndpoints()).To(Equal(2))

			time.Sleep(configObj.DropletStaleThreshold + 1*time.Millisecond)
			r.PruneStaleDroplets()

			Ω(r.NumUris()).To(Equal(0))
			Ω(r.NumEndpoints()).To(Equal(0))
		})

		It("skips fresh droplets", func() {
			endpoint := &route.Endpoint{
				Host: "192.168.1.1",
				Port: 1234,
			}

			r.Register("foo", endpoint)
			r.Register("bar", endpoint)

			r.Register("foo", endpoint)

			Ω(r.NumUris()).To(Equal(2))
			Ω(r.NumEndpoints()).To(Equal(1))

			time.Sleep(configObj.DropletStaleThreshold + 1*time.Millisecond)

			r.Register("foo", endpoint)

			r.PruneStaleDroplets()

			Ω(r.NumUris()).To(Equal(1))
			Ω(r.NumEndpoints()).To(Equal(1))

			foundEndpoint, found := r.Lookup("foo")
			Ω(found).To(BeTrue())
			Ω(foundEndpoint).To(Equal(endpoint))

			_, found = r.Lookup("bar")
			Ω(found).To(BeFalse())
		})

		It("disables pruning when NATS is unavailable", func() {
			r.Register("foo", fooEndpoint)
			r.Register("fooo", fooEndpoint)

			r.Register("bar", barEndpoint)
			r.Register("baar", barEndpoint)

			Ω(r.NumUris()).To(Equal(4))
			Ω(r.NumEndpoints()).To(Equal(2))

			time.Sleep(configObj.DropletStaleThreshold + 1*time.Millisecond)

			messageBus.OnPing(func() bool { return false })
			r.PruneStaleDroplets()

			Ω(r.NumUris()).To(Equal(4))
			Ω(r.NumEndpoints()).To(Equal(2))
		})

		It("does not block when pruning", func() {
			// when pruning stale droplets,
			// and the stale check takes a while,
			// and a read request comes in (i.e. from Lookup),
			// the read request completes before the stale check

			r.Register("foo", fooEndpoint)
			r.Register("fooo", fooEndpoint)

			barrier := make(chan struct{})

			messageBus.OnPing(func() bool {
				barrier <- struct{}{}
				<-barrier
				return false
			})

			go r.PruneStaleDroplets()
			<-barrier

			_, ok := r.Lookup("foo")
			barrier <- struct{}{}
			Ω(ok).To(BeTrue())
		})
	})

	It("marshals", func() {
		m := &route.Endpoint{
			Host: "192.168.1.1",
			Port: 1234,
		}

		r.Register("foo", m)
		marshalled, err := json.Marshal(r)
		Ω(err).NotTo(HaveOccurred())

		Ω(string(marshalled)).To(Equal(`{"foo":["192.168.1.1:1234"]}`))
	})
})
