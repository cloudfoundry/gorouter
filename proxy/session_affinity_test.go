package proxy_test

import (
	"net/http"
	"time"

	"code.cloudfoundry.org/gorouter/proxy"
	"code.cloudfoundry.org/gorouter/test_util"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

const StickyCookieKey = "JSESSIONID"

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
			Name:   StickyCookieKey,
			Value:  "xxx",
			MaxAge: 1,
		}
	})

	Context("context paths", func() {
		Context("when two requests have the same context paths", func() {
			It("responds with the same instance id", func() {
				ln := test_util.RegisterHandler(r, "app.com/path1", responseWithJSessionID, test_util.RegisterConfig{InstanceId: "instance-id-1"})
				defer ln.Close()
				ln2 := test_util.RegisterHandler(r, "app.com/path2/context/path", responseWithJSessionID, test_util.RegisterConfig{InstanceId: "instance-id-2"})
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
				ln := test_util.RegisterHandler(r, "app.com/path1", responseWithJSessionID, test_util.RegisterConfig{InstanceId: "instance-id-1"})
				defer ln.Close()
				ln2 := test_util.RegisterHandler(r, "app.com/path2/context/path", responseWithJSessionID, test_util.RegisterConfig{InstanceId: "instance-id-2"})
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
				ln := test_util.RegisterHandler(r, "app.com/path1", responseWithJSessionID, test_util.RegisterConfig{InstanceId: "instance-id-1"})
				defer ln.Close()
				ln2 := test_util.RegisterHandler(r, "app.com", responseWithJSessionID, test_util.RegisterConfig{InstanceId: "instance-id-2"})
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
		Context("when the response does not contain a JSESSIONID cookie", func() {
			It("does not respond with a VCAP_ID cookie", func() {
				ln := test_util.RegisterHandler(r, "app", responseNoCookies, test_util.RegisterConfig{InstanceId: "my-id"})
				defer ln.Close()

				x := dialProxy(proxyServer)
				req := test_util.NewRequest("GET", "app", "/", nil)
				x.WriteRequest(req)

				Eventually(done).Should(Receive())

				resp, _ := x.ReadResponse()
				Expect(getCookie(proxy.VcapCookieId, resp.Cookies())).To(BeNil())
			})
		})

		Context("when the response contains a JSESSIONID cookie", func() {

			It("responds with a VCAP_ID cookie scoped to the session", func() {
				ln := test_util.RegisterHandler(r, "app", responseWithJSessionID, test_util.RegisterConfig{InstanceId: "my-id"})
				defer ln.Close()

				x := dialProxy(proxyServer)
				req := test_util.NewRequest("GET", "app", "/", nil)
				x.WriteRequest(req)

				Eventually(done).Should(Receive())

				resp, _ := x.ReadResponse()
				jsessionId := getCookie(StickyCookieKey, resp.Cookies())
				Expect(jsessionId).ToNot(BeNil())

				cookie := getCookie(proxy.VcapCookieId, resp.Cookies())
				Expect(cookie).ToNot(BeNil())
				Expect(cookie.Value).To(Equal("my-id"))
				Expect(cookie.Secure).To(BeFalse())
				Expect(cookie.MaxAge).To(BeZero())
				Expect(cookie.Expires).To(BeZero())
			})

			Context("and the JSESSIONID cookie has an expiry date", func() {
				var expiry time.Time

				BeforeEach(func() {
					expiry, _ = time.Parse(time.RFC3339, "2000-11-01T10:01:01")
					jSessionIdCookie.Expires = expiry
				})

				It("responds with a VCAP_ID cookie that has the same expiry", func() {
					ln := test_util.RegisterHandler(r, "app", responseWithJSessionID, test_util.RegisterConfig{InstanceId: "my-id"})
					defer ln.Close()

					x := dialProxy(proxyServer)
					req := test_util.NewRequest("GET", "app", "/", nil)
					x.WriteRequest(req)

					Eventually(done).Should(Receive())

					resp, _ := x.ReadResponse()
					jsessionId := getCookie(StickyCookieKey, resp.Cookies())
					Expect(jsessionId).ToNot(BeNil())

					cookie := getCookie(proxy.VcapCookieId, resp.Cookies())
					Expect(cookie).ToNot(BeNil())
					Expect(cookie.Expires).To(Equal(expiry))
				})
			})

			Context("and JSESSIONID cookie is set to Secure", func() {

				BeforeEach(func() {
					jSessionIdCookie.Secure = true
				})

				It("responds with a VCAP_ID cookie that is also Secure ", func() {
					ln := test_util.RegisterHandler(r, "app", responseWithJSessionID, test_util.RegisterConfig{InstanceId: "my-id"})
					defer ln.Close()

					x := dialProxy(proxyServer)
					req := test_util.NewRequest("GET", "app", "/", nil)
					x.WriteRequest(req)

					Eventually(done).Should(Receive())

					resp, _ := x.ReadResponse()
					jsessionId := getCookie(StickyCookieKey, resp.Cookies())
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
					ln := test_util.RegisterHandler(r, "app", responseWithJSessionID, test_util.RegisterConfig{InstanceId: "my-id"})
					defer ln.Close()

					x := dialProxy(proxyServer)
					req := test_util.NewRequest("GET", "app", "/", nil)
					x.WriteRequest(req)

					Eventually(done).Should(Receive())

					resp, _ := x.ReadResponse()
					jsessionId := getCookie(StickyCookieKey, resp.Cookies())
					Expect(jsessionId).ToNot(BeNil())

					cookie := getCookie(proxy.VcapCookieId, resp.Cookies())
					Expect(cookie).ToNot(BeNil())
					Expect(cookie.Value).To(Equal("my-id"))
					Expect(cookie.Secure).To(BeTrue())
					Expect(cookie.MaxAge).To(BeZero())
					Expect(cookie.Expires).To(BeZero())
				})
			})

			Context("and JSESSIONID cookie has SameSite attribute set", func() {

				BeforeEach(func() {
					jSessionIdCookie.SameSite = http.SameSiteStrictMode
				})

				It("responds with a VCAP_ID cookie that has the same SameSite attribute", func() {
					ln := test_util.RegisterHandler(r, "app", responseWithJSessionID, test_util.RegisterConfig{InstanceId: "my-id"})
					defer ln.Close()

					x := dialProxy(proxyServer)
					req := test_util.NewRequest("GET", "app", "/", nil)
					x.WriteRequest(req)

					Eventually(done).Should(Receive())

					resp, _ := x.ReadResponse()
					jsessionId := getCookie(StickyCookieKey, resp.Cookies())
					Expect(jsessionId).ToNot(BeNil())

					cookie := getCookie(proxy.VcapCookieId, resp.Cookies())
					Expect(cookie).ToNot(BeNil())
					Expect(cookie.Value).To(Equal("my-id"))
					Expect(cookie.SameSite).To(Equal(http.SameSiteStrictMode))
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
				Name:  StickyCookieKey,
				Value: "xxx",
			}
			req.AddCookie(jSessionIdCookie)
		})

		Context("when the response does not contain a JSESSIONID cookie", func() {
			It("does not respond with a VCAP_ID cookie", func() {
				ln := test_util.RegisterHandler(r, host, responseNoCookies, test_util.RegisterConfig{InstanceId: "my-id"})
				defer ln.Close()

				x := dialProxy(proxyServer)

				x.WriteRequest(req)

				Eventually(done).Should(Receive())

				resp, _ := x.ReadResponse()
				Expect(getCookie(StickyCookieKey, resp.Cookies())).To(BeNil())
				Expect(getCookie(proxy.VcapCookieId, resp.Cookies())).To(BeNil())
			})

			Context("when the preferred server is gone", func() {
				It("updates the VCAP_ID with the new server", func() {
					ln := test_util.RegisterHandler(r, host, responseNoCookies, test_util.RegisterConfig{InstanceId: "other-id"})
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

		Context("when the response contains a JSESSIONID cookie", func() {
			It("responds with a VCAP_ID cookie", func() {
				ln := test_util.RegisterHandler(r, "app", responseWithJSessionID, test_util.RegisterConfig{InstanceId: "some-id"})
				defer ln.Close()

				x := dialProxy(proxyServer)
				req := test_util.NewRequest("GET", "app", "/", nil)
				x.WriteRequest(req)

				Eventually(done).Should(Receive())

				resp, _ := x.ReadResponse()
				jsessionId := getCookie(StickyCookieKey, resp.Cookies())
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
					ln := test_util.RegisterHandler(r, "app", responseWithJSessionID, test_util.RegisterConfig{InstanceId: "my-id"})
					defer ln.Close()

					x := dialProxy(proxyServer)
					req := test_util.NewRequest("GET", "app", "/", nil)
					x.WriteRequest(req)

					Eventually(done).Should(Receive())

					resp, _ := x.ReadResponse()
					jsessionId := getCookie(StickyCookieKey, resp.Cookies())
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
