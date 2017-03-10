package handlers_test

import (
	"net/http"
	"net/http/httptest"

	"code.cloudfoundry.org/gorouter/handlers"
	"code.cloudfoundry.org/gorouter/logger"
	"code.cloudfoundry.org/gorouter/test_util"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gbytes"
	"github.com/urfave/negroni"
)

const uuid_regex = `^[[:xdigit:]]{8}(-[[:xdigit:]]{4}){3}-[[:xdigit:]]{12}$`

var _ = Describe("Set Vcap Request Id header", func() {
	var (
		logger       logger.Logger
		nextCalled   bool
		resp         *httptest.ResponseRecorder
		req          *http.Request
		nextHandler  http.HandlerFunc
		nextRequest  *http.Request
		handler      negroni.Handler
		vcapIdHeader string
	)

	nextHandler = http.HandlerFunc(func(_ http.ResponseWriter, req *http.Request) {
		vcapIdHeader = req.Header.Get(handlers.VcapRequestIdHeader)
		nextCalled = true
		nextRequest = req
	})

	BeforeEach(func() {
		logger = test_util.NewTestZapLogger("setVcapRequestIdHeader")
		nextCalled = false
		handler = handlers.NewsetVcapRequestIdHeader(logger)

		req = test_util.NewRequest("GET", "example.com", "/", nil)
		resp = httptest.NewRecorder()
	})

	BeforeEach(func() {
		handler.ServeHTTP(resp, req, nextHandler)
	})

	Context("when UUID generated the guid", func() {

		It("sets the ID header correctly", func() {
			Expect(vcapIdHeader).ToNot(BeEmpty())
			Expect(vcapIdHeader).To(MatchRegexp(uuid_regex))
		})

		It("always call next", func() {
			Expect(nextCalled).To(BeTrue())
		})

		It("logs the header", func() {
			Expect(logger).To(gbytes.Say("vcap-request-id-header-set"))
			Expect(logger).To(gbytes.Say(vcapIdHeader))
		})
	})

	Context("when X-Vcap-Request-Id is set", func() {
		BeforeEach(func() {
			req.Header.Set(handlers.VcapRequestIdHeader, "BOGUS-HEADER")
		})

		It("overwrites the X-Vcap-Request-Id header", func() {
			Expect(vcapIdHeader).ToNot(BeEmpty())
			Expect(vcapIdHeader).ToNot(Equal("BOGUS-HEADER"))
			Expect(vcapIdHeader).To(MatchRegexp(uuid_regex))
		})

		It("logs the header", func() {
			Expect(logger).To(gbytes.Say("vcap-request-id-header-set"))
			Expect(logger).To(gbytes.Say(vcapIdHeader))
		})
	})
})
