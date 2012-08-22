package router

import (
	. "launchpad.net/gocheck"
)

var NO_TAG = map[string]string{}

type ServerStatusSuite struct {
	status *ServerStatus
}

var _ = Suite(&ServerStatusSuite{})

func (s *ServerStatusSuite) SetUpTest(c *C) {
	s.status = NewServerStatus()
}

func (s *ServerStatusSuite) TestNewStatus(c *C) {
	checkEmptyStatus(c, s.status)
}

func (s *ServerStatusSuite) TestUpdateStatusNilTag(c *C) {
	s.status.RecordResponse(200, 10, nil)

	c.Check(s.status.Responses2xx, Equals, 1)
}

func (s *ServerStatusSuite) TestUpdateStatusInvalidCode(c *C) {
	s.status.RecordResponse(-1, 10, NO_TAG)
	s.status.RecordResponse(999, 10, NO_TAG)

	c.Check(s.status.ResponsesXxx, Equals, 2)
}

func (s *ServerStatusSuite) TestUpdateStatusInvalidLatency(c *C) {
	s.status.RecordResponse(200, -10, NO_TAG)
	checkEmptyStatus(c, s.status)
}

func (s *ServerStatusSuite) TestUpdateStatusTag(c *C) {
	s.status = NewServerStatus()
	s.status.RecordResponse(200, 10, map[string]string{"component": "cc", "framework": "sinatra", "runtime": "ruby18"})
	s.status.checkResponseAndLatency(c, 200, 10)
	s.status.Tags["component"]["cc"].checkResponseAndLatency(c, 200, 10)
	s.status.Tags["framework"]["sinatra"].checkResponseAndLatency(c, 200, 10)
	s.status.Tags["runtime"]["ruby18"].checkResponseAndLatency(c, 200, 10)

	s.status = NewServerStatus()
	s.status.RecordResponse(300, 10, map[string]string{"component": "cc", "framework": "sinatra", "runtime": "ruby18"})
	s.status.checkResponseAndLatency(c, 300, 10)
	s.status.Tags["component"]["cc"].checkResponseAndLatency(c, 300, 10)
	s.status.Tags["framework"]["sinatra"].checkResponseAndLatency(c, 300, 10)
	s.status.Tags["runtime"]["ruby18"].checkResponseAndLatency(c, 300, 10)

	s.status = NewServerStatus()
	s.status.RecordResponse(400, 10, map[string]string{"component": "cc", "framework": "sinatra", "runtime": "ruby18"})
	s.status.checkResponseAndLatency(c, 400, 10)
	s.status.Tags["component"]["cc"].checkResponseAndLatency(c, 400, 10)
	s.status.Tags["framework"]["sinatra"].checkResponseAndLatency(c, 400, 10)
	s.status.Tags["runtime"]["ruby18"].checkResponseAndLatency(c, 400, 10)

	s.status = NewServerStatus()
	s.status.RecordResponse(500, 10, map[string]string{"component": "cc", "framework": "sinatra", "runtime": "ruby18"})
	s.status.checkResponseAndLatency(c, 500, 10)
	s.status.Tags["component"]["cc"].checkResponseAndLatency(c, 500, 10)
	s.status.Tags["framework"]["sinatra"].checkResponseAndLatency(c, 500, 10)
	s.status.Tags["runtime"]["ruby18"].checkResponseAndLatency(c, 500, 10)

	s.status = NewServerStatus()
	s.status.RecordResponse(999, 10, map[string]string{"component": "cc", "framework": "sinatra", "runtime": "ruby18"})
	s.status.checkResponseAndLatency(c, 999, 10)
	s.status.Tags["component"]["cc"].checkResponseAndLatency(c, 999, 10)
	s.status.Tags["framework"]["sinatra"].checkResponseAndLatency(c, 999, 10)
	s.status.Tags["runtime"]["ruby18"].checkResponseAndLatency(c, 999, 10)
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

func checkEmptyStatus(c *C, s *ServerStatus) {
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
