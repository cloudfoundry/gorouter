package handlers_test

import (
	"errors"
	"fmt"
	"net/http"

	"code.cloudfoundry.org/gorouter/handlers"
	"code.cloudfoundry.org/gorouter/logger"
	"code.cloudfoundry.org/gorouter/test_util"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gbytes"
	"github.com/onsi/gomega/ghttp"
	"github.com/urfave/negroni"
)

var _ = Describe("Paniccheck", func() {
	var (
		server      *ghttp.Server
		n           *negroni.Negroni
		heartbeatOK int32
		testLogger  logger.Logger
	)

	BeforeEach(func() {
		heartbeatOK = 1

		testLogger = test_util.NewTestZapLogger("test")

		n = negroni.New()
		n.UseFunc(func(rw http.ResponseWriter, req *http.Request, next http.HandlerFunc) {
			next(rw, req)
		})
		n.Use(handlers.NewPanicCheck(&heartbeatOK, testLogger))

		server = ghttp.NewUnstartedServer()
		server.AppendHandlers(n.ServeHTTP)
		server.Start()
	})

	AfterEach(func() {
		server.Close()
	})

	Context("when something panics", func() {
		BeforeEach(func() {
			n.UseHandlerFunc(func(http.ResponseWriter, *http.Request) {
				panic(errors.New("we expect this panic"))
			})
		})
		It("the healthcheck is set to 0", func() {
			_, err := http.Get(fmt.Sprintf("http://%s", server.Addr()))
			Expect(err).ToNot(HaveOccurred())

			Expect(heartbeatOK).To(Equal(int32(0)))
		})

		It("responds with a 503 Service Unavailable", func() {
			resp, err := http.Get(fmt.Sprintf("http://%s", server.Addr()))
			Expect(err).ToNot(HaveOccurred())
			Expect(resp.StatusCode).To(Equal(503))
		})

		It("logs the panic message", func() {
			_, err := http.Get(fmt.Sprintf("http://%s", server.Addr()))
			Expect(err).ToNot(HaveOccurred())

			Expect(testLogger).To(gbytes.Say("we expect this panic"))
		})
	})

	Context("when there is no panic", func() {
		It("leaves the healthcheck set to 1", func() {
			_, err := http.Get(fmt.Sprintf("http://%s", server.Addr()))
			Expect(err).ToNot(HaveOccurred())

			Expect(heartbeatOK).To(Equal(int32(1)))
		})

		It("responds with a 200", func() {
			resp, err := http.Get(fmt.Sprintf("http://%s", server.Addr()))
			Expect(err).ToNot(HaveOccurred())
			Expect(resp.StatusCode).To(Equal(200))
		})

		It("does not log anything", func() {
			_, err := http.Get(fmt.Sprintf("http://%s", server.Addr()))
			Expect(err).ToNot(HaveOccurred())

			Expect(testLogger).NotTo(gbytes.Say("panic-check"))
		})
	})

	Context("when the panic is due to an abort", func() {
		BeforeEach(func() {
			n.UseHandlerFunc(func(http.ResponseWriter, *http.Request) {
				// This panic occurs when a client goes away in the middle of a request
				// this is a panic we expect to see in normal operation and is safe to ignore
				panic(http.ErrAbortHandler)
			})
		})
		It("the healthcheck is set to 1", func() {
			_, err := http.Get(fmt.Sprintf("http://%s", server.Addr()))
			Expect(err).ToNot(HaveOccurred())

			Expect(heartbeatOK).To(Equal(int32(1)))
		})

		It("does not log anything", func() {
			_, err := http.Get(fmt.Sprintf("http://%s", server.Addr()))
			Expect(err).ToNot(HaveOccurred())

			Expect(testLogger).NotTo(gbytes.Say("panic-check"))
		})
	})
})
