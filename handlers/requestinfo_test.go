package handlers_test

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/urfave/negroni/v3"

	"code.cloudfoundry.org/gorouter/handlers"
	"code.cloudfoundry.org/gorouter/route"
	"code.cloudfoundry.org/gorouter/test_util"
)

var _ = Describe("RequestInfoHandler", func() {
	var (
		handler negroni.Handler

		resp http.ResponseWriter
		req  *http.Request

		nextCalled bool

		reqChan chan *http.Request
	)

	nextHandler := http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		_, err := io.ReadAll(req.Body)
		Expect(err).NotTo(HaveOccurred())

		rw.WriteHeader(http.StatusTeapot)
		rw.Write([]byte("I'm a little teapot, short and stout."))

		reqChan <- req
		nextCalled = true
	})

	BeforeEach(func() {
		body := bytes.NewBufferString("What are you?")
		req = test_util.NewRequest("GET", "example.com", "/", body)
		resp = httptest.NewRecorder()

		handler = handlers.NewRequestInfo()

		reqChan = make(chan *http.Request, 1)

		nextCalled = false
	})

	AfterEach(func() {
		Expect(nextCalled).To(BeTrue(), "Expected the next handler to be called.")
		close(reqChan)
	})

	It("sets RequestInfo with StartTime on the context", func() {
		handler.ServeHTTP(resp, req, nextHandler)
		var contextReq *http.Request
		Eventually(reqChan).Should(Receive(&contextReq))

		expectedStartTime := time.Now()

		ri, err := handlers.ContextRequestInfo(contextReq)
		Expect(err).ToNot(HaveOccurred())
		Expect(ri).ToNot(BeNil())
		Expect(ri.ReceivedAt).To(BeTemporally("~", expectedStartTime, 10*time.Millisecond))

	})
})

var _ = Describe("GetEndpoint", func() {
	var (
		ctx              context.Context
		requestInfo      *handlers.RequestInfo
		expectedEndpoint *route.Endpoint
	)

	BeforeEach(func() {
		// some hackery to set data on requestInfo using only exported symbols
		req, _ := http.NewRequest("banana", "", nil)
		rih := &handlers.RequestInfoHandler{}
		rih.ServeHTTP(nil, req, func(w http.ResponseWriter, r *http.Request) {
			ctx = r.Context()
			requestInfo, _ = handlers.ContextRequestInfo(r)
		})
		expectedEndpoint = &route.Endpoint{PrivateInstanceId: "some-id"}

		requestInfo.RouteEndpoint = expectedEndpoint
	})

	It("returns the endpoint private instance id", func() {
		endpoint, err := handlers.GetEndpoint(ctx)
		Expect(endpoint).To(Equal(expectedEndpoint))
		Expect(err).NotTo(HaveOccurred())
	})

	Context("when the context is missing the key", func() {
		BeforeEach(func() {
			ctx = context.Background()
		})

		It("returns a friendly error", func() {
			_, err := handlers.GetEndpoint(ctx)
			Expect(err).To(MatchError("RequestInfo not set on context"))
		})
	})

	Context("when the route endpoint is not set", func() {
		BeforeEach(func() {
			requestInfo.RouteEndpoint = nil
		})
		It("returns a friendly error", func() {
			_, err := handlers.GetEndpoint(ctx)
			Expect(err).To(MatchError("route endpoint not set on request info"))
		})

	})
})

