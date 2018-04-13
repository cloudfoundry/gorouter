package proxy_test

import (
	"bytes"
	"crypto/tls"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	router_http "code.cloudfoundry.org/gorouter/common/http"
	"code.cloudfoundry.org/gorouter/config"
	"code.cloudfoundry.org/gorouter/handlers"
	"code.cloudfoundry.org/gorouter/route"
	"code.cloudfoundry.org/gorouter/test_util"
	"github.com/cloudfoundry/dropsonde/factories"
	"github.com/cloudfoundry/sonde-go/events"
	uuid "github.com/nu7hatch/gouuid"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("Proxy", func() {
	Describe("Supported HTTP Protocol Versions", func() {
		It("responds to http/1.0", func() {
			ln := test_util.RegisterHandler(r, "test", func(conn *test_util.HttpConn) {
				conn.CheckLine("GET / HTTP/1.1")

				conn.WriteResponse(test_util.NewResponse(http.StatusOK))
			})
			defer ln.Close()

			conn := dialProxy(proxyServer)

			conn.WriteLines([]string{
				"GET / HTTP/1.0",
				"Host: test",
			})

			conn.CheckLine("HTTP/1.0 200 OK")
		})

		It("responds to HTTP/1.1", func() {
			ln := test_util.RegisterHandler(r, "test", func(conn *test_util.HttpConn) {
				conn.CheckLine("GET / HTTP/1.1")

				conn.WriteResponse(test_util.NewResponse(http.StatusOK))
			})
			defer ln.Close()

			conn := dialProxy(proxyServer)

			conn.WriteLines([]string{
				"GET / HTTP/1.1",
				"Host: test",
			})

			resp, _ := conn.ReadResponse()
			Expect(resp.StatusCode).To(Equal(http.StatusOK))
		})

		It("does not respond to unsupported HTTP versions", func() {
			conn := dialProxy(proxyServer)

			conn.WriteLines([]string{
				"GET / HTTP/1.5",
				"Host: test",
			})

			resp, _ := conn.ReadResponse()
			Expect(resp.StatusCode).To(Equal(http.StatusBadRequest))
		})
	})

	Describe("URL Handling", func() {
		It("responds transparently to a trailing slash versus no trailing slash", func() {
			lnWithoutSlash := test_util.RegisterHandler(r, "test/my%20path/your_path", func(conn *test_util.HttpConn) {
				conn.CheckLine("GET /my%20path/your_path/ HTTP/1.1")

				conn.WriteResponse(test_util.NewResponse(http.StatusOK))
			})
			defer lnWithoutSlash.Close()

			lnWithSlash := test_util.RegisterHandler(r, "test/another-path/your_path/", func(conn *test_util.HttpConn) {
				conn.CheckLine("GET /another-path/your_path HTTP/1.1")

				conn.WriteResponse(test_util.NewResponse(http.StatusOK))
			})
			defer lnWithSlash.Close()

			conn := dialProxy(proxyServer)
			y := dialProxy(proxyServer)

			req := test_util.NewRequest("GET", "test", "/my%20path/your_path/", nil)
			conn.WriteRequest(req)

			resp, _ := conn.ReadResponse()
			Expect(resp.StatusCode).To(Equal(http.StatusOK))

			req = test_util.NewRequest("GET", "test", "/another-path/your_path", nil)
			y.WriteRequest(req)

			resp, _ = y.ReadResponse()
			Expect(resp.StatusCode).To(Equal(http.StatusOK))
		})

		It("Does not append ? to the request", func() {
			ln := test_util.RegisterHandler(r, "test/?", func(conn *test_util.HttpConn) {
				conn.CheckLine("GET /? HTTP/1.1")

				conn.WriteResponse(test_util.NewResponse(http.StatusOK))
			})
			defer ln.Close()

			x := dialProxy(proxyServer)

			req := test_util.NewRequest("GET", "test", "/?", nil)
			x.WriteRequest(req)
			resp, _ := x.ReadResponse()
			Expect(resp.StatusCode).To(Equal(200))
		})

		It("responds to http/1.0 with path", func() {
			ln := test_util.RegisterHandler(r, "test/my_path", func(conn *test_util.HttpConn) {
				conn.CheckLine("GET /my_path HTTP/1.1")

				conn.WriteResponse(test_util.NewResponse(http.StatusOK))
			})
			defer ln.Close()

			conn := dialProxy(proxyServer)

			conn.WriteLines([]string{
				"GET /my_path HTTP/1.0",
				"Host: test",
			})

			conn.CheckLine("HTTP/1.0 200 OK")
		})

		It("responds to http/1.0 with path/path", func() {
			ln := test_util.RegisterHandler(r, "test/my%20path/your_path", func(conn *test_util.HttpConn) {
				conn.CheckLine("GET /my%20path/your_path HTTP/1.1")

				conn.WriteResponse(test_util.NewResponse(http.StatusOK))
			})
			defer ln.Close()

			conn := dialProxy(proxyServer)

			conn.WriteLines([]string{
				"GET /my%20path/your_path HTTP/1.0",
				"Host: test",
			})

			conn.CheckLine("HTTP/1.0 200 OK")
		})

		It("responds to HTTP/1.1 with absolute-form request target", func() {
			ln := test_util.RegisterHandler(r, "test.io", func(conn *test_util.HttpConn) {
				conn.CheckLine("GET http://test.io/ HTTP/1.1")

				conn.WriteResponse(test_util.NewResponse(http.StatusOK))
			})
			defer ln.Close()

			conn := dialProxy(proxyServer)

			conn.WriteLines([]string{
				"GET http://test.io/ HTTP/1.1",
				"Host: test.io",
			})

			resp, _ := conn.ReadResponse()
			Expect(resp.StatusCode).To(Equal(http.StatusOK))
		})

		It("responds to http/1.1 with absolute-form request that has encoded characters in the path", func() {
			ln := test_util.RegisterHandler(r, "test.io/my%20path/your_path", func(conn *test_util.HttpConn) {
				conn.CheckLine("GET http://test.io/my%20path/your_path HTTP/1.1")

				conn.WriteResponse(test_util.NewResponse(http.StatusOK))
			})
			defer ln.Close()

			conn := dialProxy(proxyServer)

			conn.WriteLines([]string{
				"GET http://test.io/my%20path/your_path HTTP/1.1",
				"Host: test.io",
			})

			conn.CheckLine("HTTP/1.1 200 OK")
		})

		It("maintains percent-encoded values in URLs", func() {
			shouldEcho("/abc%2b%2f%25%20%22%3F%5Edef", "/abc%2b%2f%25%20%22%3F%5Edef") // +, /, %, <space>, ", £, ^
		})

		It("does not encode reserved characters in URLs", func() {
			rfc3986_reserved_characters := "!*'();:@&=+$,/?#[]"
			shouldEcho("/"+rfc3986_reserved_characters, "/"+rfc3986_reserved_characters)
		})

		It("maintains encoding of percent-encoded reserved characters", func() {
			encoded_reserved_characters := "%21%27%28%29%3B%3A%40%26%3D%2B%24%2C%2F%3F%23%5B%5D"
			shouldEcho("/"+encoded_reserved_characters, "/"+encoded_reserved_characters)
		})

		It("does not encode unreserved characters in URLs", func() {
			shouldEcho("/abc123_.~def", "/abc123_.~def")
		})

		It("does not percent-encode special characters in URLs (they came in like this, they go out like this)", func() {
			shouldEcho("/abc\"£^def", "/abc\"£^def")
		})

		It("handles requests with encoded query strings", func() {
			queryString := strings.Join([]string{"a=b", url.QueryEscape("b= bc "), url.QueryEscape("c=d&e")}, "&")
			shouldEcho("/test?a=b&b%3D+bc+&c%3Dd%26e", "/test?"+queryString)
		})

		It("treats double slashes in request URI as an absolute-form request target", func() {
			ln := test_util.RegisterHandler(r, "test.io", func(conn *test_util.HttpConn) {
				conn.CheckLine("GET http://test.io//something.io HTTP/1.1")
				conn.WriteResponse(test_util.NewResponse(http.StatusOK))
			})
			defer ln.Close()

			req, err := http.NewRequest("GET", "http://test.io//something.io", nil)
			Expect(err).ToNot(HaveOccurred())

			conn := dialProxy(proxyServer)
			conn.WriteRequest(req)

			resp, _ := conn.ReadResponse()
			Expect(resp.StatusCode).To(Equal(http.StatusOK))
		})

		It("handles double slashes in an absolute-form request target correctly", func() {
			ln := test_util.RegisterHandler(r, "test.io", func(conn *test_util.HttpConn) {
				conn.CheckLine("GET http://test.io//something.io?q=something HTTP/1.1")
				conn.WriteResponse(test_util.NewResponse(http.StatusOK))
			})
			defer ln.Close()

			conn := dialProxy(proxyServer)
			conn.WriteLines([]string{
				"GET http://test.io//something.io?q=something HTTP/1.1",
				"Host: test.io",
			})

			resp, _ := conn.ReadResponse()
			Expect(resp.StatusCode).To(Equal(http.StatusOK))
		})
	})

	Describe("proxying the request headers", func() {
		var (
			receivedHeaders  chan http.Header
			extraRegisterCfg []test_util.RegisterConfig
			fakeResponseBody string
			fakeResponseCode int
			ln               net.Listener
			req              *http.Request
		)

		BeforeEach(func() {
			receivedHeaders = make(chan http.Header)
			extraRegisterCfg = nil
			fakeResponseBody = ""
			fakeResponseCode = http.StatusOK
		})

		JustBeforeEach(func() {
			ln = test_util.RegisterHandler(r, "app", func(conn *test_util.HttpConn) {
				tmpReq, err := http.ReadRequest(conn.Reader)
				Expect(err).NotTo(HaveOccurred())

				resp := test_util.NewResponse(fakeResponseCode)
				conn.WriteResponse(resp)
				if fakeResponseBody != "" {
					conn.WriteLine(fakeResponseBody)
				}
				conn.Close()

				receivedHeaders <- tmpReq.Header
			}, extraRegisterCfg...)

			req = test_util.NewRequest("GET", "app", "/", nil)
		})

		AfterEach(func() {
			ln.Close()
		})

		// proxies request, returns the value of the X-Forwarded-Proto header
		getProxiedHeaders := func(req *http.Request) http.Header {
			conn := dialProxy(proxyServer)
			conn.WriteRequest(req)
			defer conn.ReadResponse()

			var headers http.Header
			Eventually(receivedHeaders).Should(Receive(&headers))
			return headers
		}

		Describe("X-Forwarded-For", func() {
			It("sets X-Forwarded-For", func() {
				Expect(getProxiedHeaders(req).Get("X-Forwarded-For")).To(Equal("127.0.0.1"))
			})
			Context("when the header is already set", func() {
				It("appends the client IP", func() {
					req.Header.Add("X-Forwarded-For", "1.2.3.4")
					Expect(getProxiedHeaders(req).Get("X-Forwarded-For")).To(Equal("1.2.3.4, 127.0.0.1"))
				})
			})
		})

		Describe("X-Request-Start", func() {
			It("appends X-Request-Start", func() {
				Expect(getProxiedHeaders(req).Get("X-Request-Start")).To(MatchRegexp("^\\d{10}\\d{3}$")) // unix timestamp millis
			})

			Context("when the header is already set", func() {
				It("does not modify the header", func() {
					req.Header.Add("X-Request-Start", "") // impl cannot just check for empty string
					req.Header.Add("X-Request-Start", "user-set2")
					Expect(getProxiedHeaders(req)["X-Request-Start"]).To(Equal([]string{"", "user-set2"}))
				})
			})
		})

		Describe("X-CF-InstanceID", func() {
			Context("when the instance is registered with an instance id", func() {
				BeforeEach(func() {
					extraRegisterCfg = []test_util.RegisterConfig{{InstanceId: "fake-instance-id"}}
				})
				It("sets the X-CF-InstanceID header", func() {
					Expect(getProxiedHeaders(req).Get(router_http.CfInstanceIdHeader)).To(Equal("fake-instance-id"))
				})
			})

			Context("when the instance is not registered with an explicit instance id", func() {
				It("sets the X-CF-InstanceID header with the backend host:port", func() {
					Expect(getProxiedHeaders(req).Get(router_http.CfInstanceIdHeader)).To(MatchRegexp(`^\d+(\.\d+){3}:\d+$`))
				})
			})
		})

		Describe("Content-type", func() {
			It("does not set the Content-Type header", func() {
				Expect(getProxiedHeaders(req)).NotTo(HaveKey("Content-Type"))
			})

			Context("when the response body is XML", func() {
				BeforeEach(func() {
					fakeResponseBody = "<?xml version=\"1.0\" encoding=\"UTF-8\"?>"
				})
				It("still does not set the Content-Type header", func() {
					Expect(getProxiedHeaders(req)).NotTo(HaveKey("Content-Type"))
				})
			})

			Context("when the response code is 204", func() {
				BeforeEach(func() {
					fakeResponseCode = http.StatusNoContent
				})
				It("still does not set the Content-Type header", func() {
					Expect(getProxiedHeaders(req)).NotTo(HaveKey("Content-Type"))
				})
			})
		})

		Describe("X-Forwarded-Client-Cert", func() {
			Context("when gorouter is configured with ForwardedClientCert == sanitize_set", func() {
				BeforeEach(func() {
					conf.ForwardedClientCert = config.SANITIZE_SET
				})
				It("removes xfcc header", func() {
					req.Header.Add("X-Forwarded-Client-Cert", "foo")
					req.Header.Add("X-Forwarded-Client-Cert", "bar")
					Expect(getProxiedHeaders(req).Get("X-Forwarded-Client-Cert")).To(BeEmpty())
				})
			})

			Context("when ForwardedClientCert is set to forward but the request is not mTLS", func() {
				BeforeEach(func() {
					conf.ForwardedClientCert = config.FORWARD
				})
				It("removes xfcc header", func() {
					req.Header.Add("X-Forwarded-Client-Cert", "foo")
					req.Header.Add("X-Forwarded-Client-Cert", "bar")
					Expect(getProxiedHeaders(req).Get("X-Forwarded-Client-Cert")).To(BeEmpty())
				})
			})

			Context("when ForwardedClientCert is set to always_forward", func() {
				BeforeEach(func() {
					conf.ForwardedClientCert = config.ALWAYS_FORWARD
				})
				It("leaves the xfcc header intact", func() {
					req.Header.Add("X-Forwarded-Client-Cert", "foo")
					req.Header.Add("X-Forwarded-Client-Cert", "bar")
					Expect(getProxiedHeaders(req)).To(HaveKeyWithValue("X-Forwarded-Client-Cert", []string{"foo", "bar"}))
				})
			})
		})
	})

	Describe("Response Handling", func() {
		It("trace headers added on correct TraceKey", func() {
			ln := test_util.RegisterHandler(r, "trace-test", func(conn *test_util.HttpConn) {
				_, err := http.ReadRequest(conn.Reader)
				Expect(err).NotTo(HaveOccurred())

				resp := test_util.NewResponse(http.StatusOK)
				conn.WriteResponse(resp)
				conn.Close()
			})
			defer ln.Close()

			conn := dialProxy(proxyServer)

			req := test_util.NewRequest("GET", "trace-test", "/", nil)
			req.Header.Set(router_http.VcapTraceHeader, "my_trace_key")
			conn.WriteRequest(req)

			resp, _ := conn.ReadResponse()
			Expect(resp.StatusCode).To(Equal(http.StatusOK))
			Expect(resp.Header.Get(router_http.VcapBackendHeader)).To(Equal(ln.Addr().String()))
			Expect(resp.Header.Get(router_http.CfRouteEndpointHeader)).To(Equal(ln.Addr().String()))
			Expect(resp.Header.Get(router_http.VcapRouterHeader)).To(Equal(conf.Ip))
		})

		It("trace headers not added on incorrect TraceKey", func() {
			ln := test_util.RegisterHandler(r, "trace-test", func(conn *test_util.HttpConn) {
				_, err := http.ReadRequest(conn.Reader)
				Expect(err).NotTo(HaveOccurred())

				resp := test_util.NewResponse(http.StatusOK)
				conn.WriteResponse(resp)
				conn.Close()
			})
			defer ln.Close()

			conn := dialProxy(proxyServer)

			req := test_util.NewRequest("GET", "trace-test", "/", nil)
			req.Header.Set(router_http.VcapTraceHeader, "a_bad_trace_key")
			conn.WriteRequest(req)

			resp, _ := conn.ReadResponse()
			Expect(resp.Header.Get(router_http.VcapBackendHeader)).To(Equal(""))
			Expect(resp.Header.Get(router_http.CfRouteEndpointHeader)).To(Equal(""))
			Expect(resp.Header.Get(router_http.VcapRouterHeader)).To(Equal(""))
		})

		It("adds X-Vcap-Request-Id if it doesn't already exist in the response", func() {
			ln := test_util.RegisterHandler(r, "vcap-id-test", func(conn *test_util.HttpConn) {
				_, err := http.ReadRequest(conn.Reader)
				Expect(err).NotTo(HaveOccurred())

				resp := test_util.NewResponse(http.StatusOK)
				conn.WriteResponse(resp)
				conn.Close()
			})
			defer ln.Close()

			conn := dialProxy(proxyServer)

			req := test_util.NewRequest("GET", "vcap-id-test", "/", nil)
			conn.WriteRequest(req)

			resp, _ := conn.ReadResponse()
			Expect(resp.StatusCode).To(Equal(http.StatusOK))
			Expect(resp.Header.Get(handlers.VcapRequestIdHeader)).ToNot(BeEmpty())
		})

		It("does not adds X-Vcap-Request-Id if it already exists in the response", func() {
			ln := test_util.RegisterHandler(r, "vcap-id-test", func(conn *test_util.HttpConn) {
				_, err := http.ReadRequest(conn.Reader)
				Expect(err).NotTo(HaveOccurred())

				resp := test_util.NewResponse(http.StatusOK)
				resp.Header.Set(handlers.VcapRequestIdHeader, "foobar")
				conn.WriteResponse(resp)
				conn.Close()
			})
			defer ln.Close()

			conn := dialProxy(proxyServer)

			req := test_util.NewRequest("GET", "vcap-id-test", "/", nil)
			conn.WriteRequest(req)

			resp, _ := conn.ReadResponse()
			Expect(resp.StatusCode).To(Equal(http.StatusOK))
			Expect(resp.Header.Get(handlers.VcapRequestIdHeader)).To(Equal("foobar"))
		})

		It("Status No Content returns no Transfer Encoding response header", func() {
			ln := test_util.RegisterHandler(r, "not-modified", func(conn *test_util.HttpConn) {
				_, err := http.ReadRequest(conn.Reader)
				Expect(err).NotTo(HaveOccurred())

				resp := test_util.NewResponse(http.StatusNoContent)
				resp.Header.Set("Connection", "close")
				conn.WriteResponse(resp)
				conn.Close()
			})
			defer ln.Close()

			conn := dialProxy(proxyServer)

			req := test_util.NewRequest("GET", "not-modified", "/", nil)

			req.Header.Set("Connection", "close")
			conn.WriteRequest(req)

			resp, _ := conn.ReadResponse()
			Expect(resp.StatusCode).To(Equal(http.StatusNoContent))
			Expect(resp.TransferEncoding).To(BeNil())
		})

		It("transfers chunked encodings", func() {
			ln := test_util.RegisterHandler(r, "chunk", func(conn *test_util.HttpConn) {
				r, w := io.Pipe()

				// Write 3 times on a 100ms interval
				go func() {
					t := time.NewTicker(100 * time.Millisecond)
					defer t.Stop()
					defer w.Close()

					for i := 0; i < 3; i++ {
						<-t.C
						_, err := w.Write([]byte("hello"))
						Expect(err).NotTo(HaveOccurred())
					}
				}()

				_, err := http.ReadRequest(conn.Reader)
				Expect(err).NotTo(HaveOccurred())

				resp := test_util.NewResponse(http.StatusOK)
				resp.TransferEncoding = []string{"chunked"}
				resp.Body = r
				resp.Write(conn)
			})
			defer ln.Close()

			conn := dialProxy(proxyServer)

			req := test_util.NewRequest("GET", "chunk", "/", nil)

			err := req.Write(conn)
			Expect(err).NotTo(HaveOccurred())

			resp, err := http.ReadResponse(conn.Reader, &http.Request{})
			Expect(err).NotTo(HaveOccurred())

			Expect(resp.StatusCode).To(Equal(http.StatusOK))
			Expect(resp.TransferEncoding).To(Equal([]string{"chunked"}))

			// Expect 3 individual reads to complete
			b := make([]byte, 5)
			for i := 0; i < 3; i++ {
				n, err := resp.Body.Read(b[0:])
				if err != nil {
					Expect(err).To(Equal(io.EOF))
				}
				Expect(n).To(Equal(5))
				Expect(string(b[0:n])).To(Equal("hello"))
			}
		})

		It("disables compression", func() {
			ln := test_util.RegisterHandler(r, "remote", func(conn *test_util.HttpConn) {
				request, _ := http.ReadRequest(conn.Reader)
				encoding := request.Header["Accept-Encoding"]
				var resp *http.Response
				if len(encoding) != 0 {
					resp = test_util.NewResponse(http.StatusInternalServerError)
				} else {
					resp = test_util.NewResponse(http.StatusOK)
				}
				conn.WriteResponse(resp)
				conn.Close()
			})
			defer ln.Close()

			conn := dialProxy(proxyServer)

			req := test_util.NewRequest("GET", "remote", "/", nil)
			conn.WriteRequest(req)
			resp, _ := conn.ReadResponse()
			Expect(resp.StatusCode).To(Equal(http.StatusOK))
		})
	})

	Describe("Backend Connection Handling", func() {
		Context("when max conn per backend is set to > 0 ", func() {
			BeforeEach(func() {
				conf.Backends.MaxConns = 2
			})

			It("responds with 503 after conn limit is reached ", func() {
				ln := test_util.RegisterHandler(r, "sleep", func(x *test_util.HttpConn) {
					defer GinkgoRecover()
					_, err := http.ReadRequest(x.Reader)
					Expect(err).NotTo(HaveOccurred())
					time.Sleep(50 * time.Millisecond)
					resp := test_util.NewResponse(http.StatusOK)
					x.WriteResponse(resp)
					x.WriteLine("hello from server after sleeping")
					x.Close()
				})
				defer ln.Close()

				var wg sync.WaitGroup
				var badGatewayCount int32

				for i := 0; i < 3; i++ {
					wg.Add(1)
					go func() {
						defer wg.Done()
						defer GinkgoRecover()

						x := dialProxy(proxyServer)
						defer x.Close()

						req := test_util.NewRequest("GET", "sleep", "/", nil)
						req.Host = "sleep"

						x.WriteRequest(req)
						resp, _ := x.ReadResponse()
						if resp.StatusCode == http.StatusServiceUnavailable {
							atomic.AddInt32(&badGatewayCount, 1)
						} else if resp.StatusCode != http.StatusOK {
							Fail(fmt.Sprintf("Expected resp to return 200 or 503, got %d", resp.StatusCode))
						}
					}()
					time.Sleep(10 * time.Millisecond)
				}
				wg.Wait()
				Expect(atomic.LoadInt32(&badGatewayCount)).To(Equal(int32(1)))
			})
		})

		It("request terminates with slow response", func() {
			ln := test_util.RegisterHandler(r, "slow-app", func(conn *test_util.HttpConn) {
				_, err := http.ReadRequest(conn.Reader)
				Expect(err).NotTo(HaveOccurred())

				time.Sleep(1 * time.Second)
				resp := test_util.NewResponse(http.StatusOK)
				conn.WriteResponse(resp)
				conn.Close()
			})
			defer ln.Close()

			conn := dialProxy(proxyServer)

			req := test_util.NewRequest("GET", "slow-app", "/", nil)

			started := time.Now()
			conn.WriteRequest(req)

			resp, _ := readResponse(conn)

			Expect(resp.StatusCode).To(Equal(http.StatusBadGateway))
			Expect(time.Since(started)).To(BeNumerically("<", time.Duration(2*time.Second)))
		})

		It("proxy closes connections with slow apps", func() {
			serverResult := make(chan error)
			ln := test_util.RegisterHandler(r, "slow-app", func(conn *test_util.HttpConn) {
				conn.CheckLine("GET / HTTP/1.1")

				timesToTick := 2
				time.Sleep(1 * time.Second)

				conn.WriteLines([]string{
					"HTTP/1.1 200 OK",
					fmt.Sprintf("Content-Length: %d", timesToTick),
				})

				for i := 0; i < timesToTick; i++ {
					_, err := conn.Conn.Write([]byte("x"))
					if err != nil {
						serverResult <- err
						return
					}

					time.Sleep(100 * time.Millisecond)
				}

				serverResult <- nil
			})
			defer ln.Close()

			conn := dialProxy(proxyServer)

			req := test_util.NewRequest("GET", "slow-app", "/", nil)

			started := time.Now()
			conn.WriteRequest(req)

			resp, _ := readResponse(conn)

			Expect(resp.StatusCode).To(Equal(http.StatusBadGateway))
			Expect(time.Since(started)).To(BeNumerically("<", time.Duration(2*time.Second)))

			var err error
			Eventually(serverResult).Should(Receive(&err))
			Expect(err).NotTo(BeNil())
		})

		It("proxy detects closed client connection", func() {
			serverResult := make(chan error)
			readRequest := make(chan struct{})
			ln := test_util.RegisterHandler(r, "slow-app", func(conn *test_util.HttpConn) {
				conn.CheckLine("GET / HTTP/1.1")

				readRequest <- struct{}{}

				timesToTick := 10

				conn.WriteLines([]string{
					"HTTP/1.1 200 OK",
					fmt.Sprintf("Content-Length: %d", timesToTick),
				})

				for i := 0; i < timesToTick; i++ {
					_, err := conn.Conn.Write([]byte("x"))
					if err != nil {
						serverResult <- err
						return
					}

					time.Sleep(100 * time.Millisecond)
				}

				serverResult <- nil
			})
			defer ln.Close()

			conn := dialProxy(proxyServer)

			req := test_util.NewRequest("GET", "slow-app", "/", nil)

			conn.WriteRequest(req)
			Eventually(readRequest).Should(Receive())
			conn.Conn.Close()

			var err error
			Eventually(serverResult).Should(Receive(&err))
			Expect(err).NotTo(BeNil())
		})

		It("proxy closes connections to backends when client closes the connection", func() {
			serverResult := make(chan error)
			readRequest := make(chan struct{})
			ln := test_util.RegisterHandler(r, "slow-app", func(conn *test_util.HttpConn) {
				conn.CheckLine("GET / HTTP/1.1")

				readRequest <- struct{}{}

				time.Sleep(600 * time.Millisecond)

				for i := 0; i < 2; i++ {
					_, err := conn.Conn.Write([]byte("x"))
					if err != nil {
						serverResult <- err
						return
					}

					time.Sleep(100 * time.Millisecond)
				}

				serverResult <- nil
			})
			defer ln.Close()

			conn := dialProxy(proxyServer)

			req := test_util.NewRequest("GET", "slow-app", "/", nil)

			conn.WriteRequest(req)
			Eventually(readRequest).Should(Receive())
			conn.Conn.Close()

			var err error
			Eventually(serverResult).Should(Receive(&err))
			Expect(err).NotTo(BeNil())
		})

		It("retries when failed endpoints exist", func() {
			ln := test_util.RegisterHandler(r, "retries", func(conn *test_util.HttpConn) {
				req, _ := conn.ReadRequest()
				Expect(req.Method).To(Equal("GET"))
				Expect(req.Host).To(Equal("retries"))
				resp := test_util.NewResponse(http.StatusOK)
				conn.WriteResponse(resp)
				conn.Close()
			})
			defer ln.Close()

			test_util.RegisterAddr(r, "retries", "localhost:81", test_util.RegisterConfig{
				InstanceId:    "instanceId",
				InstanceIndex: "2",
			})

			for i := 0; i < 5; i++ {
				body := &bytes.Buffer{}
				body.WriteString("use an actual body")

				conn := dialProxy(proxyServer)

				req := test_util.NewRequest("GET", "retries", "/", ioutil.NopCloser(body))
				conn.WriteRequest(req)
				resp, _ := conn.ReadResponse()

				Expect(resp.StatusCode).To(Equal(http.StatusOK))
			}
		})
	})

	Describe("Access Logging", func() {
		It("Logs a request", func() {
			ln := test_util.RegisterHandler(r, "test", func(conn *test_util.HttpConn) {
				req, body := conn.ReadRequest()
				Expect(req.Method).To(Equal("POST"))
				Expect(req.URL.Path).To(Equal("/"))
				Expect(req.ProtoMajor).To(Equal(1))
				Expect(req.ProtoMinor).To(Equal(1))

				Expect(body).To(Equal("ABCD"))

				rsp := test_util.NewResponse(200)
				out := &bytes.Buffer{}
				out.WriteString("DEFG")
				rsp.Body = ioutil.NopCloser(out)
				conn.WriteResponse(rsp)
			}, test_util.RegisterConfig{InstanceId: "123", AppId: "456"})
			defer ln.Close()

			conn := dialProxy(proxyServer)

			body := &bytes.Buffer{}
			body.WriteString("ABCD")
			req := test_util.NewRequest("POST", "test", "/", ioutil.NopCloser(body))
			conn.WriteRequest(req)

			resp, _ := conn.ReadResponse()
			Expect(resp.StatusCode).To(Equal(http.StatusOK))

			var payload []byte
			Eventually(func() int {
				accessLogFile.Read(&payload)
				return len(payload)
			}).ShouldNot(BeZero())

			//make sure the record includes all the data
			//since the building of the log record happens throughout the life of the request
			Expect(strings.HasPrefix(string(payload), "test - [")).To(BeTrue())
			Expect(string(payload)).To(ContainSubstring(`"POST / HTTP/1.1" 200 4 4 "-"`))
			Expect(string(payload)).To(ContainSubstring(`x_forwarded_for:"127.0.0.1" x_forwarded_proto:"http" vcap_request_id:`))
			Expect(string(payload)).To(ContainSubstring(`response_time:`))
			Expect(string(payload)).To(ContainSubstring(`app_id:"456"`))
			Expect(string(payload)).To(ContainSubstring(`app_index:"2"`))
			Expect(payload[len(payload)-1]).To(Equal(byte('\n')))
		})

		It("Logs a request when X-Forwarded-Proto and X-Forwarded-For are provided", func() {
			ln := test_util.RegisterHandler(r, "test", func(conn *test_util.HttpConn) {
				conn.ReadRequest()
				conn.WriteResponse(test_util.NewResponse(http.StatusOK))
			})
			defer ln.Close()

			conn := dialProxy(proxyServer)

			req := test_util.NewRequest("POST", "test", "/", nil)
			req.Header.Add("X-Forwarded-For", "1.2.3.4")
			req.Header.Add("X-Forwarded-Proto", "https")
			conn.WriteRequest(req)

			resp, _ := conn.ReadResponse()
			Expect(resp.StatusCode).To(Equal(http.StatusOK))

			var payload []byte
			Eventually(func() int {
				accessLogFile.Read(&payload)
				return len(payload)
			}).ShouldNot(BeZero())

			//make sure the record includes all the data
			//since the building of the log record happens throughout the life of the request
			Expect(strings.HasPrefix(string(payload), "test - [")).To(BeTrue())
			Expect(string(payload)).To(ContainSubstring(`"POST / HTTP/1.1" 200 0 0 "-"`))
			Expect(string(payload)).To(ContainSubstring(`x_forwarded_for:"1.2.3.4, 127.0.0.1" x_forwarded_proto:"https" vcap_request_id:`))
			Expect(string(payload)).To(ContainSubstring(`response_time:`))
			Expect(payload[len(payload)-1]).To(Equal(byte('\n')))
		})

		It("Logs a request when it exits early", func() {
			conn := dialProxy(proxyServer)

			body := &bytes.Buffer{}
			body.WriteString("ABCD")
			req := test_util.NewRequest("POST", "test", "/", ioutil.NopCloser(body))
			conn.WriteRequest(req)

			resp, _ := conn.ReadResponse()
			Expect(resp.StatusCode).To(Equal(http.StatusNotFound))

			var payload []byte
			Eventually(func() int {
				n, e := accessLogFile.Read(&payload)
				Expect(e).ToNot(HaveOccurred())
				return n
			}).ShouldNot(BeZero())

			Expect(string(payload)).To(MatchRegexp("^test.*\n"))
		})

		Context("when the request has X-CF-APP-INSTANCE", func() {
			It("lookups the route to that specific app index and id", func() {
				done := make(chan struct{})
				ln := test_util.RegisterHandler(r, "app."+test_util.LocalhostDNS, func(conn *test_util.HttpConn) {
					Fail("App should not have received request")
				}, test_util.RegisterConfig{AppId: "app-1-id"})
				defer ln.Close()

				ln2 := test_util.RegisterHandler(r, "app."+test_util.LocalhostDNS, func(conn *test_util.HttpConn) {
					req, err := http.ReadRequest(conn.Reader)
					Expect(err).NotTo(HaveOccurred())

					Expect(req.Header.Get(router_http.CfAppInstance)).To(BeEmpty())

					resp := test_util.NewResponse(http.StatusOK)
					resp.Body = ioutil.NopCloser(strings.NewReader("Hellow World: App2"))
					conn.WriteResponse(resp)

					conn.Close()

					done <- struct{}{}
				}, test_util.RegisterConfig{AppId: "app-2-id"})
				defer ln2.Close()

				conn := dialProxy(proxyServer)

				req := test_util.NewRequest("GET", "app."+test_util.LocalhostDNS, "/chat", nil)
				req.Header.Set(router_http.CfAppInstance, "app-2-id:2")

				Consistently(func() string {
					conn.WriteRequest(req)

					Eventually(done).Should(Receive())
					_, b := conn.ReadResponse()
					return b
				}).Should(Equal("Hellow World: App2"))
			})

			It("returns a 404 if it cannot find the specified instance", func() {
				ln := test_util.RegisterHandler(r, "app."+test_util.LocalhostDNS, func(conn *test_util.HttpConn) {
					Fail("App should not have received request")
				}, test_util.RegisterConfig{AppId: "app-1-id"})
				defer ln.Close()

				conn := dialProxy(proxyServer)

				req := test_util.NewRequest("GET", "app."+test_util.LocalhostDNS, "/", nil)
				req.Header.Set("X-CF-APP-INSTANCE", "app-1-id:1")
				conn.WriteRequest(req)

				resp, _ := conn.ReadResponse()
				Expect(resp.StatusCode).To(Equal(http.StatusNotFound))
				Expect(resp.Header.Get("X-Cf-RouterError")).To(Equal("unknown_route"))
			})
		})

		Context("with EnableZipkin set to true", func() {
			BeforeEach(func() {
				conf.Tracing.EnableZipkin = true
			})

			It("x_b3_traceid does show up in the access log", func() {
				done := make(chan string)
				ln := test_util.RegisterHandler(r, "app", func(conn *test_util.HttpConn) {
					req, err := http.ReadRequest(conn.Reader)
					Expect(err).NotTo(HaveOccurred())

					resp := test_util.NewResponse(http.StatusOK)
					conn.WriteResponse(resp)
					conn.Close()

					done <- req.Header.Get(handlers.B3TraceIdHeader)
				})
				defer ln.Close()

				conn := dialProxy(proxyServer)
				req := test_util.NewRequest("GET", "app", "/", nil)
				conn.WriteRequest(req)

				var answer string
				Eventually(done).Should(Receive(&answer))
				Expect(answer).ToNot(BeEmpty())

				conn.ReadResponse()

				var payload []byte
				Eventually(func() int {
					accessLogFile.Read(&payload)
					return len(payload)
				}).ShouldNot(BeZero())

				Expect(string(payload)).To(ContainSubstring(fmt.Sprintf(`x_b3_traceid:"%s"`, answer)))
			})
		})
	})

	Describe("User-Agent Healthcheck", func() {
		It("responds to load balancer check", func() {
			conn := dialProxy(proxyServer)

			req := test_util.NewRequest("GET", "", "/", nil)
			req.Header.Set("User-Agent", "HTTP-Monitor/1.1")
			conn.WriteRequest(req)

			resp, body := conn.ReadResponse()
			Expect(resp.Header.Get("Cache-Control")).To(Equal("private, max-age=0"))
			Expect(resp.Header.Get("Expires")).To(Equal("0"))
			Expect(resp.Status).To(Equal("200 OK"))
			Expect(body).To(Equal("ok\n"))
		})

		It("responds with failure to load balancer check if heartbeatOK is false", func() {
			atomic.StoreInt32(&heartbeatOK, 0)

			conn := dialProxy(proxyServer)

			req := test_util.NewRequest("GET", "", "/", nil)
			req.Header.Set("User-Agent", "HTTP-Monitor/1.1")
			conn.WriteRequest(req)

			resp, body := conn.ReadResponse()
			Expect(resp.Header.Get("Cache-Control")).To(Equal("private, max-age=0"))
			Expect(resp.Header.Get("Expires")).To(Equal("0"))
			Expect(resp.Status).NotTo(Equal("200 OK"))
			Expect(body).NotTo(Equal("ok\n"))
		})
	})

	Describe("Error Responses", func() {
		It("responds to unknown host with 404", func() {
			conn := dialProxy(proxyServer)

			req := test_util.NewRequest("GET", "unknown", "/", nil)
			conn.WriteRequest(req)

			resp, body := conn.ReadResponse()
			Expect(resp.StatusCode).To(Equal(http.StatusNotFound))
			Expect(resp.Header.Get("X-Cf-RouterError")).To(Equal("unknown_route"))
			Expect(body).To(Equal("404 Not Found: Requested route ('unknown') does not exist.\n"))
		})

		It("responds to host with malicious script with 400", func() {
			conn := dialProxy(proxyServer)

			req := test_util.NewRequest("GET", "<html><header><script>alert(document.cookie);</script></header><body/></html>", "/", nil)
			conn.WriteRequest(req)

			resp, body := conn.ReadResponse()
			Expect(resp.StatusCode).To(Equal(http.StatusBadRequest))
			Expect(body).To(ContainSubstring("malformed Host header"))
		})

		It("responds with 404 for a not found host name with only valid characters", func() {
			conn := dialProxy(proxyServer)

			req := test_util.NewRequest("GET", "abcdefghijklmnopqrstuvwxyz.0123456789-ABCDEFGHIJKLMNOPQRSTUVW.XYZ", "/", nil)
			conn.WriteRequest(req)

			resp, body := conn.ReadResponse()
			Expect(resp.StatusCode).To(Equal(http.StatusNotFound))
			Expect(resp.Header.Get("X-Cf-RouterError")).To(Equal("unknown_route"))
			Expect(body).To(Equal("404 Not Found: Requested route ('abcdefghijklmnopqrstuvwxyz.0123456789-ABCDEFGHIJKLMNOPQRSTUVW.XYZ') does not exist.\n"))
		})

		It("responds to misbehaving host with 502", func() {
			ln := test_util.RegisterHandler(r, "enfant-terrible", func(conn *test_util.HttpConn) {
				conn.Close()
			})
			defer ln.Close()

			conn := dialProxy(proxyServer)

			req := test_util.NewRequest("GET", "enfant-terrible", "/", nil)
			conn.WriteRequest(req)

			resp, body := conn.ReadResponse()
			Expect(resp.StatusCode).To(Equal(http.StatusBadGateway))
			Expect(resp.Header.Get("X-Cf-RouterError")).To(Equal("endpoint_failure"))
			Expect(body).To(Equal("502 Bad Gateway: Registered endpoint failed to handle the request.\n"))
		})

		Context("when the endpoint is nil", func() {
			removeAllEndpoints := func(pool *route.Pool) {
				endpoints := make([]*route.Endpoint, 0)
				pool.Each(func(e *route.Endpoint) {
					endpoints = append(endpoints, e)
				})
				for _, e := range endpoints {
					pool.Remove(e)
				}
			}

			It("responds with a 404 NotFound", func() {
				ln := test_util.RegisterHandler(r, "nil-endpoint", func(conn *test_util.HttpConn) {
					conn.CheckLine("GET / HTTP/1.1")
					resp := test_util.NewResponse(http.StatusOK)
					conn.WriteResponse(resp)
					conn.Close()
				})
				defer ln.Close()

				removeAllEndpoints(r.Lookup(route.Uri("nil-endpoint")))
				conn := dialProxy(proxyServer)

				req := test_util.NewRequest("GET", "nil-endpoint", "/", nil)
				conn.WriteRequest(req)

				b := make([]byte, 0, 0)
				buf := bytes.NewBuffer(b)
				log.SetOutput(buf)
				res, _ := conn.ReadResponse()
				log.SetOutput(os.Stderr)
				Expect(buf).NotTo(ContainSubstring("multiple response.WriteHeader calls"))
				Expect(res.StatusCode).To(Equal(http.StatusNotFound))
			})
		})
	})

	Describe("WebSocket Connections", func() {
		Context("when the request is mapped to route service", func() {

			It("responds with 503", func() {
				done := make(chan bool)

				ln := test_util.RegisterHandler(r, "ws", func(conn *test_util.HttpConn) {
					req, err := http.ReadRequest(conn.Reader)
					Expect(err).NotTo(HaveOccurred())

					done <- req.Header.Get("Upgrade") == "WebsockeT" &&
						req.Header.Get("Connection") == "UpgradE"

					resp := test_util.NewResponse(http.StatusSwitchingProtocols)
					resp.Header.Set("Upgrade", "WebsockeT")
					resp.Header.Set("Connection", "UpgradE")

					conn.WriteResponse(resp)

					conn.CheckLine("hello from client")
					conn.WriteLine("hello from server")
					conn.Close()
				})
				defer ln.Close()

				conn := dialProxy(proxyServer)

				req := test_util.NewRequest("GET", "ws", "/chat", nil)
				req.Header.Set("Upgrade", "WebsockeT")
				req.Header.Set("Connection", "UpgradE")

				conn.WriteRequest(req)

				var answer bool
				Eventually(done).Should(Receive(&answer))
				Expect(answer).To(BeTrue())

				resp, _ := conn.ReadResponse()
				Expect(resp.StatusCode).To(Equal(http.StatusSwitchingProtocols))
				Expect(resp.Header.Get("Upgrade")).To(Equal("WebsockeT"))
				Expect(resp.Header.Get("Connection")).To(Equal("UpgradE"))

				conn.WriteLine("hello from client")
				conn.CheckLine("hello from server")

				conn.Close()
			})
		})

		It("upgrades for a WebSocket request", func() {
			done := make(chan bool)

			ln := test_util.RegisterHandler(r, "ws", func(conn *test_util.HttpConn) {
				req, err := http.ReadRequest(conn.Reader)
				Expect(err).NotTo(HaveOccurred())

				done <- req.Header.Get("Upgrade") == "WebsockeT" &&
					req.Header.Get("Connection") == "UpgradE"

				resp := test_util.NewResponse(http.StatusSwitchingProtocols)
				resp.Header.Set("Upgrade", "WebsockeT")
				resp.Header.Set("Connection", "UpgradE")

				conn.WriteResponse(resp)

				conn.CheckLine("hello from client")
				conn.WriteLine("hello from server")
				conn.Close()
			})
			defer ln.Close()

			conn := dialProxy(proxyServer)

			req := test_util.NewRequest("GET", "ws", "/chat", nil)
			req.Header.Set("Upgrade", "WebsockeT")
			req.Header.Set("Connection", "UpgradE")

			conn.WriteRequest(req)

			var answer bool
			Eventually(done).Should(Receive(&answer))
			Expect(answer).To(BeTrue())

			resp, _ := conn.ReadResponse()
			Expect(resp.StatusCode).To(Equal(http.StatusSwitchingProtocols))
			Expect(resp.Header.Get("Upgrade")).To(Equal("WebsockeT"))
			Expect(resp.Header.Get("Connection")).To(Equal("UpgradE"))

			conn.WriteLine("hello from client")
			conn.CheckLine("hello from server")

			conn.Close()
		})

		It("upgrades for a WebSocket request with comma-separated Connection header", func() {
			done := make(chan bool)

			ln := test_util.RegisterHandler(r, "ws-cs-header", func(conn *test_util.HttpConn) {
				req, err := http.ReadRequest(conn.Reader)
				Expect(err).NotTo(HaveOccurred())

				done <- req.Header.Get("Upgrade") == "Websocket" &&
					req.Header.Get("Connection") == "keep-alive, Upgrade"

				resp := test_util.NewResponse(http.StatusSwitchingProtocols)
				resp.Header.Set("Upgrade", "Websocket")
				resp.Header.Set("Connection", "Upgrade")

				conn.WriteResponse(resp)

				conn.CheckLine("hello from client")
				conn.WriteLine("hello from server")
				conn.Close()
			})
			defer ln.Close()

			conn := dialProxy(proxyServer)

			req := test_util.NewRequest("GET", "ws-cs-header", "/chat", nil)
			req.Header.Add("Upgrade", "Websocket")
			req.Header.Add("Connection", "keep-alive, Upgrade")

			conn.WriteRequest(req)

			var answer bool
			Eventually(done).Should(Receive(&answer))
			Expect(answer).To(BeTrue())

			resp, _ := conn.ReadResponse()
			Expect(resp.StatusCode).To(Equal(http.StatusSwitchingProtocols))

			Expect(resp.Header.Get("Upgrade")).To(Equal("Websocket"))
			Expect(resp.Header.Get("Connection")).To(Equal("Upgrade"))

			conn.WriteLine("hello from client")
			conn.CheckLine("hello from server")

			conn.Close()
		})

		It("upgrades for a WebSocket request with multiple Connection headers", func() {
			done := make(chan bool)

			ln := test_util.RegisterHandler(r, "ws-cs-header", func(conn *test_util.HttpConn) {
				req, err := http.ReadRequest(conn.Reader)
				Expect(err).NotTo(HaveOccurred())

				done <- req.Header.Get("Upgrade") == "Websocket" &&
					req.Header[http.CanonicalHeaderKey("Connection")][0] == "keep-alive" &&
					req.Header[http.CanonicalHeaderKey("Connection")][1] == "Upgrade"

				resp := test_util.NewResponse(http.StatusSwitchingProtocols)
				resp.Header.Set("Upgrade", "Websocket")
				resp.Header.Set("Connection", "Upgrade")

				conn.WriteResponse(resp)

				conn.CheckLine("hello from client")
				conn.WriteLine("hello from server")
				conn.Close()
			})
			defer ln.Close()

			conn := dialProxy(proxyServer)

			req := test_util.NewRequest("GET", "ws-cs-header", "/chat", nil)
			req.Header.Add("Upgrade", "Websocket")
			req.Header.Add("Connection", "keep-alive")
			req.Header.Add("Connection", "Upgrade")

			conn.WriteRequest(req)

			var answer bool
			Eventually(done).Should(Receive(&answer))
			Expect(answer).To(BeTrue())

			resp, _ := conn.ReadResponse()
			Expect(resp.StatusCode).To(Equal(http.StatusSwitchingProtocols))

			Expect(resp.Header.Get("Upgrade")).To(Equal("Websocket"))
			Expect(resp.Header.Get("Connection")).To(Equal("Upgrade"))

			conn.WriteLine("hello from client")
			conn.CheckLine("hello from server")

			conn.Close()
		})

		It("Logs the response time and status code 101 in the access logs", func() {
			done := make(chan bool)
			ln := test_util.RegisterHandler(r, "ws", func(conn *test_util.HttpConn) {
				req, err := http.ReadRequest(conn.Reader)
				Expect(err).NotTo(HaveOccurred())

				done <- req.Header.Get("Upgrade") == "Websocket" &&
					req.Header.Get("Connection") == "Upgrade"

				resp := test_util.NewResponse(http.StatusSwitchingProtocols)
				resp.Header.Set("Upgrade", "Websocket")
				resp.Header.Set("Connection", "Upgrade")

				conn.WriteResponse(resp)

				conn.CheckLine("hello from client")
				conn.WriteLine("hello from server")
				conn.Close()
			})
			defer ln.Close()

			conn := dialProxy(proxyServer)

			req := test_util.NewRequest("GET", "ws", "/chat", nil)
			req.Header.Set("Upgrade", "Websocket")
			req.Header.Set("Connection", "Upgrade")

			conn.WriteRequest(req)

			var answer bool
			Eventually(done).Should(Receive(&answer))
			Expect(answer).To(BeTrue())

			resp, _ := conn.ReadResponse()
			Expect(resp.StatusCode).To(Equal(http.StatusSwitchingProtocols))
			Expect(resp.Header.Get("Upgrade")).To(Equal("Websocket"))
			Expect(resp.Header.Get("Connection")).To(Equal("Upgrade"))

			conn.WriteLine("hello from client")
			conn.CheckLine("hello from server")

			var payload []byte
			Eventually(func() int {
				accessLogFile.Read(&payload)
				return len(payload)
			}).ShouldNot(BeZero())

			Expect(string(payload)).To(ContainSubstring(`response_time:`))
			Expect(string(payload)).To(ContainSubstring("HTTP/1.1\" 101"))
			responseTime := parseResponseTimeFromLog(string(payload))
			Expect(responseTime).To(BeNumerically(">", 0))

			conn.Close()
		})

		It("emits a xxx metric", func() {
			ln := test_util.RegisterHandler(r, "ws-cs-header", func(conn *test_util.HttpConn) {
				resp := test_util.NewResponse(http.StatusSwitchingProtocols)
				resp.Header.Set("Upgrade", "Websocket")
				resp.Header.Set("Connection", "Upgrade")

				conn.WriteResponse(resp)
				conn.Close()
			})
			defer ln.Close()

			connectClient := func() {
				conn := dialProxy(proxyServer)

				req := test_util.NewRequest("GET", "ws-cs-header", "/chat", nil)
				req.Header.Add("Upgrade", "Websocket")
				req.Header.Add("Connection", "keep-alive")
				req.Header.Add("Connection", "Upgrade")

				conn.WriteRequest(req)

			}
			// 1st client connected
			connectClient()
			// 2nd client connected
			connectClient()

			Eventually(fakeReporter.CaptureWebSocketUpdateCallCount).Should(Equal(2))
		})

		It("does not emit a latency metric", func() {
			var wg sync.WaitGroup
			ln := test_util.RegisterHandler(r, "ws-cs-header", func(conn *test_util.HttpConn) {
				defer conn.Close()
				defer wg.Done()
				resp := test_util.NewResponse(http.StatusSwitchingProtocols)
				resp.Header.Set("Upgrade", "Websocket")
				resp.Header.Set("Connection", "Upgrade")

				conn.WriteResponse(resp)

				for {
					_, err := conn.Write([]byte("Hello"))
					if err != nil {
						return
					}
				}
			})
			defer ln.Close()

			conn := dialProxy(proxyServer)

			req := test_util.NewRequest("GET", "ws-cs-header", "/chat", nil)
			req.Header.Add("Upgrade", "Websocket")
			req.Header.Add("Connection", "keep-alive")
			req.Header.Add("Connection", "Upgrade")

			wg.Add(1)
			conn.WriteRequest(req)
			resp, err := http.ReadResponse(conn.Reader, nil)
			Expect(err).ToNot(HaveOccurred())
			Expect(resp.StatusCode).To(Equal(http.StatusSwitchingProtocols))
			buf := make([]byte, 5)
			_, err = conn.Read(buf)
			Expect(err).ToNot(HaveOccurred())
			conn.Close()
			wg.Wait()

			Consistently(fakeReporter.CaptureRoutingResponseLatencyCallCount, 1).Should(Equal(0))
		})

		Context("when the connection to the backend fails", func() {
			It("emits a failure metric and logs a 502 in the access logs", func() {
				test_util.RegisterAddr(r, "ws", "192.0.2.1:1234", test_util.RegisterConfig{
					InstanceIndex: "2",
					AppId:         "abc",
				})

				conn := dialProxy(proxyServer)

				req := test_util.NewRequest("GET", "ws", "/chat", nil)
				req.Header.Set("Upgrade", "Websocket")
				req.Header.Set("Connection", "Upgrade")

				conn.WriteRequest(req)

				res, err := http.ReadResponse(conn.Reader, &http.Request{})
				Expect(err).ToNot(HaveOccurred())
				Expect(res.StatusCode).To(Equal(http.StatusBadGateway))

				var payload []byte
				Eventually(func() int {
					accessLogFile.Read(&payload)
					return len(payload)
				}).ShouldNot(BeZero())

				Expect(string(payload)).To(ContainSubstring(`response_time:`))
				Expect(string(payload)).To(ContainSubstring("HTTP/1.1\" 502"))
				responseTime := parseResponseTimeFromLog(string(payload))
				Expect(responseTime).To(BeNumerically(">", 0))

				Expect(fakeReporter.CaptureWebSocketUpdateCallCount()).To(Equal(0))
				Expect(fakeReporter.CaptureWebSocketFailureCallCount()).To(Equal(1))
				conn.Close()
			})
		})
	})

	Describe("TCP Upgrade Connections", func() {
		It("upgrades a Tcp request", func() {
			ln := test_util.RegisterHandler(r, "tcp-handler", func(conn *test_util.HttpConn) {
				conn.WriteLine("HTTP/1.1 101 Switching Protocols\r\n\r\nhello")
				conn.CheckLine("hello from client")
				conn.WriteLine("hello from server")
				conn.Close()
			})
			defer ln.Close()

			conn := dialProxy(proxyServer)

			req := test_util.NewRequest("GET", "tcp-handler", "/chat", nil)
			req.Header.Set("Upgrade", "tcp")

			req.Header.Set("Connection", "Upgrade")

			conn.WriteRequest(req)

			conn.CheckLine("HTTP/1.1 101 Switching Protocols")
			conn.CheckLine("")
			conn.CheckLine("hello")
			conn.WriteLine("hello from client")
			conn.CheckLine("hello from server")

			conn.Close()
		})
		It("logs the response time and status code 101 in the access logs", func() {
			ln := test_util.RegisterHandler(r, "tcp-handler", func(conn *test_util.HttpConn) {
				conn.WriteLine("HTTP/1.1 101 Switching Protocols\r\n\r\nhello")
				conn.CheckLine("hello from client")
				conn.WriteLine("hello from server")
				conn.Close()
			})
			defer ln.Close()

			conn := dialProxy(proxyServer)

			req := test_util.NewRequest("GET", "tcp-handler", "/chat", nil)
			req.Header.Set("Upgrade", "tcp")

			req.Header.Set("Connection", "Upgrade")

			conn.WriteRequest(req)

			conn.CheckLine("HTTP/1.1 101 Switching Protocols")
			conn.CheckLine("")
			conn.CheckLine("hello")
			conn.WriteLine("hello from client")
			conn.CheckLine("hello from server")

			var payload []byte
			Eventually(func() int {
				accessLogFile.Read(&payload)
				return len(payload)
			}).ShouldNot(BeZero())

			Expect(string(payload)).To(ContainSubstring(`response_time:`))
			Expect(string(payload)).To(ContainSubstring("HTTP/1.1\" 101"))
			responseTime := parseResponseTimeFromLog(string(payload))
			Expect(responseTime).To(BeNumerically(">", 0))

			conn.Close()
		})
		It("does not emit a latency metric", func() {
			var wg sync.WaitGroup
			first := true
			ln := test_util.RegisterHandler(r, "tcp-handler", func(conn *test_util.HttpConn) {
				defer wg.Done()
				defer conn.Close()
				if first {
					conn.WriteLine("HTTP/1.1 101 Switching Protocols\r\n\r\nhello")
					first = false
				}
				for {
					_, err := conn.Write([]byte("Hello"))
					if err != nil {
						return
					}
				}
			})
			defer ln.Close()

			conn := dialProxy(proxyServer)

			req := test_util.NewRequest("GET", "tcp-handler", "/chat", nil)
			req.Header.Set("Upgrade", "tcp")

			req.Header.Set("Connection", "Upgrade")

			wg.Add(1)
			conn.WriteRequest(req)
			buf := make([]byte, 5)
			_, err := conn.Read(buf)
			Expect(err).ToNot(HaveOccurred())
			conn.Close()
			wg.Wait()

			Consistently(fakeReporter.CaptureRoutingResponseLatencyCallCount, 1).Should(Equal(0))
		})
		Context("when the connection to the backend fails", func() {
			It("logs a 502 BadGateway", func() {
				test_util.RegisterAddr(r, "tcp-handler", "192.0.2.1:1234", test_util.RegisterConfig{
					InstanceIndex: "2",
					AppId:         "abc",
				})

				conn := dialProxy(proxyServer)

				req := test_util.NewRequest("GET", "tcp-handler", "/chat", nil)
				req.Header.Set("Upgrade", "tcp")
				req.Header.Set("Connection", "Upgrade")

				conn.WriteRequest(req)

				res, err := http.ReadResponse(conn.Reader, &http.Request{})
				Expect(err).ToNot(HaveOccurred())
				Expect(res.StatusCode).To(Equal(http.StatusBadGateway))

				var payload []byte
				Eventually(func() int {
					accessLogFile.Read(&payload)
					return len(payload)
				}).ShouldNot(BeZero())

				Expect(string(payload)).To(ContainSubstring(`response_time:`))
				Expect(string(payload)).To(ContainSubstring("HTTP/1.1\" 502"))
				responseTime := parseResponseTimeFromLog(string(payload))
				Expect(responseTime).To(BeNumerically(">", 0))

				conn.Close()
			})
		})
	})

	Describe("Metrics", func() {
		It("captures the routing response", func() {
			ln := test_util.RegisterHandler(r, "reporter-test", func(conn *test_util.HttpConn) {
				_, err := http.ReadRequest(conn.Reader)
				Expect(err).NotTo(HaveOccurred())

				resp := test_util.NewResponse(http.StatusOK)
				conn.WriteResponse(resp)
				conn.Close()
			})
			defer ln.Close()

			conn := dialProxy(proxyServer)

			req := test_util.NewRequest("GET", "reporter-test", "/", nil)
			conn.WriteRequest(req)

			resp, _ := conn.ReadResponse()
			Expect(resp.StatusCode).To(Equal(http.StatusOK))

			Expect(fakeReporter.CaptureBadGatewayCallCount()).To(Equal(0))

			Expect(fakeReporter.CaptureRoutingResponseCallCount()).To(Equal(1))
			capturedRespCode := fakeReporter.CaptureRoutingResponseArgsForCall(0)
			Expect(capturedRespCode).To(Equal(http.StatusOK))

			Expect(fakeReporter.CaptureRoutingResponseLatencyCallCount()).To(Equal(1))
			capturedEndpoint, capturedRespCode, startTime, latency := fakeReporter.CaptureRoutingResponseLatencyArgsForCall(0)
			Expect(capturedEndpoint).ToNot(BeNil())
			Expect(capturedEndpoint.ApplicationId).To(Equal(""))
			Expect(capturedEndpoint.PrivateInstanceId).To(Equal(""))
			Expect(capturedEndpoint.PrivateInstanceIndex).To(Equal("2"))
			Expect(capturedRespCode).To(Equal(http.StatusOK))
			Expect(startTime).To(BeTemporally("~", time.Now(), 100*time.Millisecond))
			Expect(latency).To(BeNumerically(">", 0))

			Expect(fakeReporter.CaptureRoutingRequestCallCount()).To(Equal(1))
			Expect(fakeReporter.CaptureRoutingRequestArgsForCall(0)).To(Equal(capturedEndpoint))
		})

		It("emits HTTP startstop events", func() {
			done := make(chan struct{})
			var vcapHeader string
			ln := test_util.RegisterHandler(r, "app", func(conn *test_util.HttpConn) {
				req, _ := conn.ReadRequest()
				vcapHeader = req.Header.Get(handlers.VcapRequestIdHeader)
				done <- struct{}{}
				resp := test_util.NewResponse(http.StatusOK)
				conn.WriteResponse(resp)
				conn.Close()
			}, test_util.RegisterConfig{InstanceId: "fake-instance-id"})
			defer ln.Close()

			conn := dialProxy(proxyServer)

			req := test_util.NewRequest("GET", "app", "/", nil)

			conn.WriteRequest(req)
			findStartStopEvent := func() *events.HttpStartStop {
				for _, ev := range fakeEmitter.GetEvents() {
					startStopEvent, ok := ev.(*events.HttpStartStop)
					if ok {
						return startStopEvent
					}
				}
				return nil
			}

			Eventually(done).Should(Receive())

			Eventually(findStartStopEvent).ShouldNot(BeNil())
			u2, err := uuid.ParseHex(vcapHeader)
			Expect(err).NotTo(HaveOccurred())
			Expect(findStartStopEvent().RequestId).To(Equal(factories.NewUUID(u2)))
		})

		Context("when the endpoint is nil", func() {
			removeAllEndpoints := func(pool *route.Pool) {
				endpoints := make([]*route.Endpoint, 0)
				pool.Each(func(e *route.Endpoint) {
					endpoints = append(endpoints, e)
				})
				for _, e := range endpoints {
					pool.Remove(e)
				}
			}

			It("captures bad gateway but does not capture routing response", func() {
				ln := test_util.RegisterHandler(r, "nil-endpoint", func(conn *test_util.HttpConn) {
					conn.CheckLine("GET / HTTP/1.1")
					resp := test_util.NewResponse(http.StatusOK)
					conn.WriteResponse(resp)
					conn.Close()
				})
				defer ln.Close()

				removeAllEndpoints(r.Lookup(route.Uri("nil-endpoint")))
				conn := dialProxy(proxyServer)

				req := test_util.NewRequest("GET", "nil-endpoint", "/", nil)
				conn.WriteRequest(req)

				res, _ := conn.ReadResponse()
				Expect(res.StatusCode).To(Equal(http.StatusNotFound))
				Expect(fakeReporter.CaptureBadRequestCallCount()).To(Equal(1))
				Expect(fakeReporter.CaptureRoutingResponseCallCount()).To(Equal(0))
				Expect(fakeReporter.CaptureRoutingResponseLatencyCallCount()).To(Equal(0))
			})
		})
	})

})

// HACK: this is used to silence any http warnings in logs
// that clutter stdout/stderr when running unit tests
func readResponse(conn *test_util.HttpConn) (*http.Response, string) {
	log.SetOutput(ioutil.Discard)
	res, body := conn.ReadResponse()
	log.SetOutput(os.Stderr)
	return res, body
}

func dialProxy(proxyServer net.Listener) *test_util.HttpConn {
	conn, err := net.Dial("tcp", proxyServer.Addr().String())
	Expect(err).NotTo(HaveOccurred())

	return test_util.NewHttpConn(conn)
}

func newTlsListener(listener net.Listener) net.Listener {
	cert, err := tls.LoadX509KeyPair("../test/assets/certs/server.pem", "../test/assets/certs/server.key")
	Expect(err).ToNot(HaveOccurred())

	tlsConfig := &tls.Config{
		Certificates: []tls.Certificate{cert},
		CipherSuites: []uint16{tls.TLS_RSA_WITH_AES_256_CBC_SHA},
	}

	return tls.NewListener(listener, tlsConfig)
}

func parseResponseTimeFromLog(log string) float64 {
	r, err := regexp.Compile("response_time:(\\d+.\\d+)")
	Expect(err).ToNot(HaveOccurred())

	responseTimeStr := r.FindStringSubmatch(log)

	f, err := strconv.ParseFloat(responseTimeStr[1], 64)
	Expect(err).ToNot(HaveOccurred())

	return f
}

func responseContains(resp *http.Response, match string) bool {
	dump, err := httputil.DumpResponse(resp, true)
	Expect(err).ToNot(HaveOccurred())
	str := strings.ToLower(string(dump))
	return strings.Contains(str, strings.ToLower(match))
}
