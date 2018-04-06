package handlers_test

import (
	"net/http"
	"net/http/httptest"

	"code.cloudfoundry.org/gorouter/handlers"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("X-Forwarded-Proto", func() {
	var (
		handler *handlers.XForwardedProto
		req     *http.Request
	)

	BeforeEach(func() {
		handler = &handlers.XForwardedProto{
			SkipSanitization:         func(req *http.Request) bool { return false },
			ForceForwardedProtoHttps: false,
			SanitizeForwardedProto:   false,
		}
		req, _ = http.NewRequest("GET", "/foo", nil)
	})

	processAndGetUpdatedHeader := func() string {
		var recordedRequest *http.Request
		mockNext := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			recordedRequest = r
		})
		handler.ServeHTTP(httptest.NewRecorder(), req, mockNext)
		return recordedRequest.Header.Get("X-Forwarded-Proto")
	}

	It("adds X-Forwarded-Proto if not present", func() {
		Expect(processAndGetUpdatedHeader()).To(Equal("http"))
	})

	It("doesn't overwrite X-Forwarded-Proto if present", func() {
		req.Header.Set("X-Forwarded-Proto", "https")
		Expect(processAndGetUpdatedHeader()).To(Equal("https"))
	})

	Context("Force Forwarded Proto HTTPS config option is set", func() {
		BeforeEach(func() {
			handler.ForceForwardedProtoHttps = true
		})
		It("forces the X-Forwarded-Proto header to https", func() {
			Expect(processAndGetUpdatedHeader()).To(Equal("https"))
		})
	})

	Context("when the sanitize forwarded proto option is enabled", func() {
		BeforeEach(func() {
			handler.SanitizeForwardedProto = true
		})
		It("prevents an http client from spoofing the X-Forwarded-Proto header", func() {
			req.Header.Set("X-Forwarded-Proto", "https")
			Expect(processAndGetUpdatedHeader()).To(Equal("http"))
		})
	})

	Context("when the request header should not be modified", func() {
		BeforeEach(func() {
			handler.SkipSanitization = func(req *http.Request) bool { return true }
		})
		Context("when sanitize is set", func() {
			BeforeEach(func() {
				handler.SanitizeForwardedProto = true
			})
			It("leaves ignores the sanitize option", func() {
				req.Header.Set("X-Forwarded-Proto", "potato")
				Expect(processAndGetUpdatedHeader()).To(Equal("potato"))
			})
		})
		Context("when force is set", func() {
			BeforeEach(func() {
				handler.ForceForwardedProtoHttps = true
			})
			It("leaves ignores the sanitize option", func() {
				req.Header.Set("X-Forwarded-Proto", "potato")
				Expect(processAndGetUpdatedHeader()).To(Equal("potato"))
			})
		})
	})
})