var _ = Describe("RequestInfo", func() {
	var requestInfo *handlers.RequestInfo

	BeforeEach(func() {
		requestInfo = &handlers.RequestInfo{}
	})

	Describe("ProvideTraceInfo", func() {
		Context("when TraceInfo is set", func() {
			BeforeEach(func() {
				requestInfo.TraceInfo = handlers.TraceInfo{
					TraceID: "11111111111111111111111111111111",
					SpanID:  "2222222222222222",
					UUID:    "11111111-1111-1111-1111-111111111111",
				}
			})

			It("returns TraceInfo", func() {
				traceInfo, err := requestInfo.ProvideTraceInfo()
				Expect(err).NotTo(HaveOccurred())
				Expect(traceInfo).To(Equal(handlers.TraceInfo{
					TraceID: "11111111111111111111111111111111",
					SpanID:  "2222222222222222",
					UUID:    "11111111-1111-1111-1111-111111111111",
				}))
			})
		})

		Context("when TraceInfo is not set", func() {
			It("generates TraceInfo", func() {
				traceInfo, err := requestInfo.ProvideTraceInfo()
				Expect(err).NotTo(HaveOccurred())
				Expect(traceInfo.TraceID).To(MatchRegexp(b3IDRegex))
				Expect(traceInfo.SpanID).To(MatchRegexp(b3SpanRegex))
				Expect(traceInfo.UUID).To(MatchRegexp(UUIDRegex))

				uuidWithoutDashes := strings.Replace(traceInfo.UUID, "-", "", -1)
				Expect(uuidWithoutDashes).To(Equal(traceInfo.TraceID))
			})
		})
	})

	Describe("SetTraceInfo", func() {
		Context("when traceID that can be converted to UUID and spanID are provided", func() {
			It("sets UUID from traceID", func() {
				requestInfo.SetTraceInfo("11111111111111111111111111111111", "2222222222222222")
				Expect(requestInfo.TraceInfo).To(Equal(handlers.TraceInfo{
					TraceID: "11111111111111111111111111111111",
					SpanID:  "2222222222222222",
					UUID:    "11111111-1111-1111-1111-111111111111",
				}))
			})
		})

		Context("when traceID that can not be converted to UUID provided", func() {
			It("generates new UUID, reuses provided trace and span ID", func() {
				requestInfo.SetTraceInfo("111111111111d1111111111111111111", "2222222222222222")
				Expect(requestInfo.TraceInfo.TraceID).To(Equal("111111111111d1111111111111111111"))
				Expect(requestInfo.TraceInfo.SpanID).To(Equal("2222222222222222"))
				Expect(requestInfo.TraceInfo.UUID).ToNot(Equal("11111111-1111-d111-1111-111111111111"))
				Expect(requestInfo.TraceInfo.UUID).To(MatchRegexp(UUIDRegex))
			})
		})

		Context("when traceID is not UUID length", func() {
			It("generates new UUID, reuses provided trace and span ID", func() {
				err := requestInfo.SetTraceInfo("12345", "2222222222222222")
				Expect(err).NotTo(HaveOccurred())
				Expect(requestInfo.TraceInfo.TraceID).To(Equal("12345"))
				Expect(requestInfo.TraceInfo.SpanID).To(Equal("2222222222222222"))
				Expect(requestInfo.TraceInfo.UUID).To(MatchRegexp(UUIDRegex))
			})
		})
	})

	Describe("LoggerWithTraceInfo", func() {
		var logger *test_util.TestLogger

		BeforeEach(func() {
			logger = test_util.NewTestLogger("request-info")
		})

		Context("when request has trace context", func() {
			BeforeEach(func() {
				req, err := http.NewRequest("GET", "http://example.com", nil)
				Expect(err).NotTo(HaveOccurred())
				ri := new(handlers.RequestInfo)
				ri.TraceInfo.TraceID = "abc"
				ri.TraceInfo.SpanID = "def"
				req = req.WithContext(context.WithValue(req.Context(), handlers.RequestInfoCtxKey, ri))

				logger.Logger = handlers.LoggerWithTraceInfo(logger.Logger, req)
				logger.Info("some-action")
			})

			It("returns a logger that adds trace and spand ids to every log line", func() {
				Expect(logger.TestSink.Lines()).To(HaveLen(1))
				Expect(logger.TestSink.Lines()[0]).To(MatchRegexp(`{.*"data":{"trace-id":"abc","span-id":"def"}}`))
			})
		})

		Context("when request doesn't have vcap request id", func() {
			BeforeEach(func() {
				req, err := http.NewRequest("GET", "http://example.com", nil)
				Expect(err).NotTo(HaveOccurred())
				logger.Logger = handlers.LoggerWithTraceInfo(logger.Logger, req)
				logger.Info("some-action")
			})

			It("returns a logger that doesn't add trace and span ids to log lines", func() {
				Expect(logger.TestSink.Lines()).To(HaveLen(1))
				Expect(logger.TestSink.Lines()[0]).NotTo(MatchRegexp(`trace-id`))
			})
		})
	})
})
