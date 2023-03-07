package handlers_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"

	"code.cloudfoundry.org/gorouter/handlers"
	"code.cloudfoundry.org/gorouter/logger"
	"code.cloudfoundry.org/gorouter/test_util"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gbytes"
	"github.com/urfave/negroni"
)

const uuid_regex = `^[[:xdigit:]]{32}(-[[:xdigit:]]{16})$`

var _ = Describe("Set Vcap Request Id header", func() {
	var (
		logger          logger.Logger
		nextCalled      bool
		resp            *httptest.ResponseRecorder
		req             *http.Request
		nextHandler     http.HandlerFunc
		handler         negroni.Handler
		previousReqInfo *handlers.RequestInfo
		newReqInfo      *handlers.RequestInfo
		vcapIdHeader    string
	)

	nextHandler = http.HandlerFunc(func(_ http.ResponseWriter, req *http.Request) {
		vcapIdHeader = req.Header.Get(handlers.VcapRequestIdHeader)
		var err error
		newReqInfo, err = handlers.ContextRequestInfo(req)
		Expect(err).NotTo(HaveOccurred())
		nextCalled = true
	})

	BeforeEach(func() {
		logger = test_util.NewTestZapLogger("setVcapRequestIdHeader")
		nextCalled = false
		handler = handlers.NewVcapRequestIdHeader(logger)

		previousReqInfo = new(handlers.RequestInfo)
		req = test_util.NewRequest("GET", "example.com", "/", nil).
			WithContext(context.WithValue(context.Background(), handlers.RequestInfoCtxKey, previousReqInfo))
		resp = httptest.NewRecorder()
	})

	JustBeforeEach(func() {
		handler.ServeHTTP(resp, req, nextHandler)
	})

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

	It("sets request context", func() {
		Expect(newReqInfo.TraceID).To(MatchRegexp(b3IDRegex))
		Expect(newReqInfo.SpanID).To(MatchRegexp(b3SpanRegex))
	})

	Context("when request context has trace and span id", func() {
		BeforeEach(func() {
			previousReqInfo.TraceID = strings.Repeat("1", 32)
			previousReqInfo.SpanID = strings.Repeat("2", 16)
		})

		It("sets the ID header from request context", func() {
			Expect(vcapIdHeader).To(Equal(strings.Repeat("1", 32) + "-" + strings.Repeat("2", 16)))
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
