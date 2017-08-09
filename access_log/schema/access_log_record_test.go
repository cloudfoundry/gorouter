package schema_test

import (
	"bytes"

	"code.cloudfoundry.org/gorouter/access_log/schema"
	"code.cloudfoundry.org/gorouter/handlers"
	"code.cloudfoundry.org/routing-api/models"

	"code.cloudfoundry.org/gorouter/route"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	"net/http"
	"net/url"
	"time"
)

var _ = Describe("AccessLogRecord", func() {
	var (
		endpoint *route.Endpoint
		record   *schema.AccessLogRecord
	)
	BeforeEach(func() {
		endpoint = route.NewEndpoint("FakeApplicationId", "1.2.3.4", 1234, "", "3", nil, 0, "", models.ModificationTag{}, "", false)
		record = &schema.AccessLogRecord{
			Request: &http.Request{
				Host:   "FakeRequestHost",
				Method: "FakeRequestMethod",
				Proto:  "FakeRequestProto",
				URL: &url.URL{
					Opaque: "http://example.com/request",
				},
				Header: http.Header{
					"Referer":                    []string{"FakeReferer"},
					"User-Agent":                 []string{"FakeUserAgent"},
					"X-Forwarded-For":            []string{"FakeProxy1, FakeProxy2"},
					"X-Forwarded-Proto":          []string{"FakeOriginalRequestProto"},
					handlers.VcapRequestIdHeader: []string{"abc-123-xyz-pdq"},
				},
				RemoteAddr: "FakeRemoteAddr",
			},
			BodyBytesSent:        23,
			StatusCode:           200,
			RouteEndpoint:        endpoint,
			StartedAt:            time.Date(2000, time.January, 1, 0, 0, 0, 0, time.UTC),
			FinishedAt:           time.Date(2000, time.January, 1, 0, 1, 0, 0, time.UTC),
			RequestBytesReceived: 30,
		}
	})

	Describe("LogMessage", func() {
		It("Makes a record with all values", func() {
			recordString := "FakeRequestHost - " +
				"[2000-01-01T00:00:00.000+0000] " +
				`"FakeRequestMethod http://example.com/request FakeRequestProto" ` +
				"200 " +
				"30 " +
				"23 " +
				`"FakeReferer" ` +
				`"FakeUserAgent" ` +
				`"FakeRemoteAddr" ` +
				`"1.2.3.4:1234" ` +
				`x_forwarded_for:"FakeProxy1, FakeProxy2" ` +
				`x_forwarded_proto:"FakeOriginalRequestProto" ` +
				`vcap_request_id:"abc-123-xyz-pdq" ` +
				`response_time:60 ` +
				`app_id:"FakeApplicationId" ` +
				`app_index:"3"` +
				"\n"

			Expect(record.LogMessage()).To(Equal(recordString))
		})

		Context("with values missing", func() {
			BeforeEach(func() {
				record.Request.Header = http.Header{}
				record.RouteEndpoint = &route.Endpoint{
					ApplicationId:        "FakeApplicationId",
					PrivateInstanceIndex: "",
				}
				record.BodyBytesSent = 0
				record.StatusCode = 0
				record.FinishedAt = time.Time{}
				record.RequestBytesReceived = 0
			})
			It("makes a record", func() {
				recordString := "FakeRequestHost - " +
					"[2000-01-01T00:00:00.000+0000] " +
					`"FakeRequestMethod http://example.com/request FakeRequestProto" ` +
					`"-" ` +
					"0 " +
					"0 " +
					`"-" ` +
					`"-" ` +
					`"FakeRemoteAddr" ` +
					`"-" ` +
					`x_forwarded_for:"-" ` +
					`x_forwarded_proto:"-" ` +
					`vcap_request_id:"-" ` +
					`response_time:"-" ` +
					`app_id:"FakeApplicationId" ` +
					`app_index:"-"` +
					"\n"

				Expect(record.LogMessage()).To(Equal(recordString))
			})
		})

		Context("with route endpoint missing", func() {
			BeforeEach(func() {
				record = &schema.AccessLogRecord{}
			})
			It("does not create a log message", func() {
				Expect(record.LogMessage()).To(Equal(""))
			})
		})

		Context("with extra headers", func() {
			BeforeEach(func() {
				record.Request.Header.Set("Cache-Control", "no-cache")
				record.Request.Header.Set("Accept-Encoding", "gzip, deflate")
				record.Request.Header.Set("If-Match", "737060cd8c284d8af7ad3082f209582d")
				record.ExtraHeadersToLog = []string{"Cache-Control", "Accept-Encoding", "If-Match", "Doesnt-Exist"}
			})
			It("appends extra headers", func() {
				recordString := "FakeRequestHost - " +
					"[2000-01-01T00:00:00.000+0000] " +
					`"FakeRequestMethod http://example.com/request FakeRequestProto" ` +
					`200 ` +
					"30 " +
					"23 " +
					`"FakeReferer" ` +
					`"FakeUserAgent" ` +
					`"FakeRemoteAddr" ` +
					`"1.2.3.4:1234" ` +
					`x_forwarded_for:"FakeProxy1, FakeProxy2" ` +
					`x_forwarded_proto:"FakeOriginalRequestProto" ` +
					`vcap_request_id:"abc-123-xyz-pdq" ` +
					`response_time:60 ` +
					`app_id:"FakeApplicationId" ` +
					`app_index:"3" ` +
					`cache_control:"no-cache" ` +
					`accept_encoding:"gzip, deflate" ` +
					`if_match:"737060cd8c284d8af7ad3082f209582d" ` +
					`doesnt_exist:"-"` +
					"\n"

				Expect(record.LogMessage()).To(Equal(recordString))
			})
		})

		Context("when extra headers is an empty slice", func() {
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
							"Referer":                    []string{"FakeReferer"},
							"User-Agent":                 []string{"FakeUserAgent"},
							"X-Forwarded-For":            []string{"FakeProxy1, FakeProxy2"},
							"X-Forwarded-Proto":          []string{"FakeOriginalRequestProto"},
							handlers.VcapRequestIdHeader: []string{"abc-123-xyz-pdq"},
						},
						RemoteAddr: "FakeRemoteAddr",
					},
					BodyBytesSent:        23,
					StatusCode:           200,
					RouteEndpoint:        endpoint,
					StartedAt:            time.Date(2000, time.January, 1, 0, 0, 0, 0, time.UTC),
					FinishedAt:           time.Date(2000, time.January, 1, 0, 1, 0, 0, time.UTC),
					RequestBytesReceived: 30,
					ExtraHeadersToLog:    []string{},
				}

				recordString := "FakeRequestHost - " +
					"[2000-01-01T00:00:00.000+0000] " +
					`"FakeRequestMethod http://example.com/request FakeRequestProto" ` +
					"200 " +
					"30 " +
					"23 " +
					`"FakeReferer" ` +
					`"FakeUserAgent" ` +
					`"FakeRemoteAddr" ` +
					`"1.2.3.4:1234" ` +
					`x_forwarded_for:"FakeProxy1, FakeProxy2" ` +
					`x_forwarded_proto:"FakeOriginalRequestProto" ` +
					`vcap_request_id:"abc-123-xyz-pdq" ` +
					`response_time:60 ` +
					`app_id:"FakeApplicationId" ` +
					`app_index:"3"` +
					"\n"

				Expect(record.LogMessage()).To(Equal(recordString))
			})
		})
	})

	Describe("WriteTo", func() {
		It("writes the correct log line to the io.Writer", func() {
			recordString := "FakeRequestHost - " +
				"[2000-01-01T00:00:00.000+0000] " +
				`"FakeRequestMethod http://example.com/request FakeRequestProto" ` +
				"200 " +
				"30 " +
				"23 " +
				`"FakeReferer" ` +
				`"FakeUserAgent" ` +
				`"FakeRemoteAddr" ` +
				`"1.2.3.4:1234" ` +
				`x_forwarded_for:"FakeProxy1, FakeProxy2" ` +
				`x_forwarded_proto:"FakeOriginalRequestProto" ` +
				`vcap_request_id:"abc-123-xyz-pdq" ` +
				`response_time:60 ` +
				`app_id:"FakeApplicationId" ` +
				`app_index:"3"` +
				"\n"

			b := new(bytes.Buffer)
			_, err := record.WriteTo(b)
			Expect(err).ToNot(HaveOccurred())
			Expect(b.String()).To(Equal(recordString))
		})
	})

	Describe("ApplicationID", func() {
		var emptyRecord schema.AccessLogRecord
		Context("when RouteEndpoint is nil", func() {
			BeforeEach(func() {
				emptyRecord.RouteEndpoint = new(route.Endpoint)
			})
			It("returns empty string", func() {
				Expect(emptyRecord.ApplicationID()).To(Equal(""))
			})
		})
		Context("when RouteEndpoint.ApplicationId is empty", func() {
			BeforeEach(func() {
				emptyRecord.RouteEndpoint = new(route.Endpoint)
			})
			It("returns empty string", func() {
				Expect(emptyRecord.ApplicationID()).To(Equal(""))
			})
		})
		Context("when RouteEndpoint.ApplicationId is set", func() {
			BeforeEach(func() {
				emptyRecord.RouteEndpoint = endpoint
			})
			It("returns the application ID", func() {
				Expect(emptyRecord.ApplicationID()).To(Equal("FakeApplicationId"))
			})
		})
	})
})
