package handlers_test

import (
	"net/http"
	"net/http/httptest"

	"code.cloudfoundry.org/gorouter/config"
	"code.cloudfoundry.org/gorouter/handlers"
	logger_fakes "code.cloudfoundry.org/gorouter/logger/fakes"

	"github.com/urfave/negroni"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("HTTPRewrite Handler", func() {
	process := func(cfg config.HTTPRewrite) *httptest.ResponseRecorder {
		mockedService := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header()["X-Foo"] = []string{"foo"}
			w.WriteHeader(http.StatusTeapot)
			w.Write([]byte("I'm a little teapot, short and stout."))
		})

		n := negroni.New()
		n.Use(handlers.NewRequestInfo())
		n.Use(handlers.NewProxyWriter(new(logger_fakes.FakeLogger)))
		n.Use(handlers.NewHTTPRewriteHandler(cfg))
		n.UseHandler(mockedService)

		res := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", "/foo", nil)
		n.ServeHTTP(res, req)
		return res
	}

	It("calls the next handler", func() {
		res := process(config.HTTPRewrite{})
		Expect(res.Header()["X-Foo"]).To(ConsistOf("foo"))
		Expect(res.Body.Bytes()).To(Equal([]byte("I'm a little teapot, short and stout.")))
	})

	Describe("with AddResponseHeaders", func() {
		It("does not change the header if already present in response", func() {
			cfg := config.HTTPRewrite{
				InjectResponseHeaders: []config.HeaderNameValue{
					{Name: "X-Foo", Value: "bar"},
				},
			}
			res := process(cfg)
			Expect(res.Header()["X-Foo"]).To(ConsistOf("foo"))
		})

		It("injects a header if it is not present and keeps existing ones", func() {
			cfg := config.HTTPRewrite{
				InjectResponseHeaders: []config.HeaderNameValue{
					{Name: "X-Bar", Value: "bar"},
				},
			}
			res := process(cfg)
			Expect(res.Header()["X-Foo"]).To(ConsistOf("foo"))
			Expect(res.Header()["X-Bar"]).To(ConsistOf("bar"))
		})

		It("injects multiple values for same header", func() {
			cfg := config.HTTPRewrite{
				InjectResponseHeaders: []config.HeaderNameValue{
					{Name: "X-Bar", Value: "bar1"},
					{Name: "X-Bar", Value: "bar2"},
				},
			}
			res := process(cfg)
			Expect(res.Header()["X-Foo"]).To(ConsistOf("foo"))
			Expect(res.Header()["X-Bar"]).To(ConsistOf("bar1", "bar2"))
		})
	})
})
