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

var _ = Describe("RemoveHeaderRewriter", func() {
	It("remove headers with same name and only those", func() {
		header := http.Header{}
		header.Add("foo1", "bar1")
		header.Add("foo1", "bar2")
		header.Add("foo2", "bar1")
		header.Add("foo3", "bar1")
		header.Add("foo3", "bar2")

		headerToRemove := http.Header{}
		headerToRemove.Add("foo1", "")
		headerToRemove.Add("foo2", "")

		rewriter := utils.RemoveHeaderRewriter{Header: headerToRemove}

		rewriter.RewriteHeader(header)

		Expect(header).ToNot(HaveKey("Foo1"))
		Expect(header).ToNot(HaveKey("Foo2"))
		Expect(header).To(HaveKey("Foo3"))
		Expect(header["Foo3"]).To(ConsistOf("bar1", "bar2"))
	})
})
