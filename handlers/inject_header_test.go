package handlers_test

import (
	"net/http"
	"net/http/httptest"

	"code.cloudfoundry.org/gorouter/handlers"
	logger_fakes "code.cloudfoundry.org/gorouter/logger/fakes"

	"github.com/urfave/negroni"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("InjectHeaderHandler", func() {
	processAndGetHeaders := func(listHandlers ...negroni.Handler) http.Header {
		mockedService := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header()["X-Foo"] = []string{"foo"}
			w.Write([]byte("some body"))
		})

		n := negroni.New()
		n.Use(handlers.NewRequestInfo())
		n.Use(handlers.NewProxyWriter(new(logger_fakes.FakeLogger)))
		for _, h := range listHandlers {
			n.Use(h)
		}
		n.UseHandler(mockedService)

		res := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", "/foo", nil)
		n.ServeHTTP(res, req)
		return res.Header()
	}

	It("dones not change the header if already present in response", func() {
		headers := processAndGetHeaders(handlers.NewInjectHeaderHandler("X-Foo", "bar"))
		Expect(headers["X-Foo"]).To(Equal([]string{"foo"}))
	})

	It("injects a header if it is not present and keeps existing ones", func() {
		headers := processAndGetHeaders(handlers.NewInjectHeaderHandler("X-Bar", "bar"))
		Expect(headers["X-Bar"]).To(Equal([]string{"bar"}))
		Expect(headers["X-Foo"]).To(Equal([]string{"foo"}))
	})

	It("injects multiple values for same header", func() {
		headers := processAndGetHeaders(
			handlers.NewInjectHeaderHandler("X-Bar", "bar1"),
			handlers.NewInjectHeaderHandler("X-Bar", "bar2"),
		)
		Expect(headers["X-Bar"]).To(HaveLen(2))
		Expect(headers["X-Bar"]).To(ContainElement("bar1"))
		Expect(headers["X-Bar"]).To(ContainElement("bar2"))
	})
})
