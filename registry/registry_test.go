package registry

import (
	"encoding/json"
	"time"

	"github.com/cloudfoundry/go_cfmessagebus/mock_cfmessagebus"
	. "launchpad.net/gocheck"

	"github.com/cloudfoundry/gorouter/config"
	"github.com/cloudfoundry/gorouter/route"
)

type RegistrySuite struct {
	*Registry

	messageBus *mock_cfmessagebus.MockMessageBus
}

var _ = Suite(&RegistrySuite{})

var fooEndpoint, barEndpoint, bar2Endpoint *route.Endpoint

func (s *RegistrySuite) SetUpTest(c *C) {
	var configObj *config.Config

	configObj = config.DefaultConfig()
	configObj.DropletStaleThreshold = 10 * time.Millisecond

	s.messageBus = mock_cfmessagebus.NewMockMessageBus()
	s.Registry = NewRegistry(configObj, s.messageBus)

	fooEndpoint = &route.Endpoint{
		Host: "192.168.1.1",
		Port: 1234,
		Uris: []route.Uri{"foo.vcap.me", "fooo.vcap.me"},

		ApplicationId: "12345",
		Tags: map[string]string{
			"runtime":   "ruby18",
			"framework": "sinatra",
		},
	}

	barEndpoint = &route.Endpoint{
		Host: "192.168.1.2",
		Port: 4321,
		Uris: []route.Uri{"bar.vcap.me", "barr.vcap.me"},

		ApplicationId: "54321",
		Tags: map[string]string{
			"runtime":   "javascript",
			"framework": "node",
		},
	}

	bar2Endpoint = &route.Endpoint{
		Host: "192.168.1.3",
		Port: 1234,
		Uris: []route.Uri{"bar.vcap.me", "barr.vcap.me"},

		ApplicationId: "54321",
		Tags: map[string]string{
			"runtime":   "javascript",
			"framework": "node",
		},
	}
}

func (s *RegistrySuite) TestRegister(c *C) {
	s.Register(fooEndpoint)
	c.Check(s.NumUris(), Equals, 2)
	firstUpdateTime := s.timeOfLastUpdate

	s.Register(barEndpoint)
	c.Check(s.NumUris(), Equals, 4)
	secondUpdateTime := s.timeOfLastUpdate

	c.Assert(s.staleTracker.Len(), Equals, 2)
	c.Assert(secondUpdateTime.After(firstUpdateTime), Equals, true)
}

func (s *RegistrySuite) TestRegisterIgnoreEmpty(c *C) {
	s.Register(&route.Endpoint{})
	c.Check(s.NumUris(), Equals, 0)
	c.Check(s.NumEndpoints(), Equals, 0)
}

func (s *RegistrySuite) TestRegisterIgnoreDuplicates(c *C) {
	s.Register(barEndpoint)
	c.Check(s.NumUris(), Equals, 2)
	c.Check(s.NumEndpoints(), Equals, 1)

	s.Register(barEndpoint)
	c.Check(s.NumUris(), Equals, 2)
	c.Check(s.NumEndpoints(), Equals, 1)

	s.Unregister(barEndpoint)
	c.Check(s.NumUris(), Equals, 0)
	c.Check(s.NumEndpoints(), Equals, 0)
}

func (s *RegistrySuite) TestRegisterUppercase(c *C) {
	m1 := &route.Endpoint{
		Host: "192.168.1.1",
		Port: 1234,
		Uris: []route.Uri{"foo.vcap.me"},
	}

	m2 := &route.Endpoint{
		Host: "192.168.1.1",
		Port: 1235,
		Uris: []route.Uri{"FOO.VCAP.ME"},
	}

	s.Register(m1)
	s.Register(m2)

	c.Check(s.NumUris(), Equals, 1)
}

