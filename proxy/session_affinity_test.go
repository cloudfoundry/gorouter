package proxy_test

import (
	"net/http"
	"time"

	"github.com/cloudfoundry/gorouter/proxy"
	"github.com/cloudfoundry/gorouter/test_util"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("Session Affinity", func() {
	var done chan bool
	var jSessionIdCookie *http.Cookie

	responseNoCookies := func(x *test_util.HttpConn) {
		_, err := http.ReadRequest(x.Reader)
		Ω(err).NotTo(HaveOccurred())

		resp := test_util.NewResponse(http.StatusOK)
		x.WriteResponse(resp)
		x.Close()
		done <- true
	}

	responseWithJSessionID := func(x *test_util.HttpConn) {
		_, err := http.ReadRequest(x.Reader)
		Ω(err).NotTo(HaveOccurred())

		resp := test_util.NewResponse(http.StatusOK)
		resp.Header.Add("Set-Cookie", jSessionIdCookie.String())
		x.WriteResponse(resp)
		x.Close()
		done <- true
	}

	BeforeEach(func() {
		done = make(chan bool)
		conf.SecureCookies = false

		jSessionIdCookie = &http.Cookie{
			Name:    proxy.StickyCookieKey,
			Value:   "xxx",
			MaxAge:  1,
			Expires: time.Now(),
		}
	})

	Context("first request", func() {
		Context("when the response does not contain a JESSIONID cookie", func() {
			It("does not respond with a VCAP_ID cookie", func() {
				ln := registerHandlerWithInstanceId(r, "app", "", responseNoCookies, "my-id")
				defer ln.Close()

				x := dialProxy(proxyServer)
				req := test_util.NewRequest("GET", "/", nil)
				req.Host = "app"
				x.WriteRequest(req)

				Eventually(done).Should(Receive())

				resp, _ := x.ReadResponse()
				Ω(getCookie(proxy.VcapCookieId, resp.Cookies())).Should(BeNil())
			})
		})

		Context("when the response contains a JESSIONID cookie", func() {
			It("responds with a VCAP_ID cookie scoped to the session", func() {
				ln := registerHandlerWithInstanceId(r, "app", "", responseWithJSessionID, "my-id")
				defer ln.Close()

				x := dialProxy(proxyServer)
				req := test_util.NewRequest("GET", "/", nil)
				req.Host = "app"
				x.WriteRequest(req)

				Eventually(done).Should(Receive())

				resp, _ := x.ReadResponse()
				jsessionId := getCookie(proxy.StickyCookieKey, resp.Cookies())
				Ω(jsessionId).ShouldNot(BeNil())

				cookie := getCookie(proxy.VcapCookieId, resp.Cookies())
				Ω(cookie).ShouldNot(BeNil())
				Ω(cookie.Value).Should(Equal("my-id"))
				Ω(cookie.Secure).Should(BeFalse())
				Ω(cookie.MaxAge).Should(BeZero())
				Ω(cookie.Expires).Should(BeZero())
			})

			Context("with secure cookies enabled", func() {
				BeforeEach(func() {
					conf.SecureCookies = true
				})

				It("marks the cookie as secure only", func() {
					ln := registerHandlerWithInstanceId(r, "app", "", responseWithJSessionID, "my-id")
					defer ln.Close()

					x := dialProxy(proxyServer)
					req := test_util.NewRequest("GET", "/", nil)
					req.Host = "app"
					x.WriteRequest(req)

					Eventually(done).Should(Receive())

					resp, _ := x.ReadResponse()
					jsessionId := getCookie(proxy.StickyCookieKey, resp.Cookies())
					Ω(jsessionId).ShouldNot(BeNil())

					cookie := getCookie(proxy.VcapCookieId, resp.Cookies())
					Ω(cookie).ShouldNot(BeNil())
					Ω(cookie.Value).Should(Equal("my-id"))
					Ω(cookie.Secure).Should(BeTrue())
					Ω(cookie.MaxAge).Should(BeZero())
					Ω(cookie.Expires).Should(BeZero())
				})
			})
		})
	})

	Context("subsequent requests", func() {
		const host = "app"
		var req *http.Request

		BeforeEach(func() {
			cookie := &http.Cookie{
				Name:  proxy.VcapCookieId,
				Value: "my-id",
				Path:  "/",

				HttpOnly: true,
				Secure:   false,
			}

			req = test_util.NewRequest("GET", "/", nil)
			req.Host = host
			req.AddCookie(cookie)

			jSessionIdCookie := &http.Cookie{
				Name:  proxy.StickyCookieKey,
				Value: "xxx",
			}
			req.AddCookie(jSessionIdCookie)
		})

		Context("when the response does not contain a JESSIONID cookie", func() {
			It("does not respond with a VCAP_ID cookie", func() {
				ln := registerHandlerWithInstanceId(r, host, "", responseNoCookies, "my-id")
				defer ln.Close()

				x := dialProxy(proxyServer)

				x.WriteRequest(req)

				Eventually(done).Should(Receive())

				resp, _ := x.ReadResponse()
				Ω(getCookie(proxy.StickyCookieKey, resp.Cookies())).Should(BeNil())
				Ω(getCookie(proxy.VcapCookieId, resp.Cookies())).Should(BeNil())
			})

			Context("when the preferred server is gone", func() {
				It("updates the VCAP_ID with the new server", func() {
					ln := registerHandlerWithInstanceId(r, host, "", responseNoCookies, "other-id")
					defer ln.Close()

					x := dialProxy(proxyServer)

					x.WriteRequest(req)

					Eventually(done).Should(Receive())

					resp, _ := x.ReadResponse()
					cookie := getCookie(proxy.VcapCookieId, resp.Cookies())
					Ω(cookie).ShouldNot(BeNil())
					Ω(cookie.Value).Should(Equal("other-id"))
					Ω(cookie.Secure).Should(BeFalse())
					Ω(cookie.MaxAge).Should(BeZero())
					Ω(cookie.Expires).Should(BeZero())
				})
			})
		})

		Context("when the response contains a JESSIONID cookie", func() {
			It("responds with a VCAP_ID cookie", func() {
				ln := registerHandlerWithInstanceId(r, "app", "", responseWithJSessionID, "some-id")
				defer ln.Close()

				x := dialProxy(proxyServer)
				req := test_util.NewRequest("GET", "/", nil)
				req.Host = "app"
				x.WriteRequest(req)

				Eventually(done).Should(Receive())

				resp, _ := x.ReadResponse()
				jsessionId := getCookie(proxy.StickyCookieKey, resp.Cookies())
				Ω(jsessionId).ShouldNot(BeNil())

				cookie := getCookie(proxy.VcapCookieId, resp.Cookies())
				Ω(cookie).ShouldNot(BeNil())
				Ω(cookie.Value).Should(Equal("some-id"))
				Ω(cookie.Secure).Should(BeFalse())
				Ω(cookie.MaxAge).Should(BeZero())
				Ω(cookie.Expires).Should(BeZero())
			})

			Context("when the JSESSIONID is expired", func() {
				BeforeEach(func() {
					jSessionIdCookie.MaxAge = -1
				})

				It("expires the VCAP_ID", func() {
					ln := registerHandlerWithInstanceId(r, "app", "", responseWithJSessionID, "my-id")
					defer ln.Close()

					x := dialProxy(proxyServer)
					req := test_util.NewRequest("GET", "/", nil)
					req.Host = "app"
					x.WriteRequest(req)

					Eventually(done).Should(Receive())

					resp, _ := x.ReadResponse()
					jsessionId := getCookie(proxy.StickyCookieKey, resp.Cookies())
					Ω(jsessionId).ShouldNot(BeNil())
					Ω(jsessionId.MaxAge).Should(Equal(-1))

					cookie := getCookie(proxy.VcapCookieId, resp.Cookies())
					Ω(cookie).ShouldNot(BeNil())
					Ω(cookie.Value).Should(Equal("my-id"))
					Ω(cookie.Secure).Should(BeFalse())
					Ω(cookie.MaxAge).Should(Equal(-1))
					Ω(cookie.Expires).Should(BeZero())
				})
			})
		})
	})
})

func getCookie(name string, cookies []*http.Cookie) *http.Cookie {
	for _, c := range cookies {
		if c.Name == name {
			return c
		}
	}

	return nil
}
