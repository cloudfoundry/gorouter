package registry

import (
	"encoding/json"
	"time"

	"github.com/cloudfoundry/yagnats/fakeyagnats"
	. "launchpad.net/gocheck"

	"github.com/cloudfoundry/gorouter/config"
	"github.com/cloudfoundry/gorouter/route"
)

type CFRegistrySuite struct {
	r *CFRegistry

	messageBus *fakeyagnats.FakeYagnats
}

var _ = Suite(&CFRegistrySuite{})

var fooEndpoint, barEndpoint, bar2Endpoint *route.Endpoint
var configObj *config.Config

func (s *CFRegistrySuite) SetUpTest(c *C) {

	configObj = config.DefaultConfig()
	configObj.DropletStaleThreshold = 10 * time.Millisecond

	s.messageBus = fakeyagnats.New()
	s.r = NewCFRegistry(configObj, s.messageBus)

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
}

func (s *CFRegistrySuite) TestRegister(c *C) {
	s.r.Register("foo", fooEndpoint)
	s.r.Register("fooo", fooEndpoint)
	c.Check(s.r.NumUris(), Equals, 2)
	firstUpdateTime := s.r.TimeOfLastUpdate()

	s.r.Register("bar", barEndpoint)
	s.r.Register("baar", barEndpoint)
	c.Check(s.r.NumUris(), Equals, 4)
	secondUpdateTime := s.r.TimeOfLastUpdate()

	c.Assert(secondUpdateTime.After(firstUpdateTime), Equals, true)
}

func (s *CFRegistrySuite) TestRegisterIgnoreDuplicates(c *C) {
	s.r.Register("bar", barEndpoint)
	s.r.Register("baar", barEndpoint)

	c.Check(s.r.NumUris(), Equals, 2)
	c.Check(s.r.NumEndpoints(), Equals, 1)

	s.r.Register("bar", barEndpoint)
	s.r.Register("baar", barEndpoint)

	c.Check(s.r.NumUris(), Equals, 2)
	c.Check(s.r.NumEndpoints(), Equals, 1)

	s.r.Unregister("bar", barEndpoint)

	c.Check(s.r.NumUris(), Equals, 1)
	c.Check(s.r.NumEndpoints(), Equals, 1)

	s.r.Unregister("baar", barEndpoint)

	c.Check(s.r.NumUris(), Equals, 0)
	c.Check(s.r.NumEndpoints(), Equals, 0)
}

func (s *CFRegistrySuite) TestRegisterUppercase(c *C) {
	m1 := &route.Endpoint{
		Host: "192.168.1.1",
		Port: 1234,
	}

	m2 := &route.Endpoint{
		Host: "192.168.1.1",
		Port: 1235,
	}

	s.r.Register("foo", m1)
	s.r.Register("FOO", m2)

	c.Check(s.r.NumUris(), Equals, 1)
}

func (s *CFRegistrySuite) TestRegisterDoesntReplaceSameEndpoint(c *C) {
	m1 := &route.Endpoint{
		Host: "192.168.1.1",
		Port: 1234,
	}

	m2 := &route.Endpoint{
		Host: "192.168.1.1",
		Port: 1234,
	}

	s.r.Register("foo", m1)
	s.r.Register("bar", m2)

	c.Check(s.r.NumUris(), Equals, 2)
	c.Check(s.r.NumEndpoints(), Equals, 1)
}

func (s *CFRegistrySuite) TestUnregister(c *C) {
	s.r.Register("bar", barEndpoint)
	s.r.Register("baar", barEndpoint)
	c.Check(s.r.NumUris(), Equals, 2)
	c.Check(s.r.NumEndpoints(), Equals, 1)

	s.r.Register("bar", bar2Endpoint)
	s.r.Register("baar", bar2Endpoint)
	c.Check(s.r.NumUris(), Equals, 2)
	c.Check(s.r.NumEndpoints(), Equals, 2)

	s.r.Unregister("bar", barEndpoint)
	s.r.Unregister("baar", barEndpoint)
	c.Check(s.r.NumUris(), Equals, 2)
	c.Check(s.r.NumEndpoints(), Equals, 1)

	s.r.Unregister("bar", bar2Endpoint)
	s.r.Unregister("baar", bar2Endpoint)
	c.Check(s.r.NumUris(), Equals, 0)
	c.Check(s.r.NumEndpoints(), Equals, 0)
}

func (s *CFRegistrySuite) TestUnregisterUppercase(c *C) {
	m1 := &route.Endpoint{
		Host: "192.168.1.1",
		Port: 1234,
	}

	m2 := &route.Endpoint{
		Host: "192.168.1.1",
		Port: 1234,
	}

	s.r.Register("foo", m1)
	s.r.Unregister("FOO", m2)

	c.Check(s.r.NumUris(), Equals, 0)
}

func (s *CFRegistrySuite) TestUnregisterDoesntDemolish(c *C) {
	m1 := &route.Endpoint{
		Host: "192.168.1.1",
		Port: 1234,
	}

	m2 := &route.Endpoint{
		Host: "192.168.1.1",
		Port: 1234,
	}

	s.r.Register("foo", m1)
	s.r.Register("bar", m1)

	s.r.Unregister("foo", m2)

	c.Check(s.r.NumUris(), Equals, 1)
}

