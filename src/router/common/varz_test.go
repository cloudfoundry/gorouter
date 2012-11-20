package common

import (
	. "launchpad.net/gocheck"
)

type VarzSuite struct {
}

var _ = Suite(&VarzSuite{})

func (s *VarzSuite) TestParseSimpleVarz(c *C) {
	type metrics struct {
		Responses2xx int `json:"responses_2xx"`
		ResponsesXxx int `json:"-"`
	}
	m := metrics{
		Responses2xx: 12,
		ResponsesXxx: 10,
	}

	d := parseVarz(m)

	c.Assert(d["-"], Equals, nil)
	c.Assert(d["ResponsesXxx"], IsNil)
	c.Assert(d["responses_2xx"], Equals, 12)
}

func (s *VarzSuite) TestParseComplexVarz(c *C) {
	type Foo struct {
		Bar string `json:"bar"`
	}
	type metrics struct {
		Foo `json:"foo" encode:"yes"` // anonymous field

		Responses2xx int `json:"responses_2xx"`
		ResponsesXxx int `json:"-"`
	}
	type varz struct {
		Type    string  `json:"type"`
		Metrics metrics `json:"metrics" encode:"yes"`
	}

	m := metrics{
		Responses2xx: 12,
		ResponsesXxx: 10,
	}
	m.Bar = "whatever"
	v := varz{
		Type:    "Router",
		Metrics: m,
	}

	d := parseVarz(v)

	c.Assert(d["type"], Equals, "Router")
	c.Assert(d["metrics"], IsNil)
	c.Assert(d["responses_2xx"], Equals, 12)
	c.Assert(d["foo"], IsNil)
	c.Assert(d["bar"], Equals, m.Bar)
}
