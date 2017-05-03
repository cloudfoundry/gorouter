package handlers_test

import (
	"bufio"
	"net"
	"net/http"

	"code.cloudfoundry.org/gorouter/handlers"
	"code.cloudfoundry.org/gorouter/logger"
	"code.cloudfoundry.org/gorouter/test_util"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/ghttp"
	"github.com/urfave/negroni"
)

var _ = Describe("Protocolcheck", func() {
	var (
		logger     logger.Logger
		nextCalled bool
		server     *ghttp.Server
		n          *negroni.Negroni
	)

	BeforeEach(func() {
		logger = test_util.NewTestZapLogger("protocolcheck")
		nextCalled = false

		n = negroni.New()
		n.UseFunc(func(rw http.ResponseWriter, req *http.Request, next http.HandlerFunc) {
			next(rw, req)
		})
		n.Use(handlers.NewProtocolCheck(logger))
		n.UseHandlerFunc(func(http.ResponseWriter, *http.Request) {
			nextCalled = true
		})

		server = ghttp.NewUnstartedServer()
		server.AppendHandlers(n.ServeHTTP)
		server.Start()
	})

	AfterEach(func() {
		server.Close()
	})

	Context("http 1.1", func() {
		It("passes the request through", func() {
			conn, err := net.Dial("tcp", server.Addr())
			defer conn.Close()
			Expect(err).ToNot(HaveOccurred())
			respReader := bufio.NewReader(conn)

			conn.Write([]byte("GET / HTTP/1.1\r\nHost: example.com\r\n\r\n"))
			resp, err := http.ReadResponse(respReader, nil)
			Expect(err).ToNot(HaveOccurred())

			Expect(resp.StatusCode).To(Equal(200))
			Expect(nextCalled).To(BeTrue())
		})
	})

	Context("http 1.0", func() {
		It("passes the request through", func() {
			conn, err := net.Dial("tcp", server.Addr())
			defer conn.Close()
			Expect(err).ToNot(HaveOccurred())
			respReader := bufio.NewReader(conn)

			conn.Write([]byte("GET / HTTP/1.0\r\nHost: example.com\r\n\r\n"))
			resp, err := http.ReadResponse(respReader, nil)
			Expect(err).ToNot(HaveOccurred())

			Expect(resp.StatusCode).To(Equal(200))
			Expect(nextCalled).To(BeTrue())
		})
	})

	Context("unsupported versions of http", func() {
		It("returns a 400 bad request", func() {
			conn, err := net.Dial("tcp", server.Addr())
			Expect(err).ToNot(HaveOccurred())
			respReader := bufio.NewReader(conn)

			conn.Write([]byte("GET / HTTP/1.5\r\nHost: example.com\r\n\r\n"))
			resp, err := http.ReadResponse(respReader, nil)
			Expect(err).ToNot(HaveOccurred())
			Expect(resp.StatusCode).To(Equal(http.StatusBadRequest))

			Expect(nextCalled).To(BeFalse())
		})
	})

	Context("http2", func() {
		It("returns a 400 bad request", func() {
			conn, err := net.Dial("tcp", server.Addr())
			Expect(err).ToNot(HaveOccurred())
			respReader := bufio.NewReader(conn)

			conn.Write([]byte("PRI * HTTP/2.0\r\nHost: example.com\r\n\r\n"))

			resp, err := http.ReadResponse(respReader, nil)
			Expect(err).ToNot(HaveOccurred())
			Expect(resp.StatusCode).To(Equal(http.StatusBadRequest))
		})
	})
})
