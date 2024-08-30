package handlers_test

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"

	"code.cloudfoundry.org/gorouter/common/uuid"
	"code.cloudfoundry.org/gorouter/handlers"
	log "code.cloudfoundry.org/gorouter/logger"
	"code.cloudfoundry.org/gorouter/route"
	"code.cloudfoundry.org/gorouter/test_util"
	"github.com/cloudfoundry/dropsonde/emitter/fake"
	"github.com/cloudfoundry/sonde-go/events"
	gouuid "github.com/nu7hatch/gouuid"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gbytes"
	"github.com/urfave/negroni/v3"
	"go.uber.org/zap/zapcore"
)

func findEnvelope(fakeEmitter *fake.FakeEventEmitter, eventType events.Envelope_EventType) *events.Envelope {
	for _, envelope := range fakeEmitter.GetEnvelopes() {
		if *envelope.EventType == eventType {
			return envelope
		}
	}
	return nil
}

func convertUUID(uuid *events.UUID) gouuid.UUID {
	var reqUUID gouuid.UUID
	binary.LittleEndian.PutUint64(reqUUID[:8], uuid.GetLow())
	binary.LittleEndian.PutUint64(reqUUID[8:], uuid.GetHigh())

	return reqUUID
}

var _ = Describe("HTTPStartStop Handler", func() {
	var (
		vcapHeader  string
		handler     *negroni.Negroni
		nextHandler http.HandlerFunc
		prevHandler negroni.Handler
		requestInfo *handlers.RequestInfo

		resp http.ResponseWriter
		req  *http.Request

		fakeEmitter *fake.FakeEventEmitter
		testSink    *test_util.TestSink
		logger      *slog.Logger

		nextCalled bool
	)

	BeforeEach(func() {
		body := bytes.NewBufferString("What are you?")
		req = test_util.NewRequest("GET", "example.com", "/", body)
		resp = httptest.NewRecorder()

		var err error
		vcapHeader, err = uuid.GenerateUUID()
		Expect(err).ToNot(HaveOccurred())
		req.Header.Set(handlers.VcapRequestIdHeader, vcapHeader)

		fakeEmitter = fake.NewFakeEventEmitter("fake")
		testSink = &test_util.TestSink{Buffer: gbytes.NewBuffer()}
		logger = log.CreateLogger()
		testSink = &test_util.TestSink{Buffer: gbytes.NewBuffer()}
		log.SetDynamicWriteSyncer(zapcore.NewMultiWriteSyncer(testSink, zapcore.AddSync(GinkgoWriter)))
		log.SetLoggingLevel("Debug")

		nextHandler = http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
			_, err := io.ReadAll(req.Body)
			Expect(err).NotTo(HaveOccurred())

			rw.WriteHeader(http.StatusTeapot)
			rw.Write([]byte("I'm a little teapot, short and stout."))

			requestInfo, err = handlers.ContextRequestInfo(req)
			Expect(err).ToNot(HaveOccurred())
			appID := "11111111-1111-1111-1111-111111111111"
			requestInfo.RouteEndpoint = route.NewEndpoint(&route.EndpointOpts{
				AppId: appID,
				Tags: map[string]string{
					"component":           "some-component",
					"instance_id":         "some-instance-id",
					"process_id":          "some-proc-id",
					"process_instance_id": "some-proc-instance-id",
					"process_type":        "some-proc-type",
					"source_id":           "some-source-id",
				},
			})
			requestInfo.TraceInfo = handlers.TraceInfo{
				SpanID:  "12345678",
				TraceID: "1234567890123456",
			}

			nextCalled = true
		})
		nextCalled = false

		prevHandler = &PrevHandler{}
	})

	JustBeforeEach(func() {
		handler = negroni.New()
		handler.Use(handlers.NewRequestInfo())
		handler.Use(prevHandler)
		handler.Use(handlers.NewProxyWriter(logger))
		handler.Use(handlers.NewHTTPStartStop(fakeEmitter, logger))
		handler.UseHandlerFunc(nextHandler)
	})

	It("emits an HTTP StartStop event", func() {
		handler.ServeHTTP(resp, req)

		envelope := findEnvelope(fakeEmitter, events.Envelope_HttpStartStop)
		Expect(envelope).ToNot(BeNil())

		startStopEvent := envelope.HttpStartStop
		Expect(startStopEvent).ToNot(BeNil())

		reqUUID := convertUUID(startStopEvent.GetRequestId())
		Expect(reqUUID.String()).To(Equal(vcapHeader))
		Expect(startStopEvent.GetMethod().String()).To(Equal("GET"))
		Expect(startStopEvent.GetStatusCode()).To(Equal(int32(http.StatusTeapot)))
		Expect(startStopEvent.GetContentLength()).To(Equal(int64(37)))

		Expect(nextCalled).To(BeTrue(), "Expected the next handler to be called.")
	})

	It("emits an HTTP StartStop with tags", func() {
		handler.ServeHTTP(resp, req)

		envelope := findEnvelope(fakeEmitter, events.Envelope_HttpStartStop)
		Expect(envelope).ToNot(BeNil())

		Expect(envelope.Tags).To(HaveLen(8))
		Expect(envelope.Tags["component"]).To(Equal("some-component"))
		Expect(envelope.Tags["instance_id"]).To(Equal("some-instance-id"))
		Expect(envelope.Tags["process_id"]).To(Equal("some-proc-id"))
		Expect(envelope.Tags["process_instance_id"]).To(Equal("some-proc-instance-id"))
		Expect(envelope.Tags["process_type"]).To(Equal("some-proc-type"))
		Expect(envelope.Tags["source_id"]).To(Equal("some-source-id"))
		Expect(envelope.Tags["trace_id"]).To(Equal("1234567890123456"))
		Expect(envelope.Tags["span_id"]).To(Equal("12345678"))
	})

	It("does not modify the endpoint tags", func() {
		handler.ServeHTTP(resp, req)

		Expect(requestInfo.RouteEndpoint.Tags).ToNot(HaveKey("trace_id"))
		Expect(requestInfo.RouteEndpoint.Tags).ToNot(HaveKey("span_id"))
	})

	Context("when x-cf-instanceindex is present", func() {
		It("does not use the value from the header", func() {
			req.Header.Set("X-CF-InstanceIndex", "99")
			var emptyInt *int32
			handler.ServeHTTP(resp, req)

			envelope := findEnvelope(fakeEmitter, events.Envelope_HttpStartStop)
			Expect(envelope).ToNot(BeNil())
			Expect(envelope.HttpStartStop.InstanceIndex).To(Equal(emptyInt))
		})
	})

	Context("when x-cf-instanceid is present", func() {
		It("does not use the value from the header", func() {
			req.Header.Set("X-CF-InstanceID", "fakeID")
			var emptyString *string
			handler.ServeHTTP(resp, req)

			envelope := findEnvelope(fakeEmitter, events.Envelope_HttpStartStop)
			Expect(envelope).ToNot(BeNil())
			Expect(envelope.HttpStartStop.InstanceId).To(Equal(emptyString))
		})
	})

	Context("when x-cf-applicationID is present", func() {
		It("does not use value from header", func() {
			req.Header.Set("X-Cf-ApplicationID", "11111111-1111-1111-1111-111111111112")

			var emptyUUID *events.UUID

			handler.ServeHTTP(resp, req)

			envelope := findEnvelope(fakeEmitter, events.Envelope_HttpStartStop)
			Expect(envelope).ToNot(BeNil())
			Expect(envelope.HttpStartStop.ApplicationId).To(Equal(emptyUUID))
		})
	})

	Context("when there is no RouteEndpoint", func() {
		BeforeEach(func() {
			nextHandler = http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
				_, err := io.ReadAll(req.Body)
				Expect(err).NotTo(HaveOccurred())

				requestInfo, err = handlers.ContextRequestInfo(req)
				Expect(err).ToNot(HaveOccurred())

				requestInfo.TraceInfo = handlers.TraceInfo{
					SpanID:  "12345678",
					TraceID: "1234567890123456",
				}

				rw.WriteHeader(http.StatusTeapot)
				rw.Write([]byte("I'm a little teapot, short and stout."))

				nextCalled = true
			})
			nextCalled = false
		})

		It("emits an HTTP StartStop without RouteEndpoint tags", func() {
			handler.ServeHTTP(resp, req)

			envelope := findEnvelope(fakeEmitter, events.Envelope_HttpStartStop)
			Expect(envelope).ToNot(BeNil())

			Expect(envelope.Tags).To(Equal(map[string]string{"span_id": "12345678", "trace_id": "1234567890123456"}))
		})
	})

	Context("when ContextRequestInfo returns an error", func() {
		It("calls Error on the logger, but does not fail the request", func() {
			handler = negroni.New()
			handler.Use(prevHandler)
			handler.Use(handlers.NewRequestInfo())
			handler.Use(handlers.NewProxyWriter(logger))
			handler.Use(&removeRequestInfoHandler{})
			handler.Use(handlers.NewHTTPStartStop(fakeEmitter, logger))
			handler.Use(handlers.NewRequestInfo())
			handler.UseHandlerFunc(nextHandler)
			handler.ServeHTTP(resp, req)

			Expect(string(testSink.Contents())).To(ContainSubstring(`"message":"request-info-err"`))

			Expect(nextCalled).To(BeTrue())
		})
	})

	Context("when VcapRequestIdHeader is not provided", func() {
		BeforeEach(func() {
			req.Header.Set(handlers.VcapRequestIdHeader, "")
		})

		It("calls error on the logger", func() {
			defer func() {
				recover()
				Expect(string(testSink.Contents())).To(ContainSubstring(`"data":{"error":"X-Vcap-Request-Id not found"}`))
				Expect(nextCalled).To(BeFalse())
			}()

			handler.ServeHTTP(resp, req)
		})

		Context("when request context has trace info", func() {
			BeforeEach(func() {
				prevHandler = &PrevHandlerWithTrace{}
			})

			It("logs message with trace info", func() {
				defer func() {
					recover()
					Expect(string(testSink.Contents())).To(ContainSubstring(`"data":{"trace-id":"1111","span-id":"2222","error":"X-Vcap-Request-Id not found"}`))
					Expect(nextCalled).To(BeFalse())
				}()

				handler.ServeHTTP(resp, req)
			})
		})
	})

	Context("when VcapRequestIdHeader is provided", func() {
		BeforeEach(func() {
			req.Header.Set(handlers.VcapRequestIdHeader, "11111111-1111-1111-1111-111111111111")
		})

		Context("when the response writer is not a proxy response writer", func() {
			var badHandler *negroni.Negroni
			BeforeEach(func() {
				badHandler = negroni.New()
				badHandler.Use(handlers.NewHTTPStartStop(fakeEmitter, logger))
			})

			It("calls error on the logger with request trace id", func() {
				defer func() {
					recover()
					Eventually(string(testSink.Contents())).Should(ContainSubstring(`"data":{"error":"ProxyResponseWriter not found"}`))
					Expect(nextCalled).To(BeFalse())
				}()
				badHandler.ServeHTTP(resp, req)
			})
		})
	})

	Context("when the emitter fails to emit", func() {
		BeforeEach(func() {
			fakeEmitter.ReturnError = errors.New("foo-error")
		})
		It("calls Info on the logger, but does not fail the request", func() {
			handler.ServeHTTP(resp, req)
			Expect(string(testSink.Contents())).To(ContainSubstring(`"message":"failed-to-emit-startstop-event"`))

			Expect(nextCalled).To(BeTrue())
		})
	})

	Context("when TraceInfo is unpopulated", func() {
		BeforeEach(func() {
			nextHandler = http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
				_, err := io.ReadAll(req.Body)
				Expect(err).NotTo(HaveOccurred())

				rw.WriteHeader(http.StatusTeapot)
				rw.Write([]byte("I'm a little teapot, short and stout."))

				requestInfo, err = handlers.ContextRequestInfo(req)
				Expect(err).ToNot(HaveOccurred())
				appID := "11111111-1111-1111-1111-111111111111"
				requestInfo.RouteEndpoint = route.NewEndpoint(&route.EndpointOpts{
					AppId: appID,
					Tags: map[string]string{
						"component":           "some-component",
						"instance_id":         "some-instance-id",
						"process_id":          "some-proc-id",
						"process_instance_id": "some-proc-instance-id",
						"process_type":        "some-proc-type",
						"source_id":           "some-source-id",
					},
				})

				nextCalled = true
			})
		})

		It("emits an HTTP StartStop without span_id and trace_id tags", func() {
			handler.ServeHTTP(resp, req)

			envelope := findEnvelope(fakeEmitter, events.Envelope_HttpStartStop)
			Expect(envelope).ToNot(BeNil())

			Expect(envelope.Tags).ToNot(HaveKey("span_id"))
			Expect(envelope.Tags).ToNot(HaveKey("trace_id"))
		})
	})
})

type removeRequestInfoHandler struct{}

func (p *removeRequestInfoHandler) ServeHTTP(rw http.ResponseWriter, r *http.Request, next http.HandlerFunc) {
	r = r.WithContext(context.Background())
	next(rw, r)
}
