package handler_test

import (
	"bytes"
	"io"
	"net/http"
	"runtime"
	"sync"
	"time"

	"github.com/onsi/gomega/gbytes"

	"code.cloudfoundry.org/gorouter/proxy/handler"
	"code.cloudfoundry.org/gorouter/test_util"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("Forwarder", func() {
	var clientConn, backendConn *MockReadWriter
	var forwarder *handler.Forwarder
	var logger *test_util.TestZapLogger

	buildFakeBackend := func(statusString string, responseBody io.Reader) *MockReadWriter {
		fakeBackend := io.MultiReader(bytes.NewBufferString("HTTP/1.1 "+statusString+"\r\n\r\n"), responseBody)
		return NewMockConn(fakeBackend)
	}

	BeforeEach(func() {
		logger = test_util.NewTestZapLogger("test")
		forwarder = &handler.Forwarder{
			BackendReadTimeout: time.Second,
			Logger:             logger,
		}
		clientConn = NewMockConn(bytes.NewReader([]byte("some client data")))
	})

	Context("when the backend gives a valid websocket response", func() {
		BeforeEach(func() {
			fakeResponseBody := io.MultiReader(bytes.NewBufferString("some websocket data"), &test_util.HangingReadCloser{})
			backendConn = buildFakeBackend("101 Switching Protocols", fakeResponseBody)
		})

		It("returns the status code that the backend responded with", func() {
			Expect(forwarder.ForwardIO(clientConn, backendConn)).To(Equal(http.StatusSwitchingProtocols))
		})

		It("always copies the full response header to the client conn, before it returns", func() {
			forwarder.ForwardIO(clientConn, backendConn)
			Expect(clientConn.GetWrittenBytes()).To(HavePrefix("HTTP/1.1 101 Switching Protocols"))
		})

		It("eventually writes all the response data", func() {
			backendConn = buildFakeBackend("101 Switching Protocols", bytes.NewBufferString("some websocket data"))
			Expect(forwarder.ForwardIO(clientConn, backendConn)).To(Equal(http.StatusSwitchingProtocols))
			Eventually(clientConn.GetWrittenBytes).Should(ContainSubstring("some websocket data"))
		})
	})

	Context("when the backend response has a non-101 status code", func() {
		BeforeEach(func() {
			backendConn = buildFakeBackend("200 OK", &test_util.HangingReadCloser{})
		})

		It("immediately returns the code, without waiting for either connection to close", func() {
			Expect(forwarder.ForwardIO(clientConn, backendConn)).To(Equal(http.StatusOK))
		})

		It("always copies the full response header to the client conn, before it returns", func() {
			forwarder.ForwardIO(clientConn, backendConn)
			Expect(clientConn.GetWrittenBytes()).To(HavePrefix("HTTP/1.1 200 OK"))
		})
	})

	Context("when the backend response is not a valid HTTP response", func() {
		BeforeEach(func() {
			backendConn = buildFakeBackend("banana", bytes.NewBufferString("bad data"))
		})

		It("immediately returns code 0, without waiting for either connection to close", func() {
			Expect(forwarder.ForwardIO(clientConn, backendConn)).To(Equal(0))
		})
	})

	Context("when the backend hangs indefinitely on reading the header", func() {
		BeforeEach(func() {
			backendConn = NewMockConn(&test_util.HangingReadCloser{})
		})

		It("times out after some time and logs the timeout", func() {
			Expect(forwarder.ForwardIO(clientConn, backendConn)).To(Equal(0))
			Expect(logger.Buffer()).To(gbytes.Say(`timeout waiting for http response from backend`))
		})
	})

	Context("when the backend responds after BackendReadTimeout", func() {
		var (
			sleepDuration time.Duration
		)

		BeforeEach(func() {
			forwarder.BackendReadTimeout = 10 * time.Millisecond
			sleepDuration = 100 * time.Millisecond
			backendConn = NewMockConn(&test_util.SlowReadCloser{SleepDuration: sleepDuration})
		})

		It("does not leak goroutines", func() {
			beforeGoroutineCount := runtime.NumGoroutine()
			Expect(forwarder.ForwardIO(clientConn, backendConn)).To(Equal(0))

			time.Sleep(2 * sleepDuration)

			Expect(runtime.NumGoroutine()).To(Equal(beforeGoroutineCount))
			Expect(logger.Buffer()).To(gbytes.Say(`timeout waiting for http response from backend`))
		})
	})
})

func NewMockConn(fakeBackend io.Reader) *MockReadWriter {
	return &MockReadWriter{
		buffer: &bytes.Buffer{},
		Reader: fakeBackend,
	}
}

type MockReadWriter struct {
	io.Reader
	sync.Mutex
	buffer *bytes.Buffer
}

func (m *MockReadWriter) Write(buffer []byte) (int, error) {
	time.Sleep(100 * time.Millisecond) // simulate some network delay
	m.Lock()
	defer m.Unlock()
	return m.buffer.Write(buffer)
}

func (m *MockReadWriter) GetWrittenBytes() string {
	m.Lock()
	defer m.Unlock()
	return m.buffer.String()
}
