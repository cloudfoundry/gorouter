package handlers_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"time"

	"code.cloudfoundry.org/gorouter/access_log/schema"
	"code.cloudfoundry.org/gorouter/handlers"
	"code.cloudfoundry.org/gorouter/metrics/fakes"
	fakeRegistry "code.cloudfoundry.org/gorouter/registry/fakes"
	"code.cloudfoundry.org/gorouter/route"
	"code.cloudfoundry.org/gorouter/test_util"

	"code.cloudfoundry.org/gorouter/logger"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/urfave/negroni"
)

var _ = Describe("Lookup", func() {
	var (
		handler     negroni.Handler
		nextHandler http.HandlerFunc
		alr         *schema.AccessLogRecord
		logger      logger.Logger
		reg         *fakeRegistry.FakeRegistry
		rep         *fakes.FakeCombinedReporter
		resp        *httptest.ResponseRecorder
		req         *http.Request
		nextCalled  bool
		nextRequest *http.Request
	)

	nextHandler = http.HandlerFunc(func(_ http.ResponseWriter, req *http.Request) {
		nextCalled = true
		nextRequest = req
	})

	BeforeEach(func() {
		nextCalled = false
		nextRequest = &http.Request{}
		logger = test_util.NewTestZapLogger("lookup_handler")
		rep = &fakes.FakeCombinedReporter{}
		reg = &fakeRegistry.FakeRegistry{}
		handler = handlers.NewLookup(reg, rep, logger)

		req = test_util.NewRequest("GET", "example.com", "/", nil)
		resp = httptest.NewRecorder()
		alr = &schema.AccessLogRecord{
			Request: req,
		}
		req = req.WithContext(context.WithValue(req.Context(), "AccessLogRecord", alr))
	})

	Context("when there are no endpoints", func() {
		BeforeEach(func() {
			handler.ServeHTTP(resp, req, nextHandler)
		})

		It("sends a bad request metric", func() {
			Expect(rep.CaptureBadRequestCallCount()).To(Equal(1))
		})

		It("Sets X-Cf-RouterError to unknown_route", func() {
			Expect(resp.Header().Get("X-Cf-RouterError")).To(Equal("unknown_route"))
		})

		It("returns a 404 NotFound and does not call next", func() {
			Expect(nextCalled).To(BeFalse())
			Expect(resp.Code).To(Equal(http.StatusNotFound))
		})

		It("has a meaningful response", func() {
			Expect(resp.Body.String()).To(ContainSubstring("Requested route ('example.com') does not exist"))
		})

		It("puts a 404 NotFound in the accessLog", func() {
			Expect(alr.StatusCode).To(Equal(http.StatusNotFound))
		})
	})

	Context("when there are endpoints", func() {
		var pool *route.Pool

		BeforeEach(func() {
			pool = route.NewPool(2*time.Minute, "example.com")
			reg.LookupReturns(pool)
		})

		JustBeforeEach(func() {
			handler.ServeHTTP(resp, req, nextHandler)
		})

		It("calls next with the pool", func() {
			Expect(nextCalled).To(BeTrue())
			Expect(nextRequest.Context().Value("RoutePool")).To(Equal(pool))
		})

		Context("when a specific instance is requested", func() {
			BeforeEach(func() {
				req.Header.Add("X-CF-App-Instance", "app-guid:instance-id")

				reg.LookupWithInstanceReturns(pool)
			})

			It("lookups with instance", func() {
				Expect(reg.LookupWithInstanceCallCount()).To(Equal(1))
				uri, appGuid, appIndex := reg.LookupWithInstanceArgsForCall(0)

				Expect(uri.String()).To(Equal("example.com"))
				Expect(appGuid).To(Equal("app-guid"))
				Expect(appIndex).To(Equal("instance-id"))
			})
		})

		Context("when an invalid instance header is requested", func() {
			BeforeEach(func() {
				req.Header.Add("X-CF-App-Instance", "app-guid:instance-id:invalid-part")

				reg.LookupWithInstanceReturns(pool)
			})

			It("does not lookup the instance", func() {
				Expect(reg.LookupWithInstanceCallCount()).To(Equal(0))
			})

			It("responds with 404", func() {
				Expect(nextCalled).To(BeFalse())
				Expect(resp.Code).To(Equal(http.StatusNotFound))
			})
		})
	})
})
