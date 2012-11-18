package router

import (
	. "launchpad.net/gocheck"
)

var NO_TAG = map[string]string{}

type VarzSuite struct {
	varz *Varz
}

var _ = Suite(&VarzSuite{})

func (s *VarzSuite) SetUpTest(c *C) {
	s.varz = NewVarz()
}

func (s *VarzSuite) TestNewVarz(c *C) {
	checkEmptyVarz(c, s.varz)
}

func (s *VarzSuite) TestUpdateRequests(c *C) {
	s.varz.IncRequests()
	c.Check(s.varz.Requests, Equals, 1)

	s.varz.IncRequests()
	c.Check(s.varz.Requests, Equals, 2)
}

func (s *VarzSuite) TestUpdateBadRequests(c *C) {
	s.varz.IncBadRequests()
	c.Check(s.varz.BadRequests, Equals, 1)

	s.varz.IncBadRequests()
	c.Check(s.varz.BadRequests, Equals, 2)
}

func (s *VarzSuite) TestUpdateRequestsTag(c *C) {
	s.varz.IncRequestsWithTags(map[string]string{"component": "cc", "framework": "sinatra", "runtime": "ruby18"})
	c.Check(s.varz.Tags["component"]["cc"].Requests, Equals, 1)
	c.Check(s.varz.Tags["framework"]["sinatra"].Requests, Equals, 1)
	c.Check(s.varz.Tags["runtime"]["ruby18"].Requests, Equals, 1)
}

func (s *VarzSuite) TestUpdateVarzNilTag(c *C) {
	s.varz.RecordResponse(200, 10, nil)

	c.Check(s.varz.Responses2xx, Equals, 1)
}

func (s *VarzSuite) TestUpdateVarzInvalidCode(c *C) {
	s.varz.RecordResponse(-1, 10, NO_TAG)
	s.varz.RecordResponse(999, 10, NO_TAG)

	c.Check(s.varz.ResponsesXxx, Equals, 2)
}

func (s *VarzSuite) TestUpdateVarzInvalidLatency(c *C) {
	s.varz.RecordResponse(200, -10, NO_TAG)
	checkEmptyVarz(c, s.varz)
}

func (s *VarzSuite) TestUpdateVarzTag(c *C) {
	s.varz = NewVarz()
	s.varz.RecordResponse(200, 10, map[string]string{"component": "cc", "framework": "sinatra", "runtime": "ruby18"})
	s.varz.checkResponseAndLatency(c, 200, 10)
	s.varz.Tags["component"]["cc"].checkResponseAndLatency(c, 200, 10)
	s.varz.Tags["framework"]["sinatra"].checkResponseAndLatency(c, 200, 10)
	s.varz.Tags["runtime"]["ruby18"].checkResponseAndLatency(c, 200, 10)

	s.varz = NewVarz()
	s.varz.RecordResponse(300, 10, map[string]string{"component": "cc", "framework": "sinatra", "runtime": "ruby18"})
	s.varz.checkResponseAndLatency(c, 300, 10)
	s.varz.Tags["component"]["cc"].checkResponseAndLatency(c, 300, 10)
	s.varz.Tags["framework"]["sinatra"].checkResponseAndLatency(c, 300, 10)
	s.varz.Tags["runtime"]["ruby18"].checkResponseAndLatency(c, 300, 10)

	s.varz = NewVarz()
	s.varz.RecordResponse(400, 10, map[string]string{"component": "cc", "framework": "sinatra", "runtime": "ruby18"})
	s.varz.checkResponseAndLatency(c, 400, 10)
	s.varz.Tags["component"]["cc"].checkResponseAndLatency(c, 400, 10)
	s.varz.Tags["framework"]["sinatra"].checkResponseAndLatency(c, 400, 10)
	s.varz.Tags["runtime"]["ruby18"].checkResponseAndLatency(c, 400, 10)

	s.varz = NewVarz()
	s.varz.RecordResponse(500, 10, map[string]string{"component": "cc", "framework": "sinatra", "runtime": "ruby18"})
	s.varz.checkResponseAndLatency(c, 500, 10)
	s.varz.Tags["component"]["cc"].checkResponseAndLatency(c, 500, 10)
	s.varz.Tags["framework"]["sinatra"].checkResponseAndLatency(c, 500, 10)
	s.varz.Tags["runtime"]["ruby18"].checkResponseAndLatency(c, 500, 10)

	s.varz = NewVarz()
	s.varz.RecordResponse(999, 10, map[string]string{"component": "cc", "framework": "sinatra", "runtime": "ruby18"})
	s.varz.checkResponseAndLatency(c, 999, 10)
	s.varz.Tags["component"]["cc"].checkResponseAndLatency(c, 999, 10)
	s.varz.Tags["framework"]["sinatra"].checkResponseAndLatency(c, 999, 10)
	s.varz.Tags["runtime"]["ruby18"].checkResponseAndLatency(c, 999, 10)
}

func (metric *HttpMetric) checkResponseAndLatency(c *C, httpCode int, latency int) {
	switch httpCode {
	case 200:
		c.Check(metric.Responses2xx, Equals, 1)
	case 300:
		c.Check(metric.Responses3xx, Equals, 1)
	case 400:
		c.Check(metric.Responses4xx, Equals, 1)
	case 500:
		c.Check(metric.Responses5xx, Equals, 1)
	default:
		c.Check(metric.ResponsesXxx, Equals, 1)
	}
	c.Check(metric.Latency.Snapshot().Mean, Equals, float64(latency))
	c.Check(metric.Latency.Snapshot().Count, Equals, uint64(1))
}

func checkEmptyVarz(c *C, s *Varz) {
	c.Check(s.Requests, Equals, 0)
	c.Check(s.Urls, Equals, 0)
	c.Check(s.Droplets, Equals, 0)
	c.Check(s.Responses2xx, Equals, 0)
	c.Check(s.Responses3xx, Equals, 0)
	c.Check(s.Responses4xx, Equals, 0)
	c.Check(s.Responses5xx, Equals, 0)
	c.Check(s.ResponsesXxx, Equals, 0)

	// there are 3 categories of tags
	c.Check(len(s.Tags), Equals, 3)

	for _, tag := range s.Tags {
		c.Check(len(tag), Equals, 0)
	}
}
