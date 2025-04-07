package schema_test

import (
	"bytes"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	. "github.com/onsi/gomega/gbytes"

	"code.cloudfoundry.org/gorouter/accesslog/schema"
	"code.cloudfoundry.org/gorouter/config"
	"code.cloudfoundry.org/gorouter/handlers"
	"code.cloudfoundry.org/gorouter/route"
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
			PrivateInstanceId:    "FakeInstanceId",
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
			RoundTripSuccessful:  true,
			RouteEndpoint:        endpoint,
			ReceivedAt:           time.Date(2000, time.January, 1, 0, 0, 0, 0, time.UTC),
			FinishedAt:           time.Date(2000, time.January, 1, 0, 1, 0, 0, time.UTC),
			AppRequestStartedAt:  time.Date(2000, time.January, 1, 0, 0, 5, 0, time.UTC),
			AppRequestFinishedAt: time.Date(2000, time.January, 1, 0, 0, 55, 0, time.UTC),
			RequestBytesReceived: 30,
			RouterError:          "some-router-error",
			GorouterTime:         10,
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
			Eventually(r).Should(Say(`vcap_request_id:"abc-123-xyz-pdq" response_time:60.000000 gorouter_time:10.000000 app_id:"FakeApplicationId" `))
			Eventually(r).Should(Say(`app_index:"3"`))
			Eventually(r).Should(Say(`instance_id:"FakeInstanceId"`))
			Eventually(r).Should(Say(`x_cf_routererror:"some-router-error"`))
		})

		Context("when the AccessLogRecord is too large for UDP", func() {
			Context("when the URL is too large", func() {
				It("truncates the log", func() {
					record.RedactQueryParams = config.REDACT_QUERY_PARMS_NONE
					qp := strings.Repeat("&a=a", 100_000)
					record.Request.URL, _ = url.Parse(fmt.Sprintf("http://meow.com/long-query-params?a=a%s", qp))
					record.Request.Method = http.MethodGet
					b := record.LogMessage()

					startOfQueryParams := strings.Index(b, `&a=a`)
					Expect(startOfQueryParams).Should(BeNumerically(">", 0))
					endOfQueryParams := strings.Index(b, "...REQUEST-URI-TOO-LONG-TO-LOG--TRUNCATED")
					Expect(endOfQueryParams).Should(BeNumerically(">", 0))
					Expect(endOfQueryParams - startOfQueryParams).Should(BeNumerically("<", 20000))
				})
			})
			Context("when the extra request headers are too large", func() {
				It("truncates the log", func() {
					record.Request.URL, _ = url.Parse("http://meow.com/long-headers")
					record.Request.Method = http.MethodGet
					for i := 0; i < 30000; i++ {
						record.Request.Header.Add(fmt.Sprintf("%d", i), fmt.Sprintf("%d", i))
						record.ExtraHeadersToLog = append(record.ExtraHeadersToLog, fmt.Sprintf("%d", i))
					}
					b := record.LogMessage()

					startOfExtraHeaders := strings.Index(b, `0:"0"`)
					Expect(startOfExtraHeaders).Should(BeNumerically(">", 0))
					endOfExtraHeaders := strings.Index(b, "...EXTRA-REQUEST-HEADERS-TOO-LONG-TO-LOG--TRUNCATED")
					Expect(endOfExtraHeaders).Should(BeNumerically(">", 0))
					Expect(endOfExtraHeaders - startOfExtraHeaders).Should(BeNumerically("<", 20000))
				})
			})

			DescribeTable("when the request headers are too large",
				func(headerToTest string, limit int) {
					record.Request.Header.Set(headerToTest, strings.Repeat(headerToTest, 100_000))
					b := record.LogMessage()
					startOfHeader := strings.Index(b, headerToTest)
					Expect(startOfHeader).Should(BeNumerically(">", 0))
					endOfHeader := strings.Index(b, fmt.Sprintf("...%s-TOO-LONG-TO-LOG--TRUNCATED", strings.ToUpper(headerToTest)))
					Expect(endOfHeader).Should(BeNumerically(">", 0))
					Expect(endOfHeader - startOfHeader).Should(BeNumerically("<", limit))
				},
				Entry("User-Agent", "User-Agent", 1_000),
				Entry("Referer", "Referer", 1_000),
				Entry("X-Forwarded-For", "X-Forwarded-For", 1_000),
				Entry("X-Forwarded-Proto", "X-Forwarded-Proto", 1_000),
			)
		})

		Context("when DisableSourceIPLogging is specified", func() {
			It("does not write RemoteAddr as part of the access log", func() {
				record.DisableSourceIPLogging = true

				r := BufferReader(bytes.NewBufferString(record.LogMessage()))
				Consistently(r).ShouldNot(Say("FakeRemoteAddr"))
			})
		})

		Context("when RedactQueryParams is set to all", func() {
			It("does redact all query parameters on GET requests", func() {
				record.Request.URL.RawQuery = "query=value"
				record.RedactQueryParams = config.REDACT_QUERY_PARMS_ALL
				record.Request.Method = http.MethodGet
				r := BufferReader(bytes.NewBufferString(record.LogMessage()))
				Consistently(r).ShouldNot(Say("query=value"))
			})
			It("does not redact any query parameters on non-GET requests", func() {
				record.Request.URL.RawQuery = "query=value"
				record.RedactQueryParams = config.REDACT_QUERY_PARMS_ALL
				record.Request.Method = http.MethodPost
				r := BufferReader(bytes.NewBufferString(record.LogMessage()))
				Eventually(r).Should(Say("query=value"))
			})
		})

		Context("when RedactQueryParams is set to hash", func() {
			It("does sha1sum all query parameters on GET requests", func() {
				record.Request.URL.RawQuery = "query=value"
				record.RedactQueryParams = config.REDACT_QUERY_PARMS_HASH
				record.Request.Method = http.MethodGet
				r := BufferReader(bytes.NewBufferString(record.LogMessage()))
				Eventually(r).Should(Say("hash=9c9042adbe045596c2299990920eaa18536d66a1")) //echo -n query=value | sha1sum
			})
			It("does not show 'redacted' if there are no query parameters on GET requests", func() {
				record.Request.URL.RawQuery = ""
				record.RedactQueryParams = config.REDACT_QUERY_PARMS_HASH
				record.Request.Method = http.MethodGet
				r := BufferReader(bytes.NewBufferString(record.LogMessage()))
				Consistently(r).ShouldNot(Say("hash="))
			})
			It("does not redact any query parameters on non-GET requests", func() {
				record.Request.URL.RawQuery = "query=value"
				record.RedactQueryParams = config.REDACT_QUERY_PARMS_HASH
				record.Request.Method = http.MethodPost
				r := BufferReader(bytes.NewBufferString(record.LogMessage()))
				Eventually(r).Should(Say("query=value"))
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
				Eventually(r).Should(Say(`vcap_request_id:"abc-123-xyz-pdq" response_time:60.000000 gorouter_time:10.000000 app_id:"FakeApplicationId" `))
				Eventually(r).Should(Say(`app_index:"3"`))
				Eventually(r).Should(Say(`x_cf_routererror:"some-router-error"`))
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
				record.FinishedAt = time.Time{}
				record.AppRequestFinishedAt = time.Time{}
				record.RequestBytesReceived = 0
				record.RouterError = ""
				record.GorouterTime = -1
			})

			It("makes a record", func() {
				r := BufferReader(bytes.NewBufferString(record.LogMessage()))
				Eventually(r).Should(Say(`FakeRequestHost\s-\s\[\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}\.\d{9}Z\]`))
				Eventually(r).Should(Say(`"FakeRequestMethod http://example.com/request FakeRequestProto" `))
				Eventually(r).Should(Say(`"-" 0 0 "-" "-" "FakeRemoteAddr" `))
				Eventually(r).Should(Say(`"-" x_forwarded_for:"-" `))
				Eventually(r).Should(Say(`x_forwarded_proto:"-" `))
				Eventually(r).Should(Say(`vcap_request_id:"-" response_time:"-" gorouter_time:"-" app_id:"FakeApplicationId" `))
				Eventually(r).Should(Say(`app_index:"-"`))
				Eventually(r).Should(Say(`x_cf_routererror:"-"`))
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
				Eventually(r).Should(Say(`vcap_request_id:"abc-123-xyz-pdq" response_time:60.000000 gorouter_time:10.000000 app_id:"FakeApplicationId" `))
				Eventually(r).Should(Say(`app_index:"3" instance_id:"FakeInstanceId" x_cf_routererror:"some-router-error" cache_control:"no-cache" accept_encoding:"gzip, deflate" `))
				Eventually(r).Should(Say(`if_match:"737060cd8c284d8af7ad3082f209582d" doesnt_exist:"-"`))
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
					ReceivedAt:           time.Date(2000, time.January, 1, 0, 0, 0, 0, time.UTC),
					FinishedAt:           time.Date(2000, time.January, 1, 0, 1, 0, 0, time.UTC),
					AppRequestStartedAt:  time.Date(2000, time.January, 1, 0, 0, 5, 0, time.UTC),
					AppRequestFinishedAt: time.Date(2000, time.January, 1, 0, 0, 55, 0, time.UTC),
					RequestBytesReceived: 30,
					ExtraHeadersToLog:    []string{},
					GorouterTime:         10,
				}

				r := BufferReader(bytes.NewBufferString(record.LogMessage()))
				Eventually(r).Should(Say(`FakeRequestHost\s-\s\[\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}\.\d{9}Z\]`))
				Eventually(r).Should(Say(`"FakeRequestMethod http://example.com/request FakeRequestProto" `))
				Eventually(r).Should(Say(`200 30 23 "FakeReferer" "FakeUserAgent" "FakeRemoteAddr" `))
				Eventually(r).Should(Say(`"1.2.3.4:1234" x_forwarded_for:"FakeProxy1, FakeProxy2" `))
				Eventually(r).Should(Say(`x_forwarded_proto:"FakeOriginalRequestProto" `))
				Eventually(r).Should(Say(`vcap_request_id:"abc-123-xyz-pdq" response_time:60.000000 gorouter_time:10.000000 app_id:"FakeApplicationId" `))
				Eventually(r).Should(Say(`app_index:"3"`))
				Eventually(r).Should(Say(`x_cf_routererror:"-"`))
			})
		})

		Context("when extra_fields is set", func() {
			Context("to [local_address]", func() {
				Context("and the local address is empty", func() {
					It("makes a record with the local address set to -", func() {
						record.ExtraFields = []string{"local_address"}

						r := BufferReader(bytes.NewBufferString(record.LogMessage()))
						Eventually(r).Should(Say(`local_address:"-"`))
					})
				})
				Context("and the local address contains an address", func() {
					It("makes a record with the local address set to that address", func() {
						record.ExtraFields = []string{"local_address"}
						record.LocalAddress = "10.0.0.1:34823"

						r := BufferReader(bytes.NewBufferString(record.LogMessage()))
						Eventually(r).Should(Say(`local_address:"10.0.0.1:34823"`))
					})
				})
			})

			// ['failed_attempts', 'failed_attempts_time', 'dns_time', 'dial_time', 'tls_time', 'backend_time']
			Context("to [failed_attempts failed_attempts_time dns_time dial_time tls_time backend_time]", func() {
				It("adds all fields if set to true", func() {
					record.ExtraFields = []string{"failed_attempts", "failed_attempts_time", "dns_time", "dial_time", "tls_time", "backend_time"}
					record.FailedAttempts = 4
					start := time.Now()
					record.ReceivedAt = start.Add(1 * time.Second)
					record.AppRequestStartedAt = start.Add(2 * time.Second)
					record.LastFailedAttemptFinishedAt = start.Add(3 * time.Second)
					record.DnsStartedAt = start.Add(4 * time.Second)
					record.DnsFinishedAt = start.Add(5 * time.Second)
					record.DialStartedAt = start.Add(6 * time.Second)
					record.DialFinishedAt = start.Add(7 * time.Second)
					record.TlsHandshakeStartedAt = start.Add(8 * time.Second)
					record.TlsHandshakeFinishedAt = start.Add(9 * time.Second)
					record.AppRequestFinishedAt = start.Add(10 * time.Second)
					record.FinishedAt = start.Add(11 * time.Second)

					var b bytes.Buffer
					_, err := record.WriteTo(&b)
					Expect(err).ToNot(HaveOccurred())

					r := b.String()

					Expect(r).To(ContainSubstring("failed_attempts:4"))
					Expect(r).To(ContainSubstring("failed_attempts_time:1.0"))
					Expect(r).To(ContainSubstring("dns_time:1.0"))
					Expect(r).To(ContainSubstring("dial_time:1.0"))
					Expect(r).To(ContainSubstring("tls_time:1.0"))
					Expect(r).To(ContainSubstring("backend_time:7.0"))
				})

				It("adds all appropriate empty values if fields are unset", func() {
					record.ExtraFields = []string{"failed_attempts", "failed_attempts_time", "dns_time", "dial_time", "tls_time", "backend_time"}
					record.FailedAttempts = 0

					var b bytes.Buffer
					_, err := record.WriteTo(&b)
					Expect(err).ToNot(HaveOccurred())

					r := b.String()

					Expect(r).To(ContainSubstring(`failed_attempts:0`))
					Expect(r).To(ContainSubstring(`failed_attempts_time:"-"`))
					Expect(r).To(ContainSubstring(`dns_time:"-"`))
					Expect(r).To(ContainSubstring(`dial_time:"-"`))
					Expect(r).To(ContainSubstring(`tls_time:"-"`))
				})

				It("adds a '-' if there was no successful attempt", func() {
					record.ExtraFields = []string{"failed_attempts", "failed_attempts_time", "dns_time", "dial_time", "tls_time", "backend_time"}
					record.FailedAttempts = 1
					record.RoundTripSuccessful = false

					var b bytes.Buffer
					_, err := record.WriteTo(&b)
					Expect(err).ToNot(HaveOccurred())

					r := b.String()

					Expect(r).To(ContainSubstring(`backend_time:"-"`))
				})
			})

			Context("to [foobarbazz]", func() {
				It("ignores it", func() {
					record.ExtraFields = []string{"foobarbazz"}
					record.LocalAddress = "10.0.0.1:34823"

					r := BufferReader(bytes.NewBufferString(record.LogMessage()))
					Consistently(r).ShouldNot(Say("foobarbazz"))
				})
				It("does not log local_address", func() {
					record.ExtraFields = []string{"foobarbazz"}
					record.LocalAddress = "10.0.0.1:34823"

					r := BufferReader(bytes.NewBufferString(record.LogMessage()))
					Consistently(r).ShouldNot(Say(`local_address:"10.0.0.1:34823"`))
				})
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
			Eventually(r).Should(Say(`vcap_request_id:"abc-123-xyz-pdq" response_time:60.000000 gorouter_time:10.000000 app_id:"FakeApplicationId" `))
			Eventually(r).Should(Say(`app_index:"3"`))
			Eventually(r).Should(Say(`x_cf_routererror:"some-router-error"`))
		})

		Context("when the AccessLogRecord is too large for UDP", func() {
			Context("when the URL is too large", func() {
				It("does not truncate the log", func() {
					record.RedactQueryParams = config.REDACT_QUERY_PARMS_NONE
					qp := strings.Repeat("&a=a", 10_000)
					record.Request.URL, _ = url.Parse(fmt.Sprintf("http://meow.com/long-query-params?a=a%s&b=b", qp))
					record.Request.Method = http.MethodGet

					b := new(bytes.Buffer)
					_, err := record.WriteTo(b)
					Expect(err).ToNot(HaveOccurred())

					r := BufferReader(b)
					Eventually(r).Should(Say("b=b"))
					Consistently(r).ShouldNot(Say("...REQUEST-URI-TOO-LONG-TO-LOG--TRUNCATED"))
				})
			})
			Context("when the extra request headers are too large", func() {
				It("does not truncate the log", func() {
					record.Request.URL, _ = url.Parse("http://meow.com/long-headers")
					record.Request.Method = http.MethodGet
					for i := 0; i < 2000; i++ {
						record.Request.Header.Add(fmt.Sprintf("%d", i), fmt.Sprintf("%d", i))
						record.ExtraHeadersToLog = append(record.ExtraHeadersToLog, fmt.Sprintf("%d", i))
					}
					b := new(bytes.Buffer)
					_, err := record.WriteTo(b)
					Expect(err).ToNot(HaveOccurred())

					r := BufferReader(b)
					Eventually(r).Should(Say(`1999:"1999"`))
					Consistently(r).ShouldNot(Say("...EXTRA-REQUEST-HEADERS-TOO-LONG-TO-LOG--TRUNCATED"))
				})
			})

			DescribeTable("does not truncate when the request headers are too large",
				func(headerToTest string, limit int) {
					record.Request.Header.Set(headerToTest, strings.Repeat(headerToTest, 1_000)+"LastEntry")
					b := new(bytes.Buffer)
					_, err := record.WriteTo(b)
					Expect(err).ToNot(HaveOccurred())

					r := BufferReader(b)
					Eventually(r).Should(Say("LastEntry"))
					Consistently(r).ShouldNot(Say(fmt.Sprintf("...%s-TOO-LONG-TO-LOG--TRUNCATED", headerToTest)))
				},
				Entry("User-Agent", "User-Agent", 1_000),
				Entry("Referer", "Referer", 1_000),
				Entry("X-Forwarded-For", "X-Forwarded-For", 1_000),
				Entry("X-Forwarded-Proto", "X-Forwarded-Proto", 1_000),
			)
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
