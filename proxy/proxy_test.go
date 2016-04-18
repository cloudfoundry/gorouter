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
	"time"

	"github.com/cloudfoundry/dropsonde"
	"github.com/cloudfoundry/dropsonde/emitter/fake"
	"github.com/cloudfoundry/dropsonde/factories"
	router_http "github.com/cloudfoundry/gorouter/common/http"
	"github.com/cloudfoundry/gorouter/registry"
	"github.com/cloudfoundry/gorouter/route"
	"github.com/cloudfoundry/gorouter/test_util"
	"github.com/cloudfoundry/sonde-go/events"
	"github.com/nu7hatch/gouuid"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

const uuid_regex = `^[[:xdigit:]]{8}(-[[:xdigit:]]{4}){3}-[[:xdigit:]]{12}$`

type connHandler func(*test_util.HttpConn)

var _ = Describe("Proxy", func() {

	It("responds to http/1.0 with path", func() {
		ln := registerHandler(r, "test/my_path", func(conn *test_util.HttpConn) {
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

	It("responds transparently to a trailing slash versus no trailing slash", func() {
		lnWithoutSlash := registerHandler(r, "test/my%20path/your_path", func(conn *test_util.HttpConn) {
			conn.CheckLine("GET /my%20path/your_path/ HTTP/1.1")

			conn.WriteResponse(test_util.NewResponse(http.StatusOK))
		})
		defer lnWithoutSlash.Close()

		lnWithSlash := registerHandler(r, "test/another-path/your_path/", func(conn *test_util.HttpConn) {
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

	It("Content-type is not set by proxy", func() {
		ln := registerHandler(r, "content-test", func(x *test_util.HttpConn) {
			_, err := http.ReadRequest(x.Reader)
			Expect(err).NotTo(HaveOccurred())

			resp := test_util.NewResponse(http.StatusOK)
			x.WriteResponse(resp)
			x.WriteLine("hello from server")
			x.Close()
		})
		defer ln.Close()

		x := dialProxy(proxyServer)

		req := test_util.NewRequest("GET", "content-test", "/", nil)
		req.Host = "content-test"
		x.WriteRequest(req)

		resp, _ := x.ReadResponse()
		h, present := resp.Header["Content-Type"]
		Expect(present).To(BeFalse())
		Expect(h).To(BeNil())
		Expect(responseContains(resp, "Content-Type:")).To(BeFalse())
	})

	It("Content-type xml is not set by proxy", func() {
		ln := registerHandler(r, "content-test", func(x *test_util.HttpConn) {
			_, err := http.ReadRequest(x.Reader)
			Expect(err).NotTo(HaveOccurred())

			resp := test_util.NewResponse(http.StatusOK)
			x.WriteResponse(resp)
			x.WriteLine("<?xml version=\"1.0\" encoding=\"UTF-8\"?>")
			x.Close()
		})
		defer ln.Close()

		x := dialProxy(proxyServer)

		req := test_util.NewRequest("GET", "content-test", "/", nil)
		req.Host = "content-test"
		x.WriteRequest(req)

		resp, _ := x.ReadResponse()

		h, present := resp.Header["Content-Type"]
		Expect(present).To(BeFalse())
		Expect(h).To(BeNil())
		Expect(responseContains(resp, "Content-Type:")).To(BeFalse())
	})

	It("Content-type header is not set for an HTTP 204 response", func() {
		ln := registerHandler(r, "no-content-test", func(x *test_util.HttpConn) {
			_, err := http.ReadRequest(x.Reader)
			Expect(err).NotTo(HaveOccurred())

			resp := test_util.NewResponse(http.StatusNoContent)
			x.WriteResponse(resp)
			x.Close()
		})
		defer ln.Close()

		x := dialProxy(proxyServer)

		req := test_util.NewRequest("GET", "no-content-test", "/", nil)
		req.Host = "no-content-test"
		x.WriteRequest(req)

		resp, _ := x.ReadResponse()

		h, present := resp.Header["Content-Type"]
		Expect(present).To(BeFalse())
		Expect(h).To(BeNil())
		Expect(responseContains(resp, "Content-Type:")).To(BeFalse())
	})

	It("responds to http/1.0 with path/path", func() {
		ln := registerHandler(r, "test/my%20path/your_path", func(conn *test_util.HttpConn) {
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

	It("responds to http/1.0", func() {
		ln := registerHandler(r, "test", func(conn *test_util.HttpConn) {
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
		ln := registerHandler(r, "test", func(conn *test_util.HttpConn) {
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

	It("responds to HTTP/1.1 with absolute-form request target", func() {
		ln := registerHandler(r, "test.io", func(conn *test_util.HttpConn) {
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
		ln := registerHandler(r, "test.io/my%20path/your_path", func(conn *test_util.HttpConn) {
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

	It("does not respond to unsupported HTTP versions", func() {
		conn := dialProxy(proxyServer)

		conn.WriteLines([]string{
			"GET / HTTP/0.9",
			"Host: test",
		})

		resp, _ := conn.ReadResponse()
		Expect(resp.StatusCode).To(Equal(http.StatusBadRequest))
	})

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

	It("responds with failure to load balancer check after StopHeartbeat() has been called", func() {
		p.Drain()

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

	It("responds to unknown host with 404", func() {
		conn := dialProxy(proxyServer)

		req := test_util.NewRequest("GET", "unknown", "/", nil)
		conn.WriteRequest(req)

		resp, body := conn.ReadResponse()
		Expect(resp.StatusCode).To(Equal(http.StatusNotFound))
		Expect(resp.Header.Get("X-Cf-RouterError")).To(Equal("unknown_route"))
		Expect(body).To(Equal("404 Not Found: Requested route ('unknown') does not exist.\n"))
	})

	It("responds to host with malicious script with 404", func() {
		conn := dialProxy(proxyServer)

		req := test_util.NewRequest("GET", "<html><header><script>alert(document.cookie);</script></header><body/></html>", "/", nil)
		conn.WriteRequest(req)

		resp, body := conn.ReadResponse()
		Expect(resp.StatusCode).To(Equal(http.StatusNotFound))
		Expect(resp.Header.Get("X-Cf-RouterError")).To(Equal("unknown_route"))
		Expect(body).To(Equal("404 Not Found: Requested route does not exist.\n"))
	})

	It("responds with 404 for a not found host name with only valid characters", func() {
		conn := dialProxy(proxyServer)

		req := test_util.NewRequest("GET", "abcdefghijklmnopqrstuvwxyz.0123456789-ABCDEFGHIJKLMNOPQRSTUVW.XYZ ", "/", nil)
		conn.WriteRequest(req)

		resp, body := conn.ReadResponse()
		Expect(resp.StatusCode).To(Equal(http.StatusNotFound))
		Expect(resp.Header.Get("X-Cf-RouterError")).To(Equal("unknown_route"))
		Expect(body).To(Equal("404 Not Found: Requested route ('abcdefghijklmnopqrstuvwxyz.0123456789-ABCDEFGHIJKLMNOPQRSTUVW.XYZ') does not exist.\n"))
	})

	It("responds to misbehaving host with 502", func() {
		ln := registerHandler(r, "enfant-terrible", func(conn *test_util.HttpConn) {
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

	It("trace headers added on correct TraceKey", func() {
		ln := registerHandler(r, "trace-test", func(conn *test_util.HttpConn) {
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
		ln := registerHandler(r, "trace-test", func(conn *test_util.HttpConn) {
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

	It("X-Forwarded-For is added", func() {
		done := make(chan bool)

		ln := registerHandler(r, "app", func(conn *test_util.HttpConn) {
			req, err := http.ReadRequest(conn.Reader)
			Expect(err).NotTo(HaveOccurred())

			resp := test_util.NewResponse(http.StatusOK)
			conn.WriteResponse(resp)
			conn.Close()

			done <- req.Header.Get("X-Forwarded-For") == "127.0.0.1"
		})
		defer ln.Close()

		conn := dialProxy(proxyServer)
		req := test_util.NewRequest("GET", "app", "/", nil)
		conn.WriteRequest(req)

		var answer bool
		Eventually(done).Should(Receive(&answer))
		Expect(answer).To(BeTrue())

		conn.ReadResponse()
	})

	It("X-Forwarded-For is appended", func() {
		done := make(chan bool)

		ln := registerHandler(r, "app", func(conn *test_util.HttpConn) {
			req, err := http.ReadRequest(conn.Reader)
			Expect(err).NotTo(HaveOccurred())

			resp := test_util.NewResponse(http.StatusOK)
			conn.WriteResponse(resp)
			conn.Close()

			done <- req.Header.Get("X-Forwarded-For") == "1.2.3.4, 127.0.0.1"
		})
		defer ln.Close()

		conn := dialProxy(proxyServer)

		req := test_util.NewRequest("GET", "app", "/", nil)
		req.Header.Add("X-Forwarded-For", "1.2.3.4")
		conn.WriteRequest(req)

		var answer bool
		Eventually(done).Should(Receive(&answer))
		Expect(answer).To(BeTrue())

		conn.ReadResponse()
	})

	It("X-Request-Start is appended", func() {
		done := make(chan string)

		ln := registerHandler(r, "app", func(conn *test_util.HttpConn) {
			req, err := http.ReadRequest(conn.Reader)
			Expect(err).NotTo(HaveOccurred())

			resp := test_util.NewResponse(http.StatusOK)
			conn.WriteResponse(resp)
			conn.Close()

			done <- req.Header.Get("X-Request-Start")
		})
		defer ln.Close()

		conn := dialProxy(proxyServer)

		req := test_util.NewRequest("GET", "app", "/", nil)
		conn.WriteRequest(req)

		var answer string
		Eventually(done).Should(Receive(&answer))
		Expect(answer).To(MatchRegexp("^\\d{10}\\d{3}$")) // unix timestamp millis

		conn.ReadResponse()
	})

	It("X-Request-Start is not overwritten", func() {
		done := make(chan []string)

		ln := registerHandler(r, "app", func(conn *test_util.HttpConn) {
			req, err := http.ReadRequest(conn.Reader)
			Expect(err).NotTo(HaveOccurred())

			resp := test_util.NewResponse(http.StatusOK)
			conn.WriteResponse(resp)
			conn.Close()

			done <- req.Header[http.CanonicalHeaderKey("X-Request-Start")]
		})
		defer ln.Close()

		conn := dialProxy(proxyServer)

		req := test_util.NewRequest("GET", "app", "/", nil)
		req.Header.Add("X-Request-Start", "") // impl cannot just check for empty string
		req.Header.Add("X-Request-Start", "user-set2")
		conn.WriteRequest(req)

		var answer []string
		Eventually(done).Should(Receive(&answer))
		Expect(answer).To(Equal([]string{"", "user-set2"}))

		conn.ReadResponse()
	})

	It("X-CF-InstanceID header is added literally if present in the routing endpoint", func() {
		done := make(chan string)

		ln := registerHandlerWithInstanceId(r, "app", "", func(conn *test_util.HttpConn) {
			req, err := http.ReadRequest(conn.Reader)
			Expect(err).NotTo(HaveOccurred())

			resp := test_util.NewResponse(http.StatusOK)
			conn.WriteResponse(resp)
			conn.Close()

			done <- req.Header.Get(router_http.CfInstanceIdHeader)
		}, "fake-instance-id")
		defer ln.Close()

		conn := dialProxy(proxyServer)

		req := test_util.NewRequest("GET", "app", "/", nil)
		conn.WriteRequest(req)

		var answer string
		Eventually(done).Should(Receive(&answer))
		Expect(answer).To(Equal("fake-instance-id"))

		conn.ReadResponse()
	})

	It("adds X-Forwarded-Proto if not present", func() {
		done := make(chan string)

		ln := registerHandler(r, "app", func(conn *test_util.HttpConn) {
			req, err := http.ReadRequest(conn.Reader)
			Expect(err).NotTo(HaveOccurred())

			resp := test_util.NewResponse(http.StatusOK)
			conn.WriteResponse(resp)
			conn.Close()

			done <- req.Header.Get("X-Forwarded-Proto")
		})
		defer ln.Close()

		conn := dialProxy(proxyServer)

		req := test_util.NewRequest("GET", "app", "/", nil)
		conn.WriteRequest(req)

		var answer string
		Eventually(done).Should(Receive(&answer))
		Expect(answer).To(Equal("http"))

		conn.ReadResponse()
	})

	It("doesn't overwrite X-Forwarded-Proto if present", func() {
		done := make(chan string)

		ln := registerHandler(r, "app", func(conn *test_util.HttpConn) {
			req, err := http.ReadRequest(conn.Reader)
			Expect(err).NotTo(HaveOccurred())

			resp := test_util.NewResponse(http.StatusOK)
			conn.WriteResponse(resp)
			conn.Close()

			done <- req.Header.Get("X-Forwarded-Proto")
		})
		defer ln.Close()

		conn := dialProxy(proxyServer)

		req := test_util.NewRequest("GET", "app", "/", nil)
		req.Header.Set("X-Forwarded-Proto", "https")
		conn.WriteRequest(req)

		var answer string
		Eventually(done).Should(Receive(&answer))
		Expect(answer).To(Equal("https"))

		conn.ReadResponse()
	})

	It("emits HTTP startstop events", func() {
		ln := registerHandlerWithInstanceId(r, "app", "", func(conn *test_util.HttpConn) {
		}, "fake-instance-id")
		defer ln.Close()

		conn := dialProxy(proxyServer)

		fakeEmitter := fake.NewFakeEventEmitter("fake")
		dropsonde.InitializeWithEmitter(fakeEmitter)

		req := test_util.NewRequest("GET", "app", "/", nil)
		requestId, err := uuid.NewV4()
		Expect(err).NotTo(HaveOccurred())
		req.Header.Set("X-Vcap-Request-Id", requestId.String())
		conn.WriteRequest(req)

		findStartStopEvent := func() *events.HttpStartStop {
			for _, event := range fakeEmitter.GetEvents() {
				startStopEvent, ok := event.(*events.HttpStartStop)
				if ok {
					return startStopEvent
				}
			}

			return nil
		}

		Eventually(findStartStopEvent).ShouldNot(BeNil())
		Expect(findStartStopEvent().GetParentRequestId()).To(Equal(factories.NewUUID(requestId)))

		conn.ReadResponse()
	})

	It("X-CF-InstanceID header is added with host:port information if NOT present in the routing endpoint", func() {
		done := make(chan string)

		ln := registerHandler(r, "app", func(conn *test_util.HttpConn) {
			req, err := http.ReadRequest(conn.Reader)
			Expect(err).NotTo(HaveOccurred())

			resp := test_util.NewResponse(http.StatusOK)
			conn.WriteResponse(resp)
			conn.Close()

			done <- req.Header.Get(router_http.CfInstanceIdHeader)
		})
		defer ln.Close()

		conn := dialProxy(proxyServer)

		req := test_util.NewRequest("GET", "app", "/", nil)
		conn.WriteRequest(req)

		var answer string
		Eventually(done).Should(Receive(&answer))
		Expect(answer).To(MatchRegexp(`^\d+(\.\d+){3}:\d+$`))

		conn.ReadResponse()
	})

	It("upgrades for a WebSocket request", func() {
		done := make(chan bool)

		ln := registerHandler(r, "ws", func(conn *test_util.HttpConn) {
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

		ln := registerHandler(r, "ws-cs-header", func(conn *test_util.HttpConn) {
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

		ln := registerHandler(r, "ws-cs-header", func(conn *test_util.HttpConn) {
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

	It("upgrades a Tcp request", func() {
		ln := registerHandler(r, "tcp-handler", func(conn *test_util.HttpConn) {
			conn.WriteLine("hello")
			conn.CheckLine("hello from client")
			conn.WriteLine("hello from server")
			conn.Close()
		})
		defer ln.Close()

		conn := dialProxy(proxyServer)

		req := test_util.NewRequest("GET", "tcp-handler", "/chat", nil)
		req.Header.Set("Upgrade", "tcp")

		req.Header.Set("Connection", "UpgradE")

		conn.WriteRequest(req)

		conn.CheckLine("hello")
		conn.WriteLine("hello from client")
		conn.CheckLine("hello from server")

		conn.Close()
	})

	It("transfers chunked encodings", func() {
		ln := registerHandler(r, "chunk", func(conn *test_util.HttpConn) {
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
		b := make([]byte, 16)
		for i := 0; i < 3; i++ {
			n, err := resp.Body.Read(b[0:])
			if err != nil {
				Expect(err).To(Equal(io.EOF))
			}
			Expect(n).To(Equal(5))
			Expect(string(b[0:n])).To(Equal("hello"))
		}
	})

	It("status no content was no Transfer Encoding response header", func() {
		ln := registerHandler(r, "not-modified", func(conn *test_util.HttpConn) {
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

	It("request terminates with slow response", func() {
		ln := registerHandler(r, "slow-app", func(conn *test_util.HttpConn) {
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
		Expect(time.Since(started)).To(BeNumerically("<", time.Duration(800*time.Millisecond)))
	})

	It("proxy detects closed client connection", func() {
		serverResult := make(chan error)
		ln := registerHandler(r, "slow-app", func(conn *test_util.HttpConn) {
			conn.CheckLine("GET / HTTP/1.1")

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

		conn.Conn.Close()

		var err error
		Eventually(serverResult).Should(Receive(&err))
		Expect(err).NotTo(BeNil())
	})

	Context("respect client keepalives", func() {
		It("closes the connection when told to close", func() {
			ln := registerHandler(r, "remote", func(conn *test_util.HttpConn) {
				http.ReadRequest(conn.Reader)
				resp := test_util.NewResponse(http.StatusOK)
				resp.Close = true
				conn.WriteResponse(resp)
				conn.Close()
			})
			defer ln.Close()

			conn := dialProxy(proxyServer)

			req := test_util.NewRequest("GET", "remote", "/", nil)
			req.Close = true
			conn.WriteRequest(req)
			resp, _ := conn.ReadResponse()
			Expect(resp.StatusCode).To(Equal(http.StatusOK))

			conn.WriteRequest(req)
			_, err := http.ReadResponse(conn.Reader, &http.Request{})
			Expect(err).To(HaveOccurred())
		})

		It("keeps the connection alive", func() {
			ln := registerHandler(r, "remote", func(conn *test_util.HttpConn) {
				http.ReadRequest(conn.Reader)
				resp := test_util.NewResponse(http.StatusOK)
				resp.Close = true
				conn.WriteResponse(resp)
				conn.Close()
			})
			defer ln.Close()

			conn := dialProxy(proxyServer)

			req := test_util.NewRequest("GET", "remote", "/", nil)
			req.Close = false
			conn.WriteRequest(req)
			resp, _ := conn.ReadResponse()
			Expect(resp.StatusCode).To(Equal(http.StatusOK))

			conn.WriteRequest(req)
			_, err := http.ReadResponse(conn.Reader, &http.Request{})
			Expect(err).ToNot(HaveOccurred())
		})

	})

	It("disables compression", func() {
		ln := registerHandler(r, "remote", func(conn *test_util.HttpConn) {
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

	It("retries when failed endpoints exist", func() {
		ln := registerHandler(r, "retries", func(conn *test_util.HttpConn) {
			conn.CheckLine("GET / HTTP/1.1")
			resp := test_util.NewResponse(http.StatusOK)
			conn.WriteResponse(resp)
			conn.Close()
		})
		defer ln.Close()

		ip, err := net.ResolveTCPAddr("tcp", "localhost:81")
		Expect(err).To(BeNil())
		registerAddr(r, "retries", "", ip, "instanceId")

		for i := 0; i < 5; i++ {
			conn := dialProxy(proxyServer)

			req := test_util.NewRequest("GET", "retries", "/", nil)
			conn.WriteRequest(req)
			resp, _ := conn.ReadResponse()

			Expect(resp.StatusCode).To(Equal(http.StatusOK))
		}
	})

	Context("Access log", func() {
		It("Logs a request", func() {
			ln := registerHandler(r, "test", func(conn *test_util.HttpConn) {
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
			})
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
			Expect(string(payload)).To(ContainSubstring(`app_id:`))
			Expect(payload[len(payload)-1]).To(Equal(byte('\n')))
		})

		It("Logs a request when it exits early", func() {
			conn := dialProxy(proxyServer)

			conn.WriteLines([]string{
				"GET / HTTP/0.9",
				"Host: test",
			})

			resp, _ := conn.ReadResponse()
			Expect(resp.StatusCode).To(Equal(http.StatusBadRequest))

			var payload []byte
			Eventually(func() int {
				n, e := accessLogFile.Read(&payload)
				Expect(e).ToNot(HaveOccurred())
				return n
			}).ShouldNot(BeZero())

			Expect(string(payload)).To(MatchRegexp("^test.*\n"))
		})

		Context("when the request is a TCP Upgrade", func() {
			It("Logs the response time", func() {

				ln := registerHandler(r, "tcp-handler", func(conn *test_util.HttpConn) {
					conn.WriteLine("hello")
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

				conn.CheckLine("hello")
				conn.WriteLine("hello from client")
				conn.CheckLine("hello from server")

				var payload []byte
				Eventually(func() int {
					accessLogFile.Read(&payload)
					return len(payload)
				}).ShouldNot(BeZero())

				Expect(string(payload)).To(ContainSubstring(`response_time:`))
				responseTime := parseResponseTimeFromLog(string(payload))
				Expect(responseTime).To(BeNumerically(">", 0))

				conn.Close()
			})
		})

		Context("when the request is a web connection", func() {
			It("Logs the response time", func() {
				done := make(chan bool)
				ln := registerHandler(r, "ws", func(conn *test_util.HttpConn) {
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
				responseTime := parseResponseTimeFromLog(string(payload))
				Expect(responseTime).To(BeNumerically(">", 0))

				conn.Close()
			})
		})
	})

	Context("when the endpoint is nil", func() {
		It("responds with a 502 BadGateway", func() {
			ln := registerHandler(r, "nil-endpoint", func(conn *test_util.HttpConn) {
				conn.CheckLine("GET / HTTP/1.1")
				resp := test_util.NewResponse(http.StatusOK)
				conn.WriteResponse(resp)
				conn.Close()
			})
			defer ln.Close()

			pool := r.Lookup(route.Uri("nil-endpoint"))
			endpoints := make([]*route.Endpoint, 0)
			pool.Each(func(e *route.Endpoint) {
				endpoints = append(endpoints, e)
			})
			for _, e := range endpoints {
				pool.Remove(e)
			}

			conn := dialProxy(proxyServer)

			req := test_util.NewRequest("GET", "nil-endpoint", "/", nil)
			conn.WriteRequest(req)

			b := make([]byte, 0, 0)
			buf := bytes.NewBuffer(b)
			log.SetOutput(buf)
			res, _ := conn.ReadResponse()
			log.SetOutput(os.Stderr)
			Expect(buf).NotTo(ContainSubstring("multiple response.WriteHeader calls"))
			Expect(res.StatusCode).To(Equal(http.StatusBadGateway))
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

func registerAddr(reg *registry.RouteRegistry, path string, routeServiceUrl string, addr net.Addr, instanceId string) {
	host, portStr, err := net.SplitHostPort(addr.String())
	Expect(err).NotTo(HaveOccurred())

	port, err := strconv.Atoi(portStr)
	Expect(err).NotTo(HaveOccurred())

	reg.Register(route.Uri(path), route.NewEndpoint("", host, uint16(port), instanceId, nil, -1, routeServiceUrl))
}

func registerHandler(reg *registry.RouteRegistry, path string, handler connHandler) net.Listener {
	return registerHandlerWithInstanceId(reg, path, "", handler, "")
}

func registerHandlerWithRouteService(reg *registry.RouteRegistry, path string, routeServiceUrl string, handler connHandler) net.Listener {
	return registerHandlerWithInstanceId(reg, path, routeServiceUrl, handler, "")
}

func registerHandlerWithInstanceId(reg *registry.RouteRegistry, path string, routeServiceUrl string, handler connHandler, instanceId string) net.Listener {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	Expect(err).NotTo(HaveOccurred())

	go runBackendInstance(ln, handler)

	registerAddr(reg, path, routeServiceUrl, ln.Addr(), instanceId)

	return ln
}

func runBackendInstance(ln net.Listener, handler connHandler) {
	var tempDelay time.Duration // how long to sleep on accept failure
	for {
		conn, err := ln.Accept()
		if err != nil {
			if ne, ok := err.(net.Error); ok && ne.Temporary() {
				if tempDelay == 0 {
					tempDelay = 5 * time.Millisecond
				} else {
					tempDelay *= 2
				}
				if max := 1 * time.Second; tempDelay > max {
					tempDelay = max
				}
				fmt.Printf("http: Accept error: %v; retrying in %v\n", err, tempDelay)
				time.Sleep(tempDelay)
				continue
			}
			break
		}
		go func() {
			defer GinkgoRecover()
			handler(test_util.NewHttpConn(conn))
		}()
	}
}

func dialProxy(proxyServer net.Listener) *test_util.HttpConn {
	conn, err := net.Dial("tcp", proxyServer.Addr().String())
	Expect(err).NotTo(HaveOccurred())

	return test_util.NewHttpConn(conn)
}

func newTlsListener(listener net.Listener) net.Listener {
	cert, err := tls.LoadX509KeyPair("../test/assets/public.pem", "../test/assets/private.pem")
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
