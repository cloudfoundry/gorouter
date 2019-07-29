package handlers_test

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io/ioutil"
	"net/http"
	"net/http/httptest"

	"code.cloudfoundry.org/gorouter/route"

	"code.cloudfoundry.org/gorouter/common/uuid"
	"code.cloudfoundry.org/gorouter/handlers"
	logger_fakes "code.cloudfoundry.org/gorouter/logger/fakes"
	"code.cloudfoundry.org/gorouter/test_util"

	"github.com/cloudfoundry/dropsonde/emitter/fake"
	"github.com/cloudfoundry/sonde-go/events"
	gouuid "github.com/nu7hatch/gouuid"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/urfave/negroni"
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

		resp http.ResponseWriter
		req  *http.Request

		fakeEmitter *fake.FakeEventEmitter
		fakeLogger  *logger_fakes.FakeLogger

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
		fakeLogger = new(logger_fakes.FakeLogger)

		nextHandler = http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
			_, err := ioutil.ReadAll(req.Body)
			Expect(err).NotTo(HaveOccurred())

			rw.WriteHeader(http.StatusTeapot)
			rw.Write([]byte("I'm a little teapot, short and stout."))

			requestInfo, err := handlers.ContextRequestInfo(req)
			Expect(err).ToNot(HaveOccurred())
			requestInfo.RouteEndpoint = route.NewEndpoint(&route.EndpointOpts{
				AppId: "appID",
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
	})

	JustBeforeEach(func() {
		handler = negroni.New()
		handler.Use(handlers.NewRequestInfo())
		handler.Use(handlers.NewProxyWriter(fakeLogger))
		handler.Use(handlers.NewHTTPStartStop(fakeEmitter, fakeLogger))
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

	Context("when there is no RouteEndpoint", func() {
		BeforeEach(func() {
			nextHandler = http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
				_, err := ioutil.ReadAll(req.Body)
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

	Context("when the response writer is not a proxy response writer", func() {
		var badHandler *negroni.Negroni
		BeforeEach(func() {
			badHandler = negroni.New()
			badHandler.Use(handlers.NewHTTPStartStop(fakeEmitter, fakeLogger))
		})
		It("calls Fatal on the logger", func() {
			badHandler.ServeHTTP(resp, req)
			Expect(fakeLogger.FatalCallCount()).To(Equal(1))

			Expect(nextCalled).To(BeFalse())
		})
	})

	Context("when VcapRequestIdHeader is not provided", func() {
		BeforeEach(func() {
			req.Header.Set(handlers.VcapRequestIdHeader, "")
		})
		It("calls Fatal on the logger", func() {
			handler.ServeHTTP(resp, req)
			Expect(fakeLogger.FatalCallCount()).To(Equal(1))

			Expect(nextCalled).To(BeFalse())
		})
	})

	Context("when the emitter fails to emit", func() {
		BeforeEach(func() {
			fakeEmitter.ReturnError = errors.New("foo-error")
		})
		It("calls Info on the logger, but does not fail the request", func() {
			handler.ServeHTTP(resp, req)
			Expect(fakeLogger.InfoCallCount()).To(Equal(1))

			Expect(nextCalled).To(BeTrue())
		})
	})
})
