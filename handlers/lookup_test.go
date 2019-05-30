package handlers_test

import (
	"net/http"
	"net/http/httptest"
	"time"

	"code.cloudfoundry.org/gorouter/handlers"
	loggerfakes "code.cloudfoundry.org/gorouter/logger/fakes"
	"code.cloudfoundry.org/gorouter/metrics/fakes"
	fakeRegistry "code.cloudfoundry.org/gorouter/registry/fakes"
	"code.cloudfoundry.org/gorouter/route"
	"code.cloudfoundry.org/gorouter/test_util"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/urfave/negroni"
)

var _ = Describe("Lookup", func() {
	var (
		handler        *negroni.Negroni
		nextHandler    http.HandlerFunc
		logger         *loggerfakes.FakeLogger
		reg            *fakeRegistry.FakeRegistry
		rep            *fakes.FakeCombinedReporter
		resp           *httptest.ResponseRecorder
		req            *http.Request
		nextCalled     bool
		nextRequest    *http.Request
		maxConnections int64
	)

	nextHandler = http.HandlerFunc(func(_ http.ResponseWriter, req *http.Request) {
		nextCalled = true
		nextRequest = req
	})

	BeforeEach(func() {
		nextCalled = false
		nextRequest = &http.Request{}
		maxConnections = 2
		logger = new(loggerfakes.FakeLogger)
		rep = &fakes.FakeCombinedReporter{}
		reg = &fakeRegistry.FakeRegistry{}
		handler = negroni.New()
		req = test_util.NewRequest("GET", "example.com", "/", nil)
		resp = httptest.NewRecorder()
		handler.Use(handlers.NewRequestInfo())
		handler.Use(handlers.NewLookup(reg, rep, logger))
		handler.UseHandler(nextHandler)
	})

	JustBeforeEach(func() {
		handler.ServeHTTP(resp, req)
	})

	Context("when there is no pool that matches the request", func() {
		It("sends a bad request metric", func() {
			Expect(rep.CaptureBadRequestCallCount()).To(Equal(1))
		})

		It("Sets X-Cf-RouterError to unknown_route", func() {
			Expect(resp.Header().Get("X-Cf-RouterError")).To(Equal("unknown_route"))
		})

		It("Sets Cache-Control to public,max-age=2", func() {
			Expect(resp.Header().Get("Cache-Control")).To(Equal("public,max-age=2"))
		})

		It("returns a 404 NotFound and does not call next", func() {
			Expect(nextCalled).To(BeFalse())
			Expect(resp.Code).To(Equal(http.StatusNotFound))
		})

		It("has a meaningful response", func() {
			Expect(resp.Body.String()).To(ContainSubstring("Requested route ('example.com') does not exist"))
		})
	})

	Context("when there is a pool that matches the request, but it has no endpoints", func() {
		var pool *route.EndpointPool

		BeforeEach(func() {
			pool = route.NewPool(&route.PoolOpts{
				Logger:             logger,
				RetryAfterFailure:  2 * time.Minute,
				Host:               "example.com",
				ContextPath:        "/",
				MaxConnsPerBackend: maxConnections,
			})
			reg.LookupReturns(pool)
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

	})

	Context("when there is a pool that matches the request, and it has endpoints", func() {
		Context("when conn limit is set to unlimited", func() {
			BeforeEach(func() {
				pool := route.NewPool(&route.PoolOpts{
					Logger:             logger,
					RetryAfterFailure:  2 * time.Minute,
					Host:               "example.com",
					ContextPath:        "/",
					MaxConnsPerBackend: 0,
				})
				testEndpoint := route.NewEndpoint(&route.EndpointOpts{Host: "1.3.5.6", Port: 5679})
				for i := 0; i < 5; i++ {
					testEndpoint.Stats.NumberConnections.Increment()
				}
				pool.Put(testEndpoint)
				testEndpoint1 := route.NewEndpoint(&route.EndpointOpts{Host: "1.2.3.6", Port: 5679})
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
				pool := route.NewPool(&route.PoolOpts{
					Logger:             logger,
					RetryAfterFailure:  2 * time.Minute,
					Host:               "example.com",
					ContextPath:        "/",
					MaxConnsPerBackend: maxConnections,
				})
				testEndpoint := route.NewEndpoint(&route.EndpointOpts{AppId: "testid1", Host: "1.3.5.6", Port: 5679})
				testEndpoint.Stats.NumberConnections.Increment()
				testEndpoint.Stats.NumberConnections.Increment()
				testEndpoint.Stats.NumberConnections.Increment()
				pool.Put(testEndpoint)
				testEndpoint1 := route.NewEndpoint(&route.EndpointOpts{AppId: "testid2", Host: "1.2.3.6", Port: 5679})
				pool.Put(testEndpoint1)
				reg.LookupReturns(pool)
			})

			It("calls next with the pool", func() {
				Expect(nextCalled).To(BeTrue())
				requestInfo, err := handlers.ContextRequestInfo(nextRequest)
				Expect(err).ToNot(HaveOccurred())
				Expect(requestInfo.RoutePool.IsEmpty()).To(BeFalse())
			})
		})

		Context("when conn limit is reached for all requested endpoints", func() {
			var testEndpoint *route.Endpoint
			BeforeEach(func() {
				pool := route.NewPool(&route.PoolOpts{
					Logger:             logger,
					RetryAfterFailure:  2 * time.Minute,
					Host:               "example.com",
					ContextPath:        "/",
					MaxConnsPerBackend: maxConnections,
				})
				testEndpoint = route.NewEndpoint(&route.EndpointOpts{Host: "1.3.5.6", Port: 5679})
				testEndpoint.Stats.NumberConnections.Increment()
				testEndpoint.Stats.NumberConnections.Increment()
				testEndpoint.Stats.NumberConnections.Increment()
				pool.Put(testEndpoint)
				testEndpoint1 := route.NewEndpoint(&route.EndpointOpts{Host: "1.4.6.7", Port: 5679})
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

		Context("when a specific instance is requested", func() {
			BeforeEach(func() {
				pool := route.NewPool(&route.PoolOpts{
					Logger:             logger,
					RetryAfterFailure:  2 * time.Minute,
					Host:               "example.com",
					ContextPath:        "/",
					MaxConnsPerBackend: maxConnections,
				})
				exampleEndpoint := &route.Endpoint{Stats: route.NewStats()}
				pool.Put(exampleEndpoint)
				reg.LookupReturns(pool)

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
				pool := route.NewPool(&route.PoolOpts{
					Logger:             logger,
					RetryAfterFailure:  2 * time.Minute,
					Host:               "example.com",
					ContextPath:        "/",
					MaxConnsPerBackend: maxConnections,
				})
				exampleEndpoint := &route.Endpoint{Stats: route.NewStats()}
				pool.Put(exampleEndpoint)
				reg.LookupReturns(pool)

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
				pool := route.NewPool(&route.PoolOpts{
					Logger:             logger,
					RetryAfterFailure:  2 * time.Minute,
					Host:               "example.com",
					ContextPath:        "/",
					MaxConnsPerBackend: maxConnections,
				})
				exampleEndpoint := &route.Endpoint{Stats: route.NewStats()}
				pool.Put(exampleEndpoint)
				reg.LookupReturns(pool)

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
				pool := route.NewPool(&route.PoolOpts{
					Logger:             logger,
					RetryAfterFailure:  2 * time.Minute,
					Host:               "example.com",
					ContextPath:        "/",
					MaxConnsPerBackend: maxConnections,
				})
				exampleEndpoint := &route.Endpoint{Stats: route.NewStats()}
				pool.Put(exampleEndpoint)
				reg.LookupReturns(pool)

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
				handler.Use(handlers.NewLookup(reg, rep, logger))
				handler.UseHandler(nextHandler)

				pool := route.NewPool(&route.PoolOpts{
					Logger:             logger,
					RetryAfterFailure:  2 * time.Minute,
					Host:               "example.com",
					ContextPath:        "/",
					MaxConnsPerBackend: 0,
				})
				testEndpoint := route.NewEndpoint(&route.EndpointOpts{Host: "1.3.5.6", Port: 5679})
				for i := 0; i < 5; i++ {
					testEndpoint.Stats.NumberConnections.Increment()
				}
				pool.Put(testEndpoint)
				testEndpoint1 := route.NewEndpoint(&route.EndpointOpts{Host: "1.2.3.6", Port: 5679})
				pool.Put(testEndpoint1)
				reg.LookupReturns(pool)
			})
			It("calls Fatal on the logger", func() {
				Expect(logger.FatalCallCount()).To(Equal(1))
				Expect(nextCalled).To(BeFalse())
			})
		})
	})
})
