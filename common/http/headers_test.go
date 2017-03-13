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
})