func (s *RegistrySuite) TestRegisterDoesntReplace(c *C) {
	m1 := &route.Endpoint{
		Host: "192.168.1.1",
		Port: 1234,
		Uris: []route.Uri{"foo.vcap.me"},
	}

	m2 := &route.Endpoint{
		Host: "192.168.1.1",
		Port: 1234,
		Uris: []route.Uri{"bar.vcap.me"},
	}

	s.Register(m1)
	s.Register(m2)

	c.Check(s.NumUris(), Equals, 2)
	c.Check(s.NumEndpoints(), Equals, 1)
}

func (s *RegistrySuite) TestRegisterWithoutUris(c *C) {
	m := &route.Endpoint{
		Host: "192.168.1.1",
		Port: 1234,
		Uris: []route.Uri{},
	}

	s.Register(m)

	c.Check(s.NumUris(), Equals, 0)
	c.Check(s.NumEndpoints(), Equals, 0)
}

func (s *RegistrySuite) TestUnregister(c *C) {
	s.Register(barEndpoint)
	c.Check(s.NumUris(), Equals, 2)
	c.Check(s.NumEndpoints(), Equals, 1)

	s.Register(bar2Endpoint)
	c.Check(s.NumUris(), Equals, 2)
	c.Check(s.NumEndpoints(), Equals, 2)

	s.Unregister(barEndpoint)
	c.Check(s.NumUris(), Equals, 2)
	c.Check(s.NumEndpoints(), Equals, 1)

	s.Unregister(bar2Endpoint)
	c.Check(s.NumUris(), Equals, 0)
	c.Check(s.NumEndpoints(), Equals, 0)
}

func (s *RegistrySuite) TestUnregisterUppercase(c *C) {
	m1 := &route.Endpoint{
		Host: "192.168.1.1",
		Port: 1234,
		Uris: []route.Uri{"foo.vcap.me"},
	}

	m2 := &route.Endpoint{
		Host: "192.168.1.1",
		Port: 1234,
		Uris: []route.Uri{"FOO.VCAP.ME"},
	}

	s.Register(m1)
	s.Unregister(m2)

	c.Check(s.NumUris(), Equals, 0)
}

func (s *RegistrySuite) TestUnregisterDoesntDemolish(c *C) {
	m1 := &route.Endpoint{
		Host: "192.168.1.1",
		Port: 1234,
		Uris: []route.Uri{"foo.vcap.me", "bar.vcap.me"},
	}

	m2 := &route.Endpoint{
		Host: "192.168.1.1",
		Port: 1234,
		Uris: []route.Uri{"foo.vcap.me"},
	}

	s.Register(m1)
	s.Unregister(m2)

	c.Check(s.NumUris(), Equals, 1)
}

func (s *RegistrySuite) TestLookup(c *C) {
	m := &route.Endpoint{
		Host: "192.168.1.1",
		Port: 1234,
		Uris: []route.Uri{"foo.vcap.me"},
	}

	s.Register(m)

	var b *route.Endpoint
	var ok bool

	b, ok = s.Lookup("foo.vcap.me")
	c.Assert(ok, Equals, true)
	c.Check(b.CanonicalAddr(), Equals, "192.168.1.1:1234")

	b, ok = s.Lookup("FOO.VCAP.ME")
	c.Assert(ok, Equals, true)
	c.Check(b.CanonicalAddr(), Equals, "192.168.1.1:1234")
}

func (s *RegistrySuite) TestLookupDoubleRegister(c *C) {
	m1 := &route.Endpoint{
		Host: "192.168.1.2",
		Port: 1234,
		Uris: []route.Uri{"bar.vcap.me", "barr.vcap.me"},
	}

	m2 := &route.Endpoint{
		Host: "192.168.1.2",
		Port: 1235,
		Uris: []route.Uri{"bar.vcap.me", "barr.vcap.me"},
	}

	s.Register(m1)
	s.Register(m2)

	c.Check(s.NumUris(), Equals, 2)
	c.Check(s.NumEndpoints(), Equals, 2)
}

