package router

import (
	. "launchpad.net/gocheck"
	"math"
)

type EPSuite struct{}

func init() {
	Suite(&EPSuite{})
}

func (s *EPSuite) TestEndpointPoolAddingAndRemoving(c *C) {
	pool := NewEndpointPool()

	endpoint := &RouteEndpoint{}

	pool.Add(endpoint)

	foundEndpoint, found := pool.Sample()
	c.Assert(found, Equals, true)
	c.Assert(foundEndpoint, Equals, endpoint)

	pool.Remove(endpoint)

	_, found = pool.Sample()
	c.Assert(found, Equals, false)
}

func (s *EPSuite) TestEndpointPoolAddingDoesNotDuplicate(c *C) {
	pool := NewEndpointPool()

	endpoint := &RouteEndpoint{}

	pool.Add(endpoint)
	pool.Add(endpoint)

	foundEndpoint, found := pool.Sample()
	c.Assert(found, Equals, true)
	c.Assert(foundEndpoint, Equals, endpoint)

	pool.Remove(endpoint)

	_, found = pool.Sample()
	c.Assert(found, Equals, false)
}

func (s *EPSuite) TestEndpointPoolIsEmptyInitially(c *C) {
	c.Assert(NewEndpointPool().IsEmpty(), Equals, true)
}

func (s *EPSuite) TestEndpointPoolIsEmptyAfterRemovingEverything(c *C) {
	pool := NewEndpointPool()

	endpoint := &RouteEndpoint{}

	pool.Add(endpoint)

	c.Assert(pool.IsEmpty(), Equals, false)

	pool.Remove(endpoint)

	c.Assert(pool.IsEmpty(), Equals, true)
}

func (s *EPSuite) TestEndpointPoolFindByPrivateInstanceId(c *C) {
	pool := NewEndpointPool()

	endpointFoo := &RouteEndpoint{PrivateInstanceId: "foo"}
	endpointBar := &RouteEndpoint{PrivateInstanceId: "bar"}

	pool.Add(endpointFoo)
	pool.Add(endpointBar)

	foundEndpoint, found := pool.FindByPrivateInstanceId("foo")
	c.Assert(found, Equals, true)
	c.Assert(foundEndpoint, Equals, endpointFoo)

	foundEndpoint, found = pool.FindByPrivateInstanceId("bar")
	c.Assert(found, Equals, true)
	c.Assert(foundEndpoint, Equals, endpointBar)

	_, found = pool.FindByPrivateInstanceId("quux")
	c.Assert(found, Equals, false)
}

func (s *EPSuite) TestEndpointPoolSamplingIsRandomIsh(c *C) {
	pool := NewEndpointPool()

	endpoint1 := &RouteEndpoint{}
	endpoint2 := &RouteEndpoint{}

	pool.Add(endpoint1)
	pool.Add(endpoint2)

	var occurrences1, occurrences2 int

	for i := 0; i < 200; i += 1 {
		foundEndpoint, _ := pool.Sample()
		if foundEndpoint == endpoint1 {
			occurrences1 += 1
		} else {
			occurrences2 += 1
		}
	}

	c.Assert(occurrences1, Not(Equals), 0)
	c.Assert(occurrences2, Not(Equals), 0)

	// they should be arbitrarily close
	c.Assert(math.Abs(float64(occurrences1-occurrences2)) < 50, Equals, true)
}

func (s *EPSuite) TestEndpointPoolMarshalsAsJSON(c *C) {
	pool := NewEndpointPool()

	pool.Add(&RouteEndpoint{Host: "1.2.3.4", Port: 5678})
	pool.Add(&RouteEndpoint{Host: "1.2.3.4", Port: 5678})

	json, err := pool.MarshalJSON()
	c.Assert(err, IsNil)

	// just to test without caring about order
	c.Assert(string(json), Equals, `["1.2.3.4:5678","1.2.3.4:5678"]`)
}
