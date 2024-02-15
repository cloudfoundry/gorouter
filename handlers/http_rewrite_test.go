package handlers_test

import (
	"net/http"
	"net/http/httptest"

	"code.cloudfoundry.org/gorouter/config"
	"code.cloudfoundry.org/gorouter/handlers"
	logger_fakes "code.cloudfoundry.org/gorouter/logger/fakes"

	"github.com/urfave/negroni/v3"

	. "github.com/onsi/ginkgo/v2"
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
		n.Use(handlers.NewHTTPRewriteHandler(cfg, []string{}))
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

	Describe("with Responses.InjectHeadersIfNotPresent", func() {
		It("does not change the header if already present in response", func() {
			cfg := config.HTTPRewrite{
				Responses: config.HTTPRewriteResponses{
					AddHeadersIfNotPresent: []config.HeaderNameValue{
						{Name: "X-Foo", Value: "bar"},
					},
				},
			}
			res := process(cfg)
			Expect(res.Header()["X-Foo"]).To(ConsistOf("foo"))
		})

		It("adds a header if it is not present and keeps existing ones", func() {
			cfg := config.HTTPRewrite{
				Responses: config.HTTPRewriteResponses{
					AddHeadersIfNotPresent: []config.HeaderNameValue{
						{Name: "X-Bar", Value: "bar"},
					},
				},
			}
			res := process(cfg)
			Expect(res.Header()["X-Foo"]).To(ConsistOf("foo"))
			Expect(res.Header()["X-Bar"]).To(ConsistOf("bar"))
		})

		It("adds multiple values for same header", func() {
			cfg := config.HTTPRewrite{
				Responses: config.HTTPRewriteResponses{
					AddHeadersIfNotPresent: []config.HeaderNameValue{
						{Name: "X-Bar", Value: "bar1"},
						{Name: "X-Bar", Value: "bar2"},
					},
				},
			}
			res := process(cfg)
			Expect(res.Header()["X-Foo"]).To(ConsistOf("foo"))
			Expect(res.Header()["X-Bar"]).To(ConsistOf("bar1", "bar2"))
		})

		It("canonicalizes the header names to be case-insentive", func() {
			cfg := config.HTTPRewrite{
				Responses: config.HTTPRewriteResponses{
					AddHeadersIfNotPresent: []config.HeaderNameValue{
						{Name: "x-FoO", Value: "bar"},
						{Name: "x-BaR", Value: "bar1"},
						{Name: "X-bAr", Value: "bar2"},
					},
				},
			}
			res := process(cfg)
			Expect(res.Header()["X-Foo"]).To(ConsistOf("foo"))
			Expect(res.Header()["X-Bar"]).To(ConsistOf("bar1", "bar2"))
		})
	})

	Describe("with Responses.RemoveHeaders", func() {
		It("does not remove headers that have same name", func() {
			cfg := config.HTTPRewrite{
				Responses: config.HTTPRewriteResponses{
					RemoveHeaders: []config.HeaderNameValue{
						{Name: "X-Bar"},
					},
				},
			}
			res := process(cfg)
			Expect(res.Header()).To(HaveKey("X-Foo"))
			Expect(res.Header()["X-Foo"]).To(ConsistOf("foo"))
		})

		It("removes headers that have same name", func() {
			cfg := config.HTTPRewrite{
				Responses: config.HTTPRewriteResponses{
					RemoveHeaders: []config.HeaderNameValue{
						{Name: "X-Foo"},
					},
				},
			}
			res := process(cfg)
			Expect(res.Header()).ToNot(HaveKey("X-Foo"))
		})

		It("canonicalizes the header names to be case-insentive", func() {
			cfg := config.HTTPRewrite{
				Responses: config.HTTPRewriteResponses{
					RemoveHeaders: []config.HeaderNameValue{
						{Name: "x-FoO"},
					},
				},
			}
			res := process(cfg)
			Expect(res.Header()).ToNot(HaveKey("X-Foo"))
		})
	})

	Describe("with Responses.RemoveHeaders and Responses.InjectHeadersIfNotPresent", func() {
		It("removes and adds the header", func() {
			cfg := config.HTTPRewrite{
				Responses: config.HTTPRewriteResponses{
					RemoveHeaders: []config.HeaderNameValue{
						{Name: "X-Foo"},
					},
					AddHeadersIfNotPresent: []config.HeaderNameValue{
						{Name: "X-Foo", Value: "bar"},
					},
				},
			}
			res := process(cfg)
			Expect(res.Header()).To(HaveKey("X-Foo"))
			Expect(res.Header()["X-Foo"]).To(ConsistOf("bar"))
		})
	})

	Describe("headersToAlwaysRemove", func() {
		process := func(headersToAlwaysRemove []string) *httptest.ResponseRecorder {
			mockedService := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header()["X-Foo"] = []string{"foo"}
				w.WriteHeader(http.StatusTeapot)
				w.Write([]byte("I'm a little teapot, short and stout."))
			})

			n := negroni.New()
			n.Use(handlers.NewRequestInfo())
			n.Use(handlers.NewProxyWriter(new(logger_fakes.FakeLogger)))
			n.Use(handlers.NewHTTPRewriteHandler(config.HTTPRewrite{}, headersToAlwaysRemove))
			n.UseHandler(mockedService)

			res := httptest.NewRecorder()
			req, _ := http.NewRequest("GET", "/foo", nil)
			n.ServeHTTP(res, req)
			return res
		}

		It("removes the header", func() {
			res := process([]string{"X-Foo"})
			Expect(res.Header().Get("X-Foo")).To(BeEmpty())
		})
	})
})
