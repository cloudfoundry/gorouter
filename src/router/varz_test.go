package router

import (
	"encoding/json"
	"fmt"
	. "launchpad.net/gocheck"
	"net/http"
	"time"
)

type VarzSuite struct {
	*Varz
}

var _ = Suite(&VarzSuite{})

func (s *VarzSuite) SetUpTest(c *C) {
	s.Varz = NewVarz()
	s.Varz.Registry = NewRegistry()
}

func (s *VarzSuite) f(x ...string) interface{} {
	var z interface{}
	var ok bool

	b, err := json.Marshal(s.Varz)
	if err != nil {
		panic(err)
	}

	y := make(map[string]interface{})
	err = json.Unmarshal(b, &y)
	if err != nil {
		panic(err)
	}

	z = y

	for _, e := range x {
		u := z.(map[string]interface{})
		z, ok = u[e]
		if !ok {
			panic(fmt.Sprintf("no key: %s", e))
		}
	}

	return z
}

func (s *VarzSuite) TestEmptyUniqueVarz(c *C) {
	v := s.Varz

	members := []string{
		"all",
		"tags",
		"urls",
		"droplets",
		"bad_requests",
		"requests_per_sec",
		"top10_app_requests",
	}

	validateJsonMembers(v, members, c)
}

func validateJsonMembers(v interface{}, members []string, c *C) {
	b, e := json.Marshal(v)
	c.Assert(e, IsNil)

	d := make(map[string]interface{})
	e = json.Unmarshal(b, &d)
	c.Assert(e, IsNil)

	for _, k := range members {
		if _, ok := d[k]; !ok {
			c.Fatalf(`member "%s" not found`, k)
		}
	}
}

func readVarzMemberFromJson(v interface{}, k string, c *C) interface{} {
	b, e := json.Marshal(v)
	c.Assert(e, IsNil)

	d := make(map[string]interface{})
	e = json.Unmarshal(b, &d)
	c.Assert(e, IsNil)

	return d[k]
}

func (s *VarzSuite) TestUpdateBadRequests(c *C) {
	r := http.Request{}

	s.CaptureBadRequest(&r)
	c.Assert(s.f("bad_requests"), Equals, float64(1))

	s.CaptureBadRequest(&r)
	c.Assert(s.f("bad_requests"), Equals, float64(2))
}

func (s *VarzSuite) TestUpdateRequests(c *C) {
	b := Backend{}
	r := http.Request{}

	s.CaptureBackendRequest(b, &r)
	c.Assert(s.f("all", "requests"), Equals, float64(1))

	s.CaptureBackendRequest(b, &r)
	c.Assert(s.f("all", "requests"), Equals, float64(2))
}

func (s *VarzSuite) TestUpdateRequestsWithTags(c *C) {
	b1 := Backend{
		Tags: map[string]string{
			"component": "cc",
			"runtime":   "ruby18",
			"framework": "sinatra",
		},
	}

	b2 := Backend{
		Tags: map[string]string{
			"component": "cc",
			"runtime":   "ruby18",
			"framework": "rails",
		},
	}

	r1 := http.Request{}
	r2 := http.Request{}

	s.CaptureBackendRequest(b1, &r1)
	s.CaptureBackendRequest(b2, &r2)

	c.Assert(s.f("tags", "component", "cc", "requests"), Equals, float64(2))
	c.Assert(s.f("tags", "runtime", "ruby18", "requests"), Equals, float64(2))
	c.Assert(s.f("tags", "framework", "sinatra", "requests"), Equals, float64(1))
	c.Assert(s.f("tags", "framework", "rails", "requests"), Equals, float64(1))
}

func (s *VarzSuite) TestUpdateResponse(c *C) {
	var b Backend
	var d time.Duration

	r1 := &http.Response{
		StatusCode: http.StatusOK,
	}

	r2 := &http.Response{
		StatusCode: http.StatusNotFound,
	}

	s.CaptureBackendResponse(b, r1, d)
	s.CaptureBackendResponse(b, r2, d)
	s.CaptureBackendResponse(b, r2, d)

	c.Assert(s.f("all", "responses_2xx"), Equals, float64(1))
	c.Assert(s.f("all", "responses_4xx"), Equals, float64(2))
}

func (s *VarzSuite) TestUpdateResponseWithTags(c *C) {
	var d time.Duration

	b1 := Backend{
		Tags: map[string]string{
			"component": "cc",
			"runtime":   "ruby18",
			"framework": "sinatra",
		},
	}

	b2 := Backend{
		Tags: map[string]string{
			"component": "cc",
			"runtime":   "ruby18",
			"framework": "rails",
		},
	}

	r1 := &http.Response{
		StatusCode: http.StatusOK,
	}

	r2 := &http.Response{
		StatusCode: http.StatusNotFound,
	}

	s.CaptureBackendResponse(b1, r1, d)
	s.CaptureBackendResponse(b2, r2, d)
	s.CaptureBackendResponse(b2, r2, d)

	c.Assert(s.f("tags", "component", "cc", "responses_2xx"), Equals, float64(1))
	c.Assert(s.f("tags", "component", "cc", "responses_4xx"), Equals, float64(2))
	c.Assert(s.f("tags", "runtime", "ruby18", "responses_2xx"), Equals, float64(1))
	c.Assert(s.f("tags", "runtime", "ruby18", "responses_4xx"), Equals, float64(2))
	c.Assert(s.f("tags", "framework", "sinatra", "responses_2xx"), Equals, float64(1))
	c.Assert(s.f("tags", "framework", "sinatra", "responses_4xx"), Equals, float64(0))
	c.Assert(s.f("tags", "framework", "rails", "responses_2xx"), Equals, float64(0))
	c.Assert(s.f("tags", "framework", "rails", "responses_4xx"), Equals, float64(2))
}
