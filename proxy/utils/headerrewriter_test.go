package utils_test

import (
	"net/http"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"code.cloudfoundry.org/gorouter/proxy/utils"
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

	It("headers match based with the canonicalized case-insentive key", func() {
		header := http.Header{}
		header.Add("FOO", "original")

		headerToAdd := http.Header{}
		headerToAdd.Add("fOo", "bar1")

		rewriter := utils.AddHeaderIfNotPresentRewriter{Header: headerToAdd}

		rewriter.RewriteHeader(header)

		Expect(header.Get("fOo")).To(Equal("original"))
		Expect(header.Get("Foo")).To(Equal("original"))
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

	It("headers match based with the canonicalized case-insentive key", func() {
		header := http.Header{}
		header.Add("X-Foo", "foo")
		header.Add("x-BAR", "bar")
		header.Add("x-foobar", "foobar")

		headerToRemove := http.Header{}
		headerToRemove.Add("X-fOo", "")
		headerToRemove.Add("x-bar", "")
		headerToRemove.Add("x-FoObAr", "")

		rewriter := utils.RemoveHeaderRewriter{Header: headerToRemove}

		Expect(header.Get("X-Foo")).ToNot(BeEmpty())
		Expect(header.Get("X-Bar")).ToNot(BeEmpty())
		Expect(header.Get("x-foobar")).ToNot(BeEmpty())

		rewriter.RewriteHeader(header)

		Expect(header.Get("X-Foo")).To(BeEmpty())
		Expect(header.Get("X-Bar")).To(BeEmpty())
		Expect(header.Get("x-foobar")).To(BeEmpty())
	})
})
