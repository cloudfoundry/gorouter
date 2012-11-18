package common

import (
	"encoding/json"
	. "launchpad.net/gocheck"
	"time"
)

type DurationSuite struct {
}

var _ = Suite(&DurationSuite{})

func (s *DurationSuite) TestJsonInterface(c *C) {
	d := Duration(123456)
	var i interface{} = &d

	_, ok := i.(json.Marshaler)
	c.Assert(ok, Equals, true)

	_, ok = i.(json.Unmarshaler)
	c.Assert(ok, Equals, true)
}

func (s *DurationSuite) TestMarshalJSON(c *C) {
	d := Duration(time.Hour*36 + time.Second*10)
	b, err := json.Marshal(d)
	c.Assert(err, IsNil)
	c.Assert(string(b), Equals, `"1d:12h:0m:10s"`)
}

func (s *DurationSuite) TestUnmarshalJSON(c *C) {
	d := Duration(time.Hour*36 + time.Second*20)
	b, err := json.Marshal(d)
	c.Assert(err, IsNil)

	var dd Duration
	dd.UnmarshalJSON(b)
	c.Assert(dd, Equals, d)
}
