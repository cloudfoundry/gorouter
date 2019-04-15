package handlers_test

import (
	"crypto/tls"
	"net/http"
	"net/http/httptest"

	"code.cloudfoundry.org/gorouter/handlers"
	logger_fakes "code.cloudfoundry.org/gorouter/logger/fakes"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("X-Forwarded-Proto", func() {
	var (
		req        *http.Request
		res        *httptest.ResponseRecorder
		nextCalled bool
		logger     *logger_fakes.FakeLogger
	)

	BeforeEach(func() {
		logger = new(logger_fakes.FakeLogger)
		req, _ = http.NewRequest("GET", "/foo", nil)
		nextCalled = false
	})

	processAndGetUpdatedHeader := func(handler *handlers.XForwardedProto) string {
		recordedRequest := &http.Request{}
		mockNext := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			recordedRequest = r
			nextCalled = true
		})
		res = httptest.NewRecorder()
		handler.ServeHTTP(res, req, mockNext)
		return recordedRequest.Header.Get("X-Forwarded-Proto")
	}

	Context("when the SkipSanitization is true", func() {
		var handler *handlers.XForwardedProto
		BeforeEach(func() {
			handler = &handlers.XForwardedProto{
				SkipSanitization:         func(req *http.Request) bool { return true },
				ForceForwardedProtoHttps: false,
				SanitizeForwardedProto:   false,
				Logger:                   logger,
			}
		})

		It("only calls next handler", func() {
			processAndGetUpdatedHeader(handler)
			Expect(nextCalled).To(BeTrue())
		})
		// This is when request is back from route services and it should not be touched
		It("does not sanitize X-Forwarded-Proto", func() {
			req.Header.Set("X-Forwarded-Proto", "http")
			Expect(processAndGetUpdatedHeader(handler)).To(Equal("http"))
		})

		It("doesn't overwrite X-Forwarded-Proto if present", func() {
			req.Header.Set("X-Forwarded-Proto", "https")
			Expect(processAndGetUpdatedHeader(handler)).To(Equal("https"))
		})
	})

	Context("when the ForceForwardedProtoHttps is true", func() {
		var handler *handlers.XForwardedProto
		BeforeEach(func() {
			handler = &handlers.XForwardedProto{
				SkipSanitization:         func(req *http.Request) bool { return false },
				ForceForwardedProtoHttps: true,
				SanitizeForwardedProto:   false,
				Logger:                   logger,
			}
		})

		It("overrides X-Forwarded-Proto if present", func() {
			req.Header.Set("X-Forwarded-Proto", "http")
			Expect(processAndGetUpdatedHeader(handler)).To(Equal("https"))
			Expect(nextCalled).To(BeTrue())
		})

		It("sets X-Forwarded-Proto to https if not present", func() {
			Expect(processAndGetUpdatedHeader(handler)).To(Equal("https"))
			Expect(nextCalled).To(BeTrue())
		})
	})

	Context("when the SanitizeForwardedProto is true", func() {
		var handler *handlers.XForwardedProto
		BeforeEach(func() {
			handler = &handlers.XForwardedProto{
				SkipSanitization:         func(req *http.Request) bool { return false },
				ForceForwardedProtoHttps: false,
				SanitizeForwardedProto:   true,
				Logger:                   logger,
			}
		})

		It("sets X-Forwarded-Proto to http when connecting over http with header set to https", func() {
			req.Header.Set("X-Forwarded-Proto", "https")
			Expect(processAndGetUpdatedHeader(handler)).To(Equal("http"))
			Expect(nextCalled).To(BeTrue())
		})

		It("sets X-Forwarded-Proto to https when connecting over https with header set to http", func() {
			req.Header.Set("X-Forwarded-Proto", "http")
			req.TLS = &tls.ConnectionState{}
			Expect(processAndGetUpdatedHeader(handler)).To(Equal("https"))
			Expect(nextCalled).To(BeTrue())
		})

		It("sets X-Forwarded-Proto to http if client is not providing one and connecting over http", func() {
			Expect(processAndGetUpdatedHeader(handler)).To(Equal("http"))
			Expect(nextCalled).To(BeTrue())
		})
	})

	Context("when the client does not provide an X-Forwarded-Proto header with every property to false", func() {
		var handler *handlers.XForwardedProto
		BeforeEach(func() {
			handler = &handlers.XForwardedProto{
				SkipSanitization:         func(req *http.Request) bool { return false },
				ForceForwardedProtoHttps: false,
				SanitizeForwardedProto:   false,
				Logger:                   logger,
			}
		})

		It("sets X-Forwarded-Proto to http when connecting over http with header not set", func() {
			Expect(processAndGetUpdatedHeader(handler)).To(Equal("http"))
			Expect(nextCalled).To(BeTrue())
		})

		It("sets X-Forwarded-Proto to https when connecting over https with header not set", func() {
			req.TLS = &tls.ConnectionState{}
			Expect(processAndGetUpdatedHeader(handler)).To(Equal("https"))
			Expect(nextCalled).To(BeTrue())
		})

		It("sets X-Forwarded-Proto to http if client is not providing one and connecting over http", func() {
			Expect(processAndGetUpdatedHeader(handler)).To(Equal("http"))
			Expect(nextCalled).To(BeTrue())
		})
	})
})
