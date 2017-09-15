package handlers_test

import (
	"net/http"
	"net/http/httptest"
	"time"

	"code.cloudfoundry.org/gorouter/handlers"
	logger_fakes "code.cloudfoundry.org/gorouter/logger/fakes"
	"code.cloudfoundry.org/gorouter/metrics/fakes"
	fakeRegistry "code.cloudfoundry.org/gorouter/registry/fakes"
	"code.cloudfoundry.org/gorouter/route"
	"code.cloudfoundry.org/gorouter/test_util"
	"code.cloudfoundry.org/routing-api/models"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/urfave/negroni"
)

var _ = Describe("Lookup", func() {
	var (
		handler        *negroni.Negroni
		nextHandler    http.HandlerFunc
		logger         *logger_fakes.FakeLogger
		reg            *fakeRegistry.FakeRegistry
		rep            *fakes.FakeCombinedReporter
		resp           *httptest.ResponseRecorder
		req            *http.Request
		nextCalled     bool
		nextRequest    *http.Request
		maxConnections int64
		modTag         models.ModificationTag
	)

	nextHandler = http.HandlerFunc(func(_ http.ResponseWriter, req *http.Request) {
		nextCalled = true
		nextRequest = req
	})

	BeforeEach(func() {
		nextCalled = false
		nextRequest = &http.Request{}
		modTag = models.ModificationTag{}
		maxConnections = 2
		logger = new(logger_fakes.FakeLogger)
		rep = &fakes.FakeCombinedReporter{}
		reg = &fakeRegistry.FakeRegistry{}
		handler = negroni.New()
		req = test_util.NewRequest("GET", "example.com", "/", nil)
		resp = httptest.NewRecorder()
	})

	JustBeforeEach(func() {
		handler.Use(handlers.NewRequestInfo())
		handler.Use(handlers.NewLookup(reg, rep, logger, maxConnections))
		handler.UseHandler(nextHandler)
		handler.ServeHTTP(resp, req)
	})

	Context("when there are no endpoints", func() {
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
	})

	Context("when there are endpoints", func() {
		var pool *route.Pool

		BeforeEach(func() {
			pool = route.NewPool(2*time.Minute, "example.com", "/")
			reg.LookupReturns(pool)
		})

		Context("when conn limit is set to zero (unlimited)", func() {
			BeforeEach(func() {
				maxConnections = 0
				pool = route.NewPool(2*time.Minute, "example.com", "/")
				testEndpoint := route.NewEndpoint("testid1", "1.3.5.6", 5679, "", "", nil, -1, "", modTag, "", false)
				for i := 0; i < 5; i++ {
					testEndpoint.Stats.NumberConnections.Increment()
				}
				pool.Put(testEndpoint)
				testEndpoint1 := route.NewEndpoint("testid2", "1.2.3.6", 5679, "", "", nil, -1, "", modTag, "", false)
				pool.Put(testEndpoint1)
				reg.LookupReturns(pool)
			})

			It("all backends are in the pool", func() {
				Expect(nextCalled).To(BeTrue())
				requestInfo, err := handlers.ContextRequestInfo(nextRequest)
				Expect(err).ToNot(HaveOccurred())
				Expect(requestInfo.RoutePool.IsEmpty()).To(BeFalse())
				len := 0
				requestInfo.RoutePool.Each(func(endpoint *route.Endpoint) {
					len++
				})
				Expect(len).To(Equal(2))
				Expect(resp.Code).To(Equal(http.StatusOK))
			})
		})

		Context("when conn limit is reached for an endpoint", func() {
			BeforeEach(func() {
				pool = route.NewPool(2*time.Minute, "example.com", "/")
				testEndpoint := route.NewEndpoint("testid1", "1.3.5.6", 5679, "", "", nil, -1, "", modTag, "", false)
				testEndpoint.Stats.NumberConnections.Increment()
				testEndpoint.Stats.NumberConnections.Increment()
				testEndpoint.Stats.NumberConnections.Increment()
				pool.Put(testEndpoint)
				testEndpoint1 := route.NewEndpoint("testid2", "1.4.6.7", 5679, "", "", nil, -1, "", modTag, "", false)
				pool.Put(testEndpoint1)
				reg.LookupReturns(pool)
			})

			It("does not include the overloaded backend in the pool", func() {
				Expect(nextCalled).To(BeTrue())
				requestInfo, err := handlers.ContextRequestInfo(nextRequest)
				Expect(err).ToNot(HaveOccurred())
				Expect(requestInfo.RoutePool.IsEmpty()).To(BeFalse())
				len := 0
				var expectedAppId string
				requestInfo.RoutePool.Each(func(endpoint *route.Endpoint) {
					expectedAppId = endpoint.ApplicationId
					len++
				})
				Expect(len).To(Equal(1))
				Expect(expectedAppId).To(Equal("testid2"))
				Expect(resp.Code).To(Equal(http.StatusOK))
			})
		})

		Context("when conn limit is reached for all requested endpoints", func() {
			var testEndpoint *route.Endpoint
			BeforeEach(func() {
				pool = route.NewPool(2*time.Minute, "example.com", "/")
				testEndpoint = route.NewEndpoint("testid1", "1.3.5.6", 5679, "", "", nil, -1, "", modTag, "", false)
				testEndpoint.Stats.NumberConnections.Increment()
				testEndpoint.Stats.NumberConnections.Increment()
				testEndpoint.Stats.NumberConnections.Increment()
				pool.Put(testEndpoint)
				testEndpoint1 := route.NewEndpoint("testid2", "1.4.6.7", 5679, "", "", nil, -1, "", modTag, "", false)
				testEndpoint1.Stats.NumberConnections.Increment()
				testEndpoint1.Stats.NumberConnections.Increment()
				testEndpoint1.Stats.NumberConnections.Increment()
				pool.Put(testEndpoint1)
				reg.LookupReturns(pool)
			})
			It("returns a 503", func() {
				Expect(nextCalled).To(BeFalse())
				Expect(resp.Code).To(Equal(http.StatusServiceUnavailable))
			})

			It("increments the backend_exhausted_conn metric", func() {
				Expect(rep.CaptureBackendExhaustedConnsCallCount()).To(Equal(1))
			})
		})

		It("calls next with the pool", func() {
			Expect(nextCalled).To(BeTrue())
			requestInfo, err := handlers.ContextRequestInfo(nextRequest)
			Expect(err).ToNot(HaveOccurred())
			Expect(requestInfo.RoutePool).To(Equal(pool))
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

		Context("when given an incomplete app instance header", func() {
			BeforeEach(func() {
				appInstanceHeader := "app-id:"
				req.Header.Add("X-CF-App-Instance", appInstanceHeader)
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

		Context("when only the app id is given", func() {
			BeforeEach(func() {
				appInstanceHeader := "app-id"
				req.Header.Add("X-CF-App-Instance", appInstanceHeader)
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

		Context("when request info is not set on the request context", func() {
			BeforeEach(func() {
				handler = negroni.New()
				handler.Use(handlers.NewLookup(reg, rep, logger, 0))
				handler.UseHandler(nextHandler)
			})
			It("calls Fatal on the logger", func() {
				Expect(logger.FatalCallCount()).To(Equal(1))
				Expect(nextCalled).To(BeFalse())
			})
		})
	})
})
