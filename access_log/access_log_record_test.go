package access_log

import (
	. "launchpad.net/gocheck"
	"net/http"
	"net/url"
	"time"

	"github.com/cloudfoundry/gorouter/route"
)

type AccessLogRecordSuite struct{}

var _ = Suite(&AccessLogRecordSuite{})

func CompleteAccessLogRecord() AccessLogRecord {
	return AccessLogRecord{
		Request: &http.Request{
			Host:   "FakeRequestHost",
			Method: "FakeRequestMethod",
			Proto:  "FakeRequestProto",
			URL: &url.URL{
				Opaque: "http://example.com/request",
			},
			Header: http.Header{
				"Referer":    []string{"FakeReferer"},
				"User-Agent": []string{"FakeUserAgent"},
				"X-Vcap-Request-Id": []string{"abc-123-xyz-pdq"},
			},
			RemoteAddr: "FakeRemoteAddr",
		},
		BodyBytesSent: 23,
		Response: &http.Response{
			StatusCode: 200,
		},
		RouteEndpoint: &route.Endpoint{
			ApplicationId: "FakeApplicationId",
		},
		StartedAt:  time.Date(2000, time.January, 1, 0, 0, 0, 0, time.UTC),
		FinishedAt: time.Date(2000, time.January, 1, 0, 1, 0, 0, time.UTC),
	}
}

func (s *AccessLogRecordSuite) TestMakeRecordWithAllValues(c *C) {
	record := CompleteAccessLogRecord()

	recordString := "FakeRequestHost - " +
		"[01/01/2000:00:00:00 +0000] " +
		"\"FakeRequestMethod http://example.com/request FakeRequestProto\" " +
		"200 " +
		"23 " +
		"\"FakeReferer\" " +
		"\"FakeUserAgent\" " +
		"FakeRemoteAddr " +
		"vcap_request:abc-123-xyz-pdq " +
		"response_time:60.000000000 " +
		"app_id:FakeApplicationId\n"

	c.Assert(record.makeRecord().String(), Equals, recordString)
}

func (s *AccessLogRecordSuite) TestMakeRecordWithValuesMissing(c *C) {
	record := AccessLogRecord{
		Request: &http.Request{
			Host:   "FakeRequestHost",
			Method: "FakeRequestMethod",
			Proto:  "FakeRequestProto",
			URL: &url.URL{
				Opaque: "http://example.com/request",
			},
			Header: http.Header{
				"Referer":    []string{"FakeReferer"},
				"User-Agent": []string{"FakeUserAgent"},
			},
			RemoteAddr: "FakeRemoteAddr",
		},
		StartedAt: time.Date(2000, time.January, 1, 0, 0, 0, 0, time.UTC),
	}

	recordString := "FakeRequestHost - " +
		"[01/01/2000:00:00:00 +0000] " +
		"\"FakeRequestMethod http://example.com/request FakeRequestProto\" " +
		"MissingResponseStatusCode " +
		"0 " +
		"\"FakeReferer\" " +
		"\"FakeUserAgent\" " +
		"FakeRemoteAddr " +
		"vcap_request:- " +
		"response_time:MissingFinishedAt " +
		"app_id:MissingRouteEndpointApplicationId\n"

	c.Assert(record.makeRecord().String(), Equals, recordString)
}

func (s *AccessLogRecordSuite) TestLogMessage(c *C) {
	record := CompleteAccessLogRecord()

	recordString := record.makeRecord().String()

	c.Assert(record.LogMessage(), Equals, recordString)
}

func (s *AccessLogRecordSuite) TestLogMessageWithRouteEndpointMissing(c *C) {
	record := AccessLogRecord{}
	c.Assert(record.LogMessage(), Equals, "")
}
