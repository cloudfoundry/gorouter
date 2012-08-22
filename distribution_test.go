package router

import (
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
