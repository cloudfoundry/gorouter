package proxy_test

import (
	"net/http"
	"time"

	"code.cloudfoundry.org/gorouter/proxy"
	"code.cloudfoundry.org/gorouter/test_util"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("Session Affinity", func() {
	var done chan bool
	var jSessionIdCookie *http.Cookie

	responseNoCookies := func(x *test_util.HttpConn) {
		_, err := http.ReadRequest(x.Reader)
		Expect(err).ToNot(HaveOccurred())

		resp := test_util.NewResponse(http.StatusOK)
		x.WriteResponse(resp)
		x.Close()
		done <- true
	}

	responseWithJSessionID := func(x *test_util.HttpConn) {
		_, err := http.ReadRequest(x.Reader)
		Expect(err).ToNot(HaveOccurred())

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

	Context("context paths", func() {
		Context("when two requests have the same context paths", func() {
			It("responds with the same instance id", func() {
				ln := registerHandlerWithInstanceId(r, "app.com/path1", "", responseWithJSessionID, "instance-id-1")
				defer ln.Close()
				ln2 := registerHandlerWithInstanceId(r, "app.com/path2/context/path", "", responseWithJSessionID, "instance-id-2")
				defer ln2.Close()

				conn := dialProxy(proxyServer)
				req := test_util.NewRequest("GET", "app.com", "/path1/some/sub/path/index.html", nil)
				conn.WriteRequest(req)

				Eventually(done).Should(Receive())

				resp, _ := conn.ReadResponse()
				cookie := getCookie(proxy.VcapCookieId, resp.Cookies())
				Expect(cookie).ToNot(BeNil())
				Expect(cookie.Path).To(Equal("/path1"))
				Expect(cookie.Value).To(Equal("instance-id-1"))

				req2 := test_util.NewRequest("GET", "app.com", "/path1/other/sub/path/index.html", nil)
				conn.WriteRequest(req2)

				Eventually(done).Should(Receive())

				resp, _ = conn.ReadResponse()
				cookie = getCookie(proxy.VcapCookieId, resp.Cookies())
				Expect(cookie).ToNot(BeNil())
				Expect(cookie.Path).To(Equal("/path1"))
				Expect(cookie.Value).To(Equal("instance-id-1"))
			})
		})

		Context("when two requests have different context paths", func() {
			It("responds with different instance ids", func() {
				ln := registerHandlerWithInstanceId(r, "app.com/path1", "", responseWithJSessionID, "instance-id-1")
				defer ln.Close()
				ln2 := registerHandlerWithInstanceId(r, "app.com/path2/context/path", "", responseWithJSessionID, "instance-id-2")
				defer ln2.Close()

				conn := dialProxy(proxyServer)
				req := test_util.NewRequest("GET", "app.com", "/path1/some/sub/path/index.html", nil)
				conn.WriteRequest(req)

				Eventually(done).Should(Receive())

				resp, _ := conn.ReadResponse()
				cookie := getCookie(proxy.VcapCookieId, resp.Cookies())
				Expect(cookie).ToNot(BeNil())
				Expect(cookie.Path).To(Equal("/path1"))
				Expect(cookie.Value).To(Equal("instance-id-1"))

				req2 := test_util.NewRequest("GET", "app.com", "/path2/context/path/index.html", nil)
				conn.WriteRequest(req2)

				Eventually(done).Should(Receive())

				resp, _ = conn.ReadResponse()
				cookie = getCookie(proxy.VcapCookieId, resp.Cookies())
				Expect(cookie).ToNot(BeNil())
				Expect(cookie.Path).To(Equal("/path2/context/path"))
				Expect(cookie.Value).To(Equal("instance-id-2"))
			})
		})

		Context("when only one request has a context path", func() {
			It("responds with different instance ids", func() {
				ln := registerHandlerWithInstanceId(r, "app.com/path1", "", responseWithJSessionID, "instance-id-1")
				defer ln.Close()
				ln2 := registerHandlerWithInstanceId(r, "app.com", "", responseWithJSessionID, "instance-id-2")
				defer ln2.Close()

				conn := dialProxy(proxyServer)
				req := test_util.NewRequest("GET", "app.com", "/path1/some/sub/path/index.html", nil)
				conn.WriteRequest(req)

				Eventually(done).Should(Receive())

				resp, _ := conn.ReadResponse()
				cookie := getCookie(proxy.VcapCookieId, resp.Cookies())
				Expect(cookie).ToNot(BeNil())
				Expect(cookie.Path).To(Equal("/path1"))
				Expect(cookie.Value).To(Equal("instance-id-1"))

				req2 := test_util.NewRequest("GET", "app.com", "/path2/context/path/index.html", nil)
				conn.WriteRequest(req2)

				Eventually(done).Should(Receive())

				resp, _ = conn.ReadResponse()
				cookie = getCookie(proxy.VcapCookieId, resp.Cookies())
				Expect(cookie).ToNot(BeNil())
				Expect(cookie.Path).To(Equal("/"))
				Expect(cookie.Value).To(Equal("instance-id-2"))
			})
		})

	})

	Context("first request", func() {
		Context("when the response does not contain a JESSIONID cookie", func() {
			It("does not respond with a VCAP_ID cookie", func() {
				ln := registerHandlerWithInstanceId(r, "app", "", responseNoCookies, "my-id")
				defer ln.Close()

				x := dialProxy(proxyServer)
				req := test_util.NewRequest("GET", "app", "/", nil)
				x.WriteRequest(req)

				Eventually(done).Should(Receive())

				resp, _ := x.ReadResponse()
				Expect(getCookie(proxy.VcapCookieId, resp.Cookies())).To(BeNil())
			})
		})

		Context("when the response contains a JESSIONID cookie", func() {

			It("responds with a VCAP_ID cookie scoped to the session", func() {
				ln := registerHandlerWithInstanceId(r, "app", "", responseWithJSessionID, "my-id")
				defer ln.Close()

				x := dialProxy(proxyServer)
				req := test_util.NewRequest("GET", "app", "/", nil)
				x.WriteRequest(req)

				Eventually(done).Should(Receive())

				resp, _ := x.ReadResponse()
				jsessionId := getCookie(proxy.StickyCookieKey, resp.Cookies())
				Expect(jsessionId).ToNot(BeNil())

				cookie := getCookie(proxy.VcapCookieId, resp.Cookies())
				Expect(cookie).ToNot(BeNil())
				Expect(cookie.Value).To(Equal("my-id"))
				Expect(cookie.Secure).To(BeFalse())
				Expect(cookie.MaxAge).To(BeZero())
				Expect(cookie.Expires).To(BeZero())
			})

			Context("and JESSIONID cookie is set to Secure", func() {

				BeforeEach(func() {
					jSessionIdCookie.Secure = true
				})

				It("responds with a VCAP_ID cookie that is also Secure ", func() {
					ln := registerHandlerWithInstanceId(r, "app", "", responseWithJSessionID, "my-id")
					defer ln.Close()

					x := dialProxy(proxyServer)
					req := test_util.NewRequest("GET", "app", "/", nil)
					x.WriteRequest(req)

					Eventually(done).Should(Receive())

					resp, _ := x.ReadResponse()
					jsessionId := getCookie(proxy.StickyCookieKey, resp.Cookies())
					Expect(jsessionId).ToNot(BeNil())

					cookie := getCookie(proxy.VcapCookieId, resp.Cookies())
					Expect(cookie).ToNot(BeNil())
					Expect(cookie.Value).To(Equal("my-id"))
					Expect(cookie.Secure).To(BeTrue())
				})
			})

			Context("with secure cookies enabled and non-secure cookie", func() {
				BeforeEach(func() {
					conf.SecureCookies = true
					jSessionIdCookie.Secure = false
				})

				It("marks the cookie as secure only", func() {
					ln := registerHandlerWithInstanceId(r, "app", "", responseWithJSessionID, "my-id")
					defer ln.Close()

					x := dialProxy(proxyServer)
					req := test_util.NewRequest("GET", "app", "/", nil)
					x.WriteRequest(req)

					Eventually(done).Should(Receive())

					resp, _ := x.ReadResponse()
					jsessionId := getCookie(proxy.StickyCookieKey, resp.Cookies())
					Expect(jsessionId).ToNot(BeNil())

					cookie := getCookie(proxy.VcapCookieId, resp.Cookies())
					Expect(cookie).ToNot(BeNil())
					Expect(cookie.Value).To(Equal("my-id"))
					Expect(cookie.Secure).To(BeTrue())
					Expect(cookie.MaxAge).To(BeZero())
					Expect(cookie.Expires).To(BeZero())
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

			req = test_util.NewRequest("GET", host, "/", nil)
			req.AddCookie(cookie)

			jSessionIdCookie = &http.Cookie{
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
				Expect(getCookie(proxy.StickyCookieKey, resp.Cookies())).To(BeNil())
				Expect(getCookie(proxy.VcapCookieId, resp.Cookies())).To(BeNil())
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
					Expect(cookie).ToNot(BeNil())
					Expect(cookie.Value).To(Equal("other-id"))
					Expect(cookie.Secure).To(BeFalse())
					Expect(cookie.MaxAge).To(BeZero())
					Expect(cookie.Expires).To(BeZero())
				})
			})
		})

		Context("when the response contains a JESSIONID cookie", func() {
			It("responds with a VCAP_ID cookie", func() {
				ln := registerHandlerWithInstanceId(r, "app", "", responseWithJSessionID, "some-id")
				defer ln.Close()

				x := dialProxy(proxyServer)
				req := test_util.NewRequest("GET", "app", "/", nil)
				x.WriteRequest(req)

				Eventually(done).Should(Receive())

				resp, _ := x.ReadResponse()
				jsessionId := getCookie(proxy.StickyCookieKey, resp.Cookies())
				Expect(jsessionId).ToNot(BeNil())

				cookie := getCookie(proxy.VcapCookieId, resp.Cookies())
				Expect(cookie).ToNot(BeNil())
				Expect(cookie.Value).To(Equal("some-id"))
				Expect(cookie.Secure).To(BeFalse())
				Expect(cookie.MaxAge).To(BeZero())
				Expect(cookie.Expires).To(BeZero())
			})

			Context("when the JSESSIONID is expired", func() {
				BeforeEach(func() {
					jSessionIdCookie.MaxAge = -1
				})

				It("expires the VCAP_ID", func() {
					ln := registerHandlerWithInstanceId(r, "app", "", responseWithJSessionID, "my-id")
					defer ln.Close()

					x := dialProxy(proxyServer)
					req := test_util.NewRequest("GET", "app", "/", nil)
					x.WriteRequest(req)

					Eventually(done).Should(Receive())

					resp, _ := x.ReadResponse()
					jsessionId := getCookie(proxy.StickyCookieKey, resp.Cookies())
					Expect(jsessionId).ToNot(BeNil())
					Expect(jsessionId.MaxAge).To(Equal(-1))

					cookie := getCookie(proxy.VcapCookieId, resp.Cookies())
					Expect(cookie).ToNot(BeNil())
					Expect(cookie.Value).To(Equal("my-id"))
					Expect(cookie.Secure).To(BeFalse())
					Expect(cookie.MaxAge).To(Equal(-1))
					Expect(cookie.Expires).To(BeZero())
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
