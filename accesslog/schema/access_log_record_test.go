package schema_test

import (
	"bytes"

	"code.cloudfoundry.org/gorouter/accesslog/schema"
	"code.cloudfoundry.org/gorouter/handlers"

	"code.cloudfoundry.org/gorouter/route"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	. "github.com/onsi/gomega/gbytes"

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
		endpoint = route.NewEndpoint(&route.EndpointOpts{
			AppId:                "FakeApplicationId",
			Host:                 "1.2.3.4",
			Port:                 1234,
			PrivateInstanceIndex: "3",
		})

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
			RoundtripStartedAt:   time.Date(2000, time.January, 1, 0, 0, 0, 0, time.UTC),
			RoundtripFinishedAt:  time.Date(2000, time.January, 1, 0, 1, 0, 0, time.UTC),
			AppRequestStartedAt:  time.Date(2000, time.January, 1, 0, 0, 5, 0, time.UTC),
			AppRequestFinishedAt: time.Date(2000, time.January, 1, 0, 0, 55, 0, time.UTC),
			RequestBytesReceived: 30,
		}
	})

	Describe("LogMessage", func() {
		It("makes a record with all values", func() {
			r := BufferReader(bytes.NewBufferString(record.LogMessage()))
			Eventually(r).Should(Say(`FakeRequestHost\s-\s\[\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}\.\d{9}Z\]`))
			Eventually(r).Should(Say(`"FakeRequestMethod http://example.com/request FakeRequestProto" `))
			Eventually(r).Should(Say(`200 30 23 "FakeReferer" "FakeUserAgent" "FakeRemoteAddr" `))
			Eventually(r).Should(Say(`"1.2.3.4:1234" x_forwarded_for:"FakeProxy1, FakeProxy2" `))
			Eventually(r).Should(Say(`x_forwarded_proto:"FakeOriginalRequestProto" `))
			Eventually(r).Should(Say(`vcap_request_id:"abc-123-xyz-pdq" response_time:60.000000 gorouter_time:10.000000 app_time:50.000000 app_id:"FakeApplicationId" `))
			Eventually(r).Should(Say(`app_index:"3"\n`))
		})

		Context("when DisableSourceIPLogging is specified", func() {
			It("does not write RemoteAddr as part of the access log", func() {
				record.DisableSourceIPLogging = true

				r := BufferReader(bytes.NewBufferString(record.LogMessage()))
				Consistently(r).ShouldNot(Say("FakeRemoteAddr"))
			})
		})

		Context("when DisableXFFLogging is specified", func() {
			It("does not write x_forwarded_for as part of the access log", func() {
				record.HeadersOverride = http.Header{
					"X-Forwarded-For": []string{"FooProxy1, FooProxy2"},
				}
				record.DisableXFFLogging = true

				r := BufferReader(bytes.NewBufferString(record.LogMessage()))
				Eventually(r).Should(Say(`x_forwarded_for:"-"`))
			})
		})

		Context("with HeadersOverride specified", func() {
			BeforeEach(func() {
				record.HeadersOverride = http.Header{
					"Referer":                    []string{"FooReferer"},
					"User-Agent":                 []string{"FooUserAgent"},
					"X-Forwarded-For":            []string{"FooProxy1, FooProxy2"},
					"X-Forwarded-Proto":          []string{"FooOriginalRequestProto"},
					handlers.VcapRequestIdHeader: []string{"abc-123-xyz-pdq"},
				}
			})

			It("makes a record with all values", func() {
				r := BufferReader(bytes.NewBufferString(record.LogMessage()))
				Eventually(r).Should(Say(`FakeRequestHost\s-\s\[\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}\.\d{9}Z\]`))
				Eventually(r).Should(Say(`"FakeRequestMethod http://example.com/request FakeRequestProto" `))
				Eventually(r).Should(Say(`200 30 23 "FooReferer" "FooUserAgent" "FakeRemoteAddr" `))
				Eventually(r).Should(Say(`"1.2.3.4:1234" x_forwarded_for:"FooProxy1, FooProxy2" `))
				Eventually(r).Should(Say(`x_forwarded_proto:"FooOriginalRequestProto" `))
				Eventually(r).Should(Say(`vcap_request_id:"abc-123-xyz-pdq" response_time:60.000000 gorouter_time:10.000000 app_time:50.000000 app_id:"FakeApplicationId" `))
				Eventually(r).Should(Say(`app_index:"3"\n`))
			})
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
				record.RoundtripFinishedAt = time.Time{}
				record.AppRequestFinishedAt = time.Time{}
				record.RequestBytesReceived = 0
			})

			It("makes a record", func() {
				r := BufferReader(bytes.NewBufferString(record.LogMessage()))
				Eventually(r).Should(Say(`FakeRequestHost\s-\s\[\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}\.\d{9}Z\]`))
				Eventually(r).Should(Say(`"FakeRequestMethod http://example.com/request FakeRequestProto" `))
				Eventually(r).Should(Say(`"-" 0 0 "-" "-" "FakeRemoteAddr" `))
				Eventually(r).Should(Say(`"-" x_forwarded_for:"-" `))
				Eventually(r).Should(Say(`x_forwarded_proto:"-" `))
				Eventually(r).Should(Say(`vcap_request_id:"-" response_time:"-" gorouter_time:"-" app_time:"-" app_id:"FakeApplicationId" `))
				Eventually(r).Should(Say(`app_index:"-"\n`))
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
				r := BufferReader(bytes.NewBufferString(record.LogMessage()))
				Eventually(r).Should(Say(`FakeRequestHost\s-\s\[\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}\.\d{9}Z\]`))
				Eventually(r).Should(Say(`"FakeRequestMethod http://example.com/request FakeRequestProto" `))
				Eventually(r).Should(Say(`200 30 23 "FakeReferer" "FakeUserAgent" "FakeRemoteAddr" `))
				Eventually(r).Should(Say(`"1.2.3.4:1234" x_forwarded_for:"FakeProxy1, FakeProxy2" `))
				Eventually(r).Should(Say(`x_forwarded_proto:"FakeOriginalRequestProto" `))
				Eventually(r).Should(Say(`vcap_request_id:"abc-123-xyz-pdq" response_time:60.000000 gorouter_time:10.000000 app_time:50.000000 app_id:"FakeApplicationId" `))
				Eventually(r).Should(Say(`app_index:"3" cache_control:"no-cache" accept_encoding:"gzip, deflate" `))
				Eventually(r).Should(Say(`if_match:"737060cd8c284d8af7ad3082f209582d" doesnt_exist:"-"\n`))
			})
		})

		Context("when extra headers is an empty slice", func() {
			It("makes a record with all values", func() {
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
					RoundtripStartedAt:   time.Date(2000, time.January, 1, 0, 0, 0, 0, time.UTC),
					RoundtripFinishedAt:  time.Date(2000, time.January, 1, 0, 1, 0, 0, time.UTC),
					AppRequestStartedAt:  time.Date(2000, time.January, 1, 0, 0, 5, 0, time.UTC),
					AppRequestFinishedAt: time.Date(2000, time.January, 1, 0, 0, 55, 0, time.UTC),
					RequestBytesReceived: 30,
					ExtraHeadersToLog:    []string{},
				}

				r := BufferReader(bytes.NewBufferString(record.LogMessage()))
				Eventually(r).Should(Say(`FakeRequestHost\s-\s\[\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}\.\d{9}Z\]`))
				Eventually(r).Should(Say(`"FakeRequestMethod http://example.com/request FakeRequestProto" `))
				Eventually(r).Should(Say(`200 30 23 "FakeReferer" "FakeUserAgent" "FakeRemoteAddr" `))
				Eventually(r).Should(Say(`"1.2.3.4:1234" x_forwarded_for:"FakeProxy1, FakeProxy2" `))
				Eventually(r).Should(Say(`x_forwarded_proto:"FakeOriginalRequestProto" `))
				Eventually(r).Should(Say(`vcap_request_id:"abc-123-xyz-pdq" response_time:60.000000 gorouter_time:10.000000 app_time:50.000000 app_id:"FakeApplicationId" `))
				Eventually(r).Should(Say(`app_index:"3"\n`))
			})
		})
	})

	Describe("WriteTo", func() {
		It("writes the correct log line to the io.Writer", func() {
			b := new(bytes.Buffer)
			_, err := record.WriteTo(b)
			Expect(err).ToNot(HaveOccurred())

			r := BufferReader(b)
			Eventually(r).Should(Say(`FakeRequestHost\s-\s\[\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}\.\d{9}Z\]`))
			Eventually(r).Should(Say(`"FakeRequestMethod http://example.com/request FakeRequestProto" `))
			Eventually(r).Should(Say(`200 30 23 "FakeReferer" "FakeUserAgent" "FakeRemoteAddr" `))
			Eventually(r).Should(Say(`"1.2.3.4:1234" x_forwarded_for:"FakeProxy1, FakeProxy2" `))
			Eventually(r).Should(Say(`x_forwarded_proto:"FakeOriginalRequestProto" `))
			Eventually(r).Should(Say(`vcap_request_id:"abc-123-xyz-pdq" response_time:60.000000 gorouter_time:10.000000 app_time:50.000000 app_id:"FakeApplicationId" `))
			Eventually(r).Should(Say(`app_index:"3"\n`))
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
