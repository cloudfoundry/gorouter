package common

import (
	. "launchpad.net/gocheck"
)

type CommonSuite struct{}

var _ = Suite(&CommonSuite{})

func (s *CommonSuite) TestUUID(c *C) {
	uuid, err := GenerateUUID()

	c.Assert(err, IsNil)
	c.Check(len(uuid), Equals, 36)
}
