package schema_test

import (
	"github.com/cloudfoundry/gorouter/access_log/schema"

	router_http "github.com/cloudfoundry/gorouter/common/http"
	"github.com/cloudfoundry/gorouter/route"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	"net/http"
	"net/url"
	"time"
)

var _ = Describe("AccessLogRecord", func() {

	It("Makes a record with all values", func() {
		record := schema.AccessLogRecord{
			Request: &http.Request{
				Host:   "FakeRequestHost",
				Method: "FakeRequestMethod",
				Proto:  "FakeRequestProto",
				URL: &url.URL{
					Opaque: "http://example.com/request",
				},
				Header: http.Header{
					"Referer":                       []string{"FakeReferer"},
					"User-Agent":                    []string{"FakeUserAgent"},
					"X-Forwarded-For":               []string{"FakeProxy1, FakeProxy2"},
					"X-Forwarded-Proto":             []string{"FakeOriginalRequestProto"},
					router_http.VcapRequestIdHeader: []string{"abc-123-xyz-pdq"},
				},
				RemoteAddr: "FakeRemoteAddr",
			},
			BodyBytesSent: 23,
			StatusCode:    200,
			RouteEndpoint: &route.Endpoint{
				ApplicationId: "FakeApplicationId",
			},
			StartedAt:            time.Date(2000, time.January, 1, 0, 0, 0, 0, time.UTC),
			FinishedAt:           time.Date(2000, time.January, 1, 0, 1, 0, 0, time.UTC),
			RequestBytesReceived: 30,
		}

		recordString := "FakeRequestHost - " +
			"[01/01/2000:00:00:00.000 +0000] " +
			"\"FakeRequestMethod http://example.com/request FakeRequestProto\" " +
			"200 " +
			"30 " +
			"23 " +
			"\"FakeReferer\" " +
			"\"FakeUserAgent\" " +
			"FakeRemoteAddr " +
			"x_forwarded_for:\"FakeProxy1, FakeProxy2\" " +
			"x_forwarded_proto:\"FakeOriginalRequestProto\" " +
			"vcap_request_id:abc-123-xyz-pdq " +
			"response_time:60 " +
			"app_id:FakeApplicationId" +
			"\n"

		Expect(record.LogMessage()).To(Equal(recordString))
	})

	It("Makes a record with values missing", func() {
		record := schema.AccessLogRecord{
			Request: &http.Request{
				Host:   "FakeRequestHost",
				Method: "FakeRequestMethod",
				Proto:  "FakeRequestProto",
				URL: &url.URL{
					Opaque: "http://example.com/request",
				},
				Header:     http.Header{},
				RemoteAddr: "FakeRemoteAddr",
			},
			RouteEndpoint: &route.Endpoint{
				ApplicationId: "FakeApplicationId",
			},
			StartedAt: time.Date(2000, time.January, 1, 0, 0, 0, 0, time.UTC),
		}

		recordString := "FakeRequestHost - " +
			"[01/01/2000:00:00:00.000 +0000] " +
			"\"FakeRequestMethod http://example.com/request FakeRequestProto\" " +
			"- " +
			"0 " +
			"0 " +
			"\"-\" " +
			"\"-\" " +
			"FakeRemoteAddr " +
			"x_forwarded_for:\"-\" " +
			"x_forwarded_proto:\"-\" " +
			"vcap_request_id:- " +
			"response_time:- " +
			"app_id:FakeApplicationId" +
			"\n"

		Expect(record.LogMessage()).To(Equal(recordString))
	})

	It("does not create a log message when route endpoint missing", func() {
		record := schema.AccessLogRecord{}
		Expect(record.LogMessage()).To(Equal(""))
	})

	It("Appends extra headers if specified", func() {
		record := schema.AccessLogRecord{
			Request: &http.Request{
				Host:   "FakeRequestHost",
				Method: "FakeRequestMethod",
				Proto:  "FakeRequestProto",
				URL: &url.URL{
					Opaque: "http://example.com/request",
				},
				Header: http.Header{
					"Cache-Control":   []string{"no-cache"},
					"Accept-Encoding": []string{"gzip, deflate"},
					"If-Match":        []string{"\"737060cd8c284d8af7ad3082f209582d\""},
				},
				RemoteAddr: "FakeRemoteAddr",
			},
			RouteEndpoint: &route.Endpoint{
				ApplicationId: "FakeApplicationId",
			},
			StartedAt:         time.Date(2000, time.January, 1, 0, 0, 0, 0, time.UTC),
			ExtraHeadersToLog: []string{"Cache-Control", "Accept-Encoding", "If-Match", "Doesnt-Exist"},
		}

		recordString := "FakeRequestHost - " +
			"[01/01/2000:00:00:00.000 +0000] " +
			"\"FakeRequestMethod http://example.com/request FakeRequestProto\" " +
			"- " +
			"0 " +
			"0 " +
			"\"-\" " +
			"\"-\" " +
			"FakeRemoteAddr " +
			"x_forwarded_for:\"-\" " +
			"x_forwarded_proto:\"-\" " +
			"vcap_request_id:- " +
			"response_time:- " +
			"app_id:FakeApplicationId " +
			"cache_control:\"no-cache\" " +
			"accept_encoding:\"gzip, deflate\" " +
			"if_match:\"\\\"737060cd8c284d8af7ad3082f209582d\\\"\" " +
			"doesnt_exist:\"-\"" +
			"\n"

		Expect(record.LogMessage()).To(Equal(recordString))
	})
})
