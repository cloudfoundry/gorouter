package handlers_test

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"

	"code.cloudfoundry.org/gorouter/logger"
	"code.cloudfoundry.org/gorouter/route"

	"code.cloudfoundry.org/gorouter/common/uuid"
	"code.cloudfoundry.org/gorouter/handlers"
	"code.cloudfoundry.org/gorouter/test_util"

	"github.com/cloudfoundry/dropsonde/emitter/fake"
	"github.com/cloudfoundry/sonde-go/events"
	gouuid "github.com/nu7hatch/gouuid"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gbytes"
	"github.com/urfave/negroni/v3"
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

		resp http.ResponseWriter
		req  *http.Request

		fakeEmitter *fake.FakeEventEmitter
		logger      logger.Logger

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
		logger = test_util.NewTestZapLogger("test")

		nextHandler = http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
			_, err := io.ReadAll(req.Body)
			Expect(err).NotTo(HaveOccurred())

			rw.WriteHeader(http.StatusTeapot)
			rw.Write([]byte("I'm a little teapot, short and stout."))

			requestInfo, err := handlers.ContextRequestInfo(req)
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

		Expect(envelope.Tags).To(HaveLen(6))
		Expect(envelope.Tags["component"]).To(Equal("some-component"))
		Expect(envelope.Tags["instance_id"]).To(Equal("some-instance-id"))
		Expect(envelope.Tags["process_id"]).To(Equal("some-proc-id"))
		Expect(envelope.Tags["process_instance_id"]).To(Equal("some-proc-instance-id"))
		Expect(envelope.Tags["process_type"]).To(Equal("some-proc-type"))
		Expect(envelope.Tags["source_id"]).To(Equal("some-source-id"))
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

				rw.WriteHeader(http.StatusTeapot)
				rw.Write([]byte("I'm a little teapot, short and stout."))

				nextCalled = true
			})
			nextCalled = false
		})

		It("emits an HTTP StartStop without tags", func() {
			handler.ServeHTTP(resp, req)

			envelope := findEnvelope(fakeEmitter, events.Envelope_HttpStartStop)
			Expect(envelope).ToNot(BeNil())

			Expect(envelope.Tags).To(HaveLen(0))
		})
	})

	Context("when VcapRequestIdHeader is not provided", func() {
		BeforeEach(func() {
			req.Header.Set(handlers.VcapRequestIdHeader, "")
		})

		It("calls error on the logger", func() {
			defer func() {
				recover()
				Expect(logger).To(gbytes.Say(`"data":{"error":"X-Vcap-Request-Id not found"}`))
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
					Expect(logger).To(gbytes.Say(`"data":{"trace-id":"1111","span-id":"2222","error":"X-Vcap-Request-Id not found"}`))
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
					Eventually(logger).Should(gbytes.Say(`"data":{"error":"ProxyResponseWriter not found"}`))
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
			Expect(logger).To(gbytes.Say(`"message":"failed-to-emit-startstop-event"`))

			Expect(nextCalled).To(BeTrue())
		})
	})
})
