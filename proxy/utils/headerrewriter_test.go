package utils_test

import (
	"net/http"

	"code.cloudfoundry.org/gorouter/proxy/utils"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("InjectHeaderRewriter", func() {
	It("injects headers if missing in the original Header", func() {
		header := http.Header{}

		headerToInject := http.Header{}
		headerToInject.Add("foo", "bar1")
		headerToInject.Add("foo", "bar2")

		rewriter := utils.InjectHeaderRewriter{headerToInject}

		rewriter.RewriteHeader(header)

		Expect(header).To(HaveKey("Foo"))
		Expect(header["Foo"]).To(ConsistOf("bar1", "bar2"))
	})

	It("does not inject headers if present in the original Header", func() {
		header := http.Header{}
		header.Add("foo", "original")

		headerToInject := http.Header{}
		headerToInject.Add("foo", "bar1")
		headerToInject.Add("foo", "bar2")

		rewriter := utils.InjectHeaderRewriter{headerToInject}

		rewriter.RewriteHeader(header)

		Expect(header).To(HaveKey("Foo"))
		Expect(header["Foo"]).To(ConsistOf("original"))
	})
})
