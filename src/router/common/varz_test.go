package common

import (
	"encoding/json"
	. "launchpad.net/gocheck"
)

type VarzSuite struct {
}

var _ = Suite(&VarzSuite{})

func (s *VarzSuite) SetUpTest(c *C) {
	Component = VcapComponent{
		Credentials: []string{"foo", "bar"},
		Config:      map[string]interface{}{"ip": "localhost", "port": 8080},
	}
}

func (s *VarzSuite) TearDownTest(c *C) {
	Component = VcapComponent{}
}

func (s *VarzSuite) TestEmptyVarz(c *C) {
	v := &Varz{}
	b, e := json.Marshal(v)
	c.Assert(e, IsNil)

	m := make(map[string]interface{})
	e = json.Unmarshal(b, &m)
	c.Assert(e, IsNil)

	members := []string{
		"type",
		"index",
		"host",
		"credentials",
		"config",
		"start",
		"uuid",
		"uptime",
		"num_cores",
		"mem",
		"cpu",
	}

	for _, k := range members {
		if _, ok := m[k]; !ok {
			c.Fatalf(`member "%s" not found`, k)
		}
	}
}

func (s *VarzSuite) TestTransformStruct(c *C) {
	component := struct {
		Type  string `json:"type"`
		Index int    `json:"index"`
	}{
		Type:  "Router",
		Index: 1,
	}

	m := make(map[string]interface{})
	transform(component, &m)
	c.Assert(m["type"], Equals, "Router")
	c.Assert(m["index"], Equals, float64(1))
}

func (s *VarzSuite) TestTransformMap(c *C) {
	data := map[string]interface{}{"type": "Dea", "index": 1}

	m := make(map[string]interface{})
	transform(data, &m)
	c.Assert(m["type"], Equals, "Dea")
	c.Assert(m["index"], Equals, float64(1))
}