func (s *CFRegistrySuite) TestLookup(c *C) {
	m := &route.Endpoint{
		Host: "192.168.1.1",
		Port: 1234,
	}

	s.r.Register("foo", m)

	var b *route.Endpoint
	var ok bool

	b, ok = s.r.Lookup("foo")
	c.Assert(ok, Equals, true)
	c.Check(b.CanonicalAddr(), Equals, "192.168.1.1:1234")

	b, ok = s.r.Lookup("FOO")
	c.Assert(ok, Equals, true)
	c.Check(b.CanonicalAddr(), Equals, "192.168.1.1:1234")
}

func (s *CFRegistrySuite) TestLookupDoubleRegister(c *C) {
	m1 := &route.Endpoint{
		Host: "192.168.1.2",
		Port: 1234,
	}

	m2 := &route.Endpoint{
		Host: "192.168.1.2",
		Port: 1235,
	}

	s.r.Register("bar", m1)
	s.r.Register("barr", m1)

	s.r.Register("bar", m2)
	s.r.Register("barr", m2)

	c.Check(s.r.NumUris(), Equals, 2)
	c.Check(s.r.NumEndpoints(), Equals, 2)
}

func (s *CFRegistrySuite) TestPruneStaleApps(c *C) {
	s.r.Register("foo", fooEndpoint)
	s.r.Register("fooo", fooEndpoint)

	s.r.Register("bar", barEndpoint)
	s.r.Register("baar", barEndpoint)

	c.Check(s.r.NumUris(), Equals, 4)
	c.Check(s.r.NumEndpoints(), Equals, 2)

	time.Sleep(configObj.DropletStaleThreshold + 1*time.Millisecond)
	s.r.PruneStaleDroplets()

	s.r.Register("bar", bar2Endpoint)
	s.r.Register("baar", bar2Endpoint)

	c.Check(s.r.NumUris(), Equals, 2)
	c.Check(s.r.NumEndpoints(), Equals, 1)
}

func (s *CFRegistrySuite) TestPruningIsByUriNotJustAddr(c *C) {
	endpoint := &route.Endpoint{
		Host: "192.168.1.1",
		Port: 1234,
	}

	s.r.Register("foo", endpoint)
	s.r.Register("bar", endpoint)

	s.r.Register("foo", endpoint)

	c.Check(s.r.NumUris(), Equals, 2)
	c.Check(s.r.NumEndpoints(), Equals, 1)

	time.Sleep(configObj.DropletStaleThreshold + 1*time.Millisecond)

	s.r.Register("foo", endpoint)

	s.r.PruneStaleDroplets()

	c.Check(s.r.NumUris(), Equals, 1)
	c.Check(s.r.NumEndpoints(), Equals, 1)

	foundEndpoint, found := s.r.Lookup("foo")
	c.Check(found, Equals, true)
	c.Check(foundEndpoint, DeepEquals, endpoint)

	_, found = s.r.Lookup("bar")
	c.Check(found, Equals, false)
}

func (s *CFRegistrySuite) TestPruneStaleAppsWhenStateStale(c *C) {
	s.r.Register("foo", fooEndpoint)
	s.r.Register("fooo", fooEndpoint)

	s.r.Register("bar", barEndpoint)
	s.r.Register("baar", barEndpoint)

	c.Check(s.r.NumUris(), Equals, 4)
	c.Check(s.r.NumEndpoints(), Equals, 2)

	time.Sleep(configObj.DropletStaleThreshold + 1*time.Millisecond)

	s.messageBus.OnPing(func() bool { return false })

	time.Sleep(configObj.DropletStaleThreshold + 1*time.Millisecond)

	s.r.PruneStaleDroplets()

	c.Check(s.r.NumUris(), Equals, 4)
	c.Check(s.r.NumEndpoints(), Equals, 2)
}

func (s *CFRegistrySuite) TestPruneStaleDropletsDoesNotDeadlock(c *C) {
	// when pruning stale droplets,
	// and the stale check takes a while,
	// and a read request comes in (i.e. from Lookup),
	// the read request completes before the stale check

	s.r.Register("foo", fooEndpoint)
	s.r.Register("fooo", fooEndpoint)

	completeSequence := make(chan string)

	s.messageBus.OnPing(func() bool {
		time.Sleep(5 * time.Second)
		completeSequence <- "stale"
		return false
	})

	go s.r.PruneStaleDroplets()

	go func() {
		s.r.Lookup("foo")
		completeSequence <- "lookup"
	}()

	firstCompleted := <-completeSequence

	c.Assert(firstCompleted, Equals, "lookup")
}

func (s *CFRegistrySuite) TestInfoMarshalling(c *C) {
	m := &route.Endpoint{
		Host: "192.168.1.1",
		Port: 1234,
	}

	s.r.Register("foo", m)
	marshalled, err := json.Marshal(s.r)
	c.Check(err, IsNil)

	c.Check(string(marshalled), Equals, "{\"foo\":[\"192.168.1.1:1234\"]}")
}
