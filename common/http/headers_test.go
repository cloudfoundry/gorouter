package http_test

import (
	"net/http"

	commonhttp "code.cloudfoundry.org/gorouter/common/http"
	"code.cloudfoundry.org/gorouter/common/http/fakes"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

const uuid_regex = `^[[:xdigit:]]{8}(-[[:xdigit:]]{4}){3}-[[:xdigit:]]{12}$`

// 64-bit random hexadecimal string
const b3_id_regex = `^[[:xdigit:]]{16}$`

var _ = Describe("Headers", func() {
	Describe("SetTraceHeaders", func() {
		var respWriter http.ResponseWriter

		BeforeEach(func() {
			respWriter = httpfakes.NewFakeResponseWriter()
		})

		JustBeforeEach(func() {
			commonhttp.SetTraceHeaders(respWriter, "1.1.1.1", "example.com")
		})

		It("sets the trace headers on the response", func() {
			Expect(respWriter.Header().Get(commonhttp.VcapRouterHeader)).To(Equal("1.1.1.1"))
			Expect(respWriter.Header().Get(commonhttp.VcapBackendHeader)).To(Equal("example.com"))
			Expect(respWriter.Header().Get(commonhttp.CfRouteEndpointHeader)).To(Equal("example.com"))
		})
	})

	Describe("ValidateCfAppInstance", func() {
		var (
			appInstanceHeader string
		)

		Context("when given a complete app instance header", func() {
			BeforeEach(func() {
				appInstanceHeader = "app-id:1"
			})

			It("returns the app id and app index", func() {
				appID, appIndex, err := commonhttp.ValidateCfAppInstance(appInstanceHeader)
				Expect(err).ToNot(HaveOccurred())
				Expect(appID).To(Equal("app-id"))
				Expect(appIndex).To(Equal("1"))
			})
		})

		Context("when given an incomplete app instance header", func() {
			BeforeEach(func() {
				appInstanceHeader = "app-id:"
			})

			It("returns an error", func() {
				_, _, err := commonhttp.ValidateCfAppInstance(appInstanceHeader)
				Expect(err).To(HaveOccurred())
			})
		})
		Context("when only the app id is given", func() {
			BeforeEach(func() {
				appInstanceHeader = "app-id"
			})

			It("returns an error", func() {
				_, _, err := commonhttp.ValidateCfAppInstance(appInstanceHeader)
				Expect(err).To(HaveOccurred())
			})
		})
	})
})
