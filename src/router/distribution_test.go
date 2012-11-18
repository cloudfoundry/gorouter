package router

import (
	"encoding/json"
	. "launchpad.net/gocheck"
)

type DistributionSuite struct {
}

var _ = Suite(&DistributionSuite{})

func (s *DistributionSuite) TestDistribution(c *C) {
	a := NewDistribution(10, "name")
	b := NewDistribution(10, "name")

	c.Check(a, DeepEquals, b)

	a.Add(int64(10))
	a.Add(int64(20))

	c.Check(a.m.Snapshot().Count, Equals, uint64(2))
	c.Check(a.m.Snapshot().Mean, Equals, float64((10+20)/2))
}

func (s *DistributionSuite) TestMarshalJSON(c *C) {
	a := NewDistribution(10, "foobar")
	a.Add(int64(10))
	a.Add(int64(20))

	b, _ := a.MarshalJSON()
	var f interface{}
	err := json.Unmarshal(b, &f)
	c.Assert(err, IsNil)
	m := f.(map[string]interface{})

	// JSON numbers are unmarshaled as float64 type numbers in golang
	c.Assert(m["samples"], Equals, float64(2))
	c.Assert(m["value"], Equals, float64((10+20)/2))
}
