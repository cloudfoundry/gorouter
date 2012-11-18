package common

import (
	. "launchpad.net/gocheck"
)

type VarzSuite struct {
}

var _ = Suite(&VarzSuite{})

func (s *VarzSuite) TestParseFromUniqueVarz(c *C) {
	type metrics struct {
		Responses2xx int `json:"responses_2xx"`
		ResponsesXxx int `json:"-"`
	}

	data := make(map[string]interface{})
	m := metrics{
		Responses2xx: 12,
	}

	parseFromUniqueVarz(m, data)
	c.Assert(data["responses_2xx"], Equals, 12)
	c.Assert(data["responsesXxx"], Equals, nil)
	c.Assert(data["-"], Equals, nil)
}