func (s *RegistrySuite) TestTracker(c *C) {
	s.Register(fooEndpoint)
	s.Register(barEndpoint)
	c.Assert(s.staleTracker.Len(), Equals, 2)

	s.Unregister(fooEndpoint)
	s.Unregister(barEndpoint)
	c.Assert(s.staleTracker.Len(), Equals, 0)
}

func (s *RegistrySuite) TestMessageBusPingTimesout(c *C) {

}

func (s *RegistrySuite) TestPruneStaleApps(c *C) {
	s.Register(fooEndpoint)
	s.Register(barEndpoint)
	c.Check(s.NumUris(), Equals, 4)
	c.Check(s.NumEndpoints(), Equals, 2)
	c.Assert(s.staleTracker.Len(), Equals, 2)

	time.Sleep(s.dropletStaleThreshold + 1*time.Millisecond)
	s.PruneStaleDroplets()

	s.Register(bar2Endpoint)

	c.Check(s.NumUris(), Equals, 2)
	c.Check(s.NumEndpoints(), Equals, 1)
	c.Assert(s.staleTracker.Len(), Equals, 1)
}

func (s *RegistrySuite) TestPruneStaleAppsWhenStateStale(c *C) {
	s.Register(fooEndpoint)
	s.Register(barEndpoint)
	c.Check(s.NumUris(), Equals, 4)
	c.Check(s.NumEndpoints(), Equals, 2)
	c.Assert(s.staleTracker.Len(), Equals, 2)

	time.Sleep(s.dropletStaleThreshold + 1*time.Millisecond)

	s.messageBus.OnPing(func() bool { return false })

	time.Sleep(s.dropletStaleThreshold + 1*time.Millisecond)

	s.PruneStaleDroplets()

	c.Check(s.NumUris(), Equals, 4)
	c.Check(s.NumEndpoints(), Equals, 2)
	c.Assert(s.staleTracker.Len(), Equals, 2)

	routeEndpoint, _ := s.Lookup("foo.vcap.me")
	c.Assert(s.IsStale(routeEndpoint), Equals, false)

	routeEndpoint, _ = s.Lookup("bar.vcap.me")
	c.Assert(s.IsStale(routeEndpoint), Equals, false)
}

func (s *RegistrySuite) TestPruneStaleDropletsDoesNotDeadlock(c *C) {
	// when pruning stale droplets,
	// and the stale check takes a while,
	// and a read request comes in (i.e. from Lookup),
	// the read request completes before the stale check

	s.Register(fooEndpoint)

	completeSequence := make(chan string)

	s.messageBus.OnPing(func() bool {
		time.Sleep(5 * time.Second)
		completeSequence <- "stale"
		return false
	})

	go s.PruneStaleDroplets()

	go func() {
		s.Lookup("foo.vcap.me")
		completeSequence <- "lookup"
	}()

	firstCompleted := <-completeSequence

	c.Assert(firstCompleted, Equals, "lookup")
}

func (s *RegistrySuite) TestInfoMarshalling(c *C) {
	m := &route.Endpoint{
		Host: "192.168.1.1",
		Port: 1234,
		Uris: []route.Uri{"foo.vcap.me"},
	}

	s.Register(m)
	marshalled, err := json.Marshal(s)
	c.Check(err, IsNil)

	c.Check(string(marshalled), Equals, "{\"foo.vcap.me\":[\"192.168.1.1:1234\"]}")
}

func (s *RegistrySuite) TestIsStale(c *C) {
	s.Register(fooEndpoint)

	routeEndpoint, _ := s.Lookup("foo.vcap.me")
	c.Assert(s.IsStale(routeEndpoint), Equals, false)

	time.Sleep(s.dropletStaleThreshold + 1*time.Millisecond)

	c.Assert(s.IsStale(routeEndpoint), Equals, true)
}
