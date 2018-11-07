package utils

import (
	"bufio"
	"errors"
	"net"
	"net/http"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

type fakeResponseWriter struct {
	header                http.Header
	headerCalled          bool
	flushCalled           bool
	writeCalled           bool
	writeHeaderCalled     bool
	writeHeaderStatusCode int
}

func newFakeResponseWriter() *fakeResponseWriter {
	return &fakeResponseWriter{
		header: http.Header{},
	}
}

func (f *fakeResponseWriter) Header() http.Header {
	f.headerCalled = true
	return f.header
}

func (f *fakeResponseWriter) Write(b []byte) (int, error) {
	f.writeCalled = true
	return len(b), nil
}

func (f *fakeResponseWriter) WriteHeader(statusCode int) {
	f.writeHeaderCalled = true
	f.writeHeaderStatusCode = statusCode
}

func (f *fakeResponseWriter) Flush() {
	f.flushCalled = true
}

type fakeCloseNotifierResponseWriter struct {
	fakeResponseWriter
	c <-chan bool
}

func (f *fakeCloseNotifierResponseWriter) CloseNotify() <-chan bool {
	return f.c
}

type fakeHijackerResponseWriter struct {
	fakeResponseWriter
	hijackCalled bool
}

func (f *fakeHijackerResponseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	f.hijackCalled = true
	return nil, nil, errors.New("Not Implemented")
}

type fakeHeaderRewriter struct {
	rewriteHeaderCalled bool
	rewriteHeaderHeader http.Header
}

func (f *fakeHeaderRewriter) RewriteHeader(h http.Header) {
	f.rewriteHeaderCalled = true
	f.rewriteHeaderHeader = h
}

var _ = Describe("ProxyWriter", func() {
	var (
		closeNotifier chan bool
		fake          *fakeResponseWriter
		proxy         *proxyResponseWriter
	)

	BeforeEach(func() {
		fake = newFakeResponseWriter()
		proxy = NewProxyResponseWriter(fake)
	})

	It("delegates the call to Header", func() {
		proxy.Header()
		Expect(fake.headerCalled).To(BeTrue())
	})

	It("delegates the call to Flush", func() {
		proxy.Flush()
		Expect(fake.flushCalled).To(BeTrue())
	})

	It("delegates CloseNotify() if the writer is a http.CloseNotifier", func() {
		closeNotifier = make(chan bool, 1)
		fake := &fakeCloseNotifierResponseWriter{
			fakeResponseWriter: *newFakeResponseWriter(),
			c:                  closeNotifier,
		}
		proxy = NewProxyResponseWriter(fake)

		closeNotifier <- true
		Expect(proxy.CloseNotify()).To(Receive())
	})

	It("returns a channel if the writer is not http.CloseNotifier", func() {
		Expect(proxy.CloseNotify()).To(BeAssignableToTypeOf(make(<-chan bool)))
	})

	It("delegates Hijack() if the writer is a http.Hijacker", func() {
		fake := &fakeHijackerResponseWriter{
			fakeResponseWriter: *newFakeResponseWriter(),
		}
		proxy = NewProxyResponseWriter(fake)
		proxy.Hijack()
		Expect(fake.hijackCalled).To(BeTrue())
	})

	It("Hijack() returns error if the writer is not http.Hijacker", func() {
		_, _, err := proxy.Hijack()
		Expect(err).To(MatchError("response writer cannot hijack"))
	})

	It("delegates the call to WriteHeader", func() {
		proxy.WriteHeader(http.StatusOK)
		Expect(fake.writeHeaderCalled).To(BeTrue())
		Expect(fake.writeHeaderStatusCode).To(Equal(http.StatusOK))
	})

	It("WriteHeader sets Content-Type to empty if not set", func() {
		proxy.WriteHeader(http.StatusOK)
		Expect(fake.writeHeaderCalled).To(BeTrue())
		Expect(fake.writeHeaderStatusCode).To(Equal(http.StatusOK))
		Expect(fake.Header()).To(HaveKey("Content-Type"))
		Expect(fake.Header()["Content-Type"]).To(HaveLen(0))
	})

	It("WriteHeader returns if Done() has been called", func() {
		proxy.Done()
		proxy.WriteHeader(http.StatusOK)
		Expect(fake.writeHeaderCalled).To(BeFalse())
		Expect(fake.Header()).ToNot(HaveKey("Content-Type"))
	})

	It("WriteHeader sets the status once", func() {
		Expect(proxy.Status()).To(Equal(0))
		proxy.WriteHeader(http.StatusTeapot)
		Expect(proxy.Status()).To(Equal(http.StatusTeapot))
		proxy.WriteHeader(http.StatusOK)
		Expect(proxy.Status()).To(Equal(http.StatusTeapot))
	})

	It("delegates the call to Write", func() {
		l, err := proxy.Write([]byte("foo"))
		Expect(l).To(BeNumerically("==", 3))
		Expect(err).ToNot(HaveOccurred())
		Expect(fake.writeCalled).To(BeTrue())
	})

	It("Write returns if Done() has been called", func() {
		proxy.Done()
		l, err := proxy.Write([]byte("foo"))
		Expect(l).To(BeNumerically("==", 0))
		Expect(err).ToNot(HaveOccurred())
		Expect(fake.writeCalled).To(BeFalse())
	})

	It("Write calls WriteHeader with http.StatusOK if it has not been set", func() {
		proxy.Write([]byte("foo"))
		Expect(fake.writeCalled).To(BeTrue())
		Expect(fake.writeHeaderCalled).To(BeTrue())
		Expect(fake.writeHeaderStatusCode).To(Equal(http.StatusOK))
	})

	It("Write does not call WriteHeader it has been already set", func() {
		proxy.SetStatus(http.StatusTeapot)
		proxy.Write([]byte("foo"))
		Expect(fake.writeCalled).To(BeTrue())
		Expect(fake.writeHeaderCalled).To(BeFalse())
	})

	It("Write does not call WriteHeader it has been already set", func() {
		proxy.SetStatus(http.StatusTeapot)
		proxy.Write([]byte("foo"))
		Expect(fake.writeCalled).To(BeTrue())
		Expect(fake.writeHeaderCalled).To(BeFalse())
	})

	It("Write keeps track of the size", func() {
		proxy.Write([]byte("foo"))
		proxy.Write([]byte("foo"))
		Expect(proxy.Size()).To(BeNumerically("==", 6))
	})

	It("WriteHeader calls the registered HeaderRewriter with the proxied Header", func() {
		r1 := &fakeHeaderRewriter{}
		r2 := &fakeHeaderRewriter{}
		proxy.AddHeaderRewriter(r1)
		proxy.AddHeaderRewriter(r2)

		fake.Header().Add("foo", "bar")

		proxy.WriteHeader(http.StatusOK)
		Expect(r1.rewriteHeaderCalled).To(BeTrue())
		Expect(r1.rewriteHeaderHeader).To(HaveKey("Foo"))
		Expect(r2.rewriteHeaderCalled).To(BeTrue())
		Expect(r2.rewriteHeaderHeader).To(HaveKey("Foo"))
	})
})
