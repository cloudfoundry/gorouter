package utils_test

import (
	"net/http"

	"code.cloudfoundry.org/gorouter/proxy/utils"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("AddHeaderIfNotPresentRewriter", func() {
	It("adds headers if missing in the original Header", func() {
		header := http.Header{}

		headerToAdd := http.Header{}
		headerToAdd.Add("foo", "bar1")
		headerToAdd.Add("foo", "bar2")

		rewriter := utils.AddHeaderIfNotPresentRewriter{Header: headerToAdd}

		rewriter.RewriteHeader(header)

		Expect(header).To(HaveKey("Foo"))
		Expect(header["Foo"]).To(ConsistOf("bar1", "bar2"))
	})

	It("does not add headers if present in the original Header", func() {
		header := http.Header{}
		header.Add("foo", "original")

		headerToAdd := http.Header{}
		headerToAdd.Add("foo", "bar1")
		headerToAdd.Add("foo", "bar2")

		rewriter := utils.AddHeaderIfNotPresentRewriter{Header: headerToAdd}

		rewriter.RewriteHeader(header)

		Expect(header).To(HaveKey("Foo"))
		Expect(header["Foo"]).To(ConsistOf("original"))
	})
})
