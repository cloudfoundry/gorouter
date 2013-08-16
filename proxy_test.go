package router

import (
	"bufio"
	"encoding/json"
	"io"
	"io/ioutil"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/cloudfoundry/go_cfmessagebus/mock_cfmessagebus"
	. "launchpad.net/gocheck"
)

type connHandler func(*conn)

type nullVarz struct{}

func (_ nullVarz) MarshalJSON() ([]byte, error) { return json.Marshal(nil) }

func (_ nullVarz) CaptureBadRequest(req *http.Request)                                          {}
func (_ nullVarz) CaptureRoutingRequest(b *RouteEndpoint, req *http.Request)                    {}
func (_ nullVarz) CaptureRoutingResponse(b *RouteEndpoint, res *http.Response, d time.Duration) {}

type conn struct {
	net.Conn

	c *C

	reader *bufio.Reader
	writer *bufio.Writer
}

func newConn(x net.Conn, c *C) *conn {
	return &conn{
		Conn:   x,
		c:      c,
		reader: bufio.NewReader(x),
		writer: bufio.NewWriter(x),
	}
}

func (x *conn) ReadRequest() (*http.Request, string) {
	req, err := http.ReadRequest(x.reader)
	x.c.Assert(err, IsNil)

	b, err := ioutil.ReadAll(req.Body)
	x.c.Assert(err, IsNil)

	return req, string(b)
}

func (x *conn) NewRequest(method, urlStr string, body io.Reader) *http.Request {
	req, err := http.NewRequest(method, urlStr, body)
	x.c.Assert(err, IsNil)
	return req
}

func (x *conn) WriteRequest(req *http.Request) {
	err := req.Write(x.writer)
	x.c.Assert(err, IsNil)
	x.writer.Flush()
}

func (x *conn) ReadResponse() (*http.Response, string) {
	resp, err := http.ReadResponse(x.reader, &http.Request{})
	x.c.Assert(err, IsNil)

	b, err := ioutil.ReadAll(resp.Body)
	x.c.Assert(err, IsNil)

	return resp, string(b)
}

func newResponse(status int) *http.Response {
	return &http.Response{
		StatusCode: status,
		ProtoMajor: 1,
		ProtoMinor: 1,
		Header:     make(http.Header),
	}
}

func (x *conn) WriteResponse(resp *http.Response) {
	err := resp.Write(x.writer)
	x.c.Assert(err, IsNil)
	x.writer.Flush()
}

func (x *conn) CheckLine(expected string) {
	l, err := x.reader.ReadString('\n')
	x.c.Check(err, IsNil)
	x.c.Check(strings.TrimRight(l, "\r\n"), Equals, expected)
}

func (x *conn) CheckLines(expected []string) {
	for _, e := range expected {
		x.CheckLine(e)
	}

	x.CheckLine("")
}

func (x *conn) WriteLine(line string) {
	x.writer.WriteString(line)
	x.writer.WriteString("\r\n")
	x.writer.Flush()
}

func (x *conn) WriteLines(lines []string) {
	for _, e := range lines {
		x.WriteLine(e)
	}

	x.WriteLine("")
}

type ProxySuite struct {
	r *Registry
	p *Proxy

	proxyServer net.Listener

	// This channel is closed when the test is done
	done chan bool
}

var _ = Suite(&ProxySuite{})

func (s *ProxySuite) SetUpTest(c *C) {
	x := DefaultConfig()
	x.TraceKey = "my_trace_key"

	mbus := mock_cfmessagebus.NewMockMessageBus()
	s.r = NewRegistry(x, mbus)
	s.p = NewProxy(x, s.r, nullVarz{})

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		panic(err)
	}

	go http.Serve(ln, s.p)

	s.proxyServer = ln
}

func (s *ProxySuite) TearDownTest(c *C) {
	s.proxyServer.Close()
}

func (s *ProxySuite) registerAddr(u string, a net.Addr) {
	h, p, err := net.SplitHostPort(a.String())
	if err != nil {
		panic(err)
	}

	x, err := strconv.Atoi(p)
	if err != nil {
		panic(err)
	}

	s.r.Register(&RouteEndpoint{
		Host: h,
		Port: uint16(x),
		Uris: []Uri{Uri(u)},
	})
}

func (s *ProxySuite) RegisterHandler(c *C, u string, h connHandler) net.Listener {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		panic(err)
	}

	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				break
			}

			// there's a race in net/http transport.go between Transport.getConn and Transport.roundTrip;
			// if the request is sent before .roundTrip is called, .readLoop will be very uncouth
			time.Sleep(50 * time.Millisecond)

			go h(newConn(conn, c))
		}
	}()

	s.registerAddr(u, ln.Addr())

	return ln
}

func (s *ProxySuite) DialProxy(c *C) *conn {
	x, err := net.Dial("tcp", s.proxyServer.Addr().String())
	if err != nil {
		panic(err)
	}

	return newConn(x, c)
}

func (s *ProxySuite) TestRespondsToHttp10(c *C) {
	s.RegisterHandler(c, "test", func(x *conn) {
		x.CheckLine("GET / HTTP/1.1")

		x.WriteLines([]string{
			"HTTP/1.1 200 OK",
			"Content-Length: 0",
		})
	})

	x := s.DialProxy(c)

	x.WriteLines([]string{
		"GET / HTTP/1.0",
		"Host: test",
	})

	x.CheckLine("HTTP/1.0 200 OK")
}

func (s *ProxySuite) TestRespondsToHttp11(c *C) {
	s.RegisterHandler(c, "test", func(x *conn) {
		x.CheckLine("GET / HTTP/1.1")

		x.WriteLines([]string{
			"HTTP/1.1 200 OK",
			"Content-Length: 0",
		})
	})

	x := s.DialProxy(c)

	x.WriteLines([]string{
		"GET / HTTP/1.1",
		"Host: test",
	})

	x.CheckLine("HTTP/1.1 200 OK")
}

func (s *ProxySuite) TestDoesNotRespondToUnsupportedHttp(c *C) {
	x := s.DialProxy(c)

	x.WriteLines([]string{
		"GET / HTTP/0.9",
		"Host: test",
	})

	x.CheckLine("HTTP/1.0 400 Bad Request")
}

func (s *ProxySuite) TestRespondsToLoadBalancerCheck(c *C) {
	x := s.DialProxy(c)

	req := x.NewRequest("GET", "/", nil)
	req.Header.Set("User-Agent", "HTTP-Monitor/1.1")
	x.WriteRequest(req)

	_, body := x.ReadResponse()
	c.Check(body, Equals, "ok\n")
}

func (s *ProxySuite) TestRespondsToUnknownHostWith404(c *C) {
	x := s.DialProxy(c)

	req := x.NewRequest("GET", "/", nil)
	req.Header.Set("Host", "unknown")
	x.WriteRequest(req)

	resp, body := x.ReadResponse()
	c.Check(resp.StatusCode, Equals, http.StatusNotFound)
	c.Check(body, Equals, "404 Not Found\n")
}

func (s *ProxySuite) TestRespondsToMisbehavingHostWith502(c *C) {
	s.RegisterHandler(c, "enfant-terrible", func(x *conn) {
		x.Close()
	})

	x := s.DialProxy(c)

	req := x.NewRequest("GET", "/", nil)
	req.Host = "enfant-terrible"
	x.WriteRequest(req)

	resp, body := x.ReadResponse()
	c.Check(resp.StatusCode, Equals, http.StatusBadGateway)
	c.Check(body, Equals, "502 Bad Gateway\n")
}

func (s *ProxySuite) TestTraceHeadersAddedOnCorrectTraceKey(c *C) {
	ln := s.RegisterHandler(c, "trace-test", func(x *conn) {
		resp := newResponse(http.StatusOK)
		x.WriteResponse(resp)
		x.Close()
	})

	x := s.DialProxy(c)

	req := x.NewRequest("GET", "/", nil)
	req.Host = "trace-test"
	req.Header.Set("X-Vcap-Trace", "my_trace_key")
	x.WriteRequest(req)

	resp, _ := x.ReadResponse()
	c.Check(resp.Header.Get("X-Vcap-Backend"), Equals, ln.Addr().String())
	c.Check(resp.Header.Get("X-Cf-RouteEndpoint"), Equals, ln.Addr().String())
	c.Check(resp.Header.Get("X-Vcap-Router"), Equals, s.p.Config.Ip)
}

func (s *ProxySuite) TestTraceHeadersNotAddedOnIncorrectTraceKey(c *C) {
	s.RegisterHandler(c, "trace-test", func(x *conn) {
		resp := newResponse(http.StatusOK)
		x.WriteResponse(resp)
		x.Close()
	})

	x := s.DialProxy(c)

	req := x.NewRequest("GET", "/", nil)
	req.Host = "trace-test"
	req.Header.Set("X-Vcap-Trace", "a_bad_trace_key")
	x.WriteRequest(req)

	resp, _ := x.ReadResponse()
	c.Check(resp.Header.Get("X-Vcap-Backend"), Equals, "")
	c.Check(resp.Header.Get("X-Cf-RouteEndpoint"), Equals, "")
	c.Check(resp.Header.Get("X-Vcap-Router"), Equals, "")
}

func (s *ProxySuite) TestXFFIsAdded(c *C) {
	done := make(chan bool)

	s.RegisterHandler(c, "app", func(x *conn) {
		req, _ := x.ReadRequest()
		c.Check(req.Header.Get("X-Forwarded-For"), Equals, "127.0.0.1")
		done <- true
	})

	x := s.DialProxy(c)

	req := x.NewRequest("GET", "/", nil)
	req.Host = "app"
	x.WriteRequest(req)

	<-done
}

func (s *ProxySuite) TestXFFIsAppended(c *C) {
	done := make(chan bool)

	s.RegisterHandler(c, "app", func(x *conn) {
		req, _ := x.ReadRequest()
		c.Check(req.Header.Get("X-Forwarded-For"), Equals, "1.2.3.4, 127.0.0.1")
		done <- true
	})

	x := s.DialProxy(c)

	req := x.NewRequest("GET", "/", nil)
	req.Host = "app"
	req.Header.Add("X-Forwarded-For", "1.2.3.4")
	x.WriteRequest(req)

	<-done
}

func (s *ProxySuite) TestWebSocketUpgrade(c *C) {
	s.RegisterHandler(c, "ws", func(x *conn) {
		req, _ := x.ReadRequest()
		c.Check(req.Header.Get("Upgrade"), Equals, "websocket")
		c.Check(req.Header.Get("Connection"), Equals, "Upgrade")

		resp := newResponse(http.StatusSwitchingProtocols)
		resp.Header.Set("Upgrade", "websocket")
		resp.Header.Set("Connection", "Upgrade")

		x.WriteResponse(resp)

		x.CheckLine("hello from client")
		x.WriteLine("hello from server")
	})

	x := s.DialProxy(c)

	req := x.NewRequest("GET", "/chat", nil)
	req.Host = "ws"
	req.Header.Set("Upgrade", "websocket")
	req.Header.Set("Connection", "Upgrade")

	x.WriteRequest(req)

	resp, _ := x.ReadResponse()
	c.Check(resp.StatusCode, Equals, http.StatusSwitchingProtocols)
	c.Check(resp.Header.Get("Upgrade"), Equals, "websocket")
	c.Check(resp.Header.Get("Connection"), Equals, "Upgrade")

	x.WriteLine("hello from client")
	x.CheckLine("hello from server")
}

func (s *ProxySuite) TestTcpUpgrade(c *C) {
	s.RegisterHandler(c, "tcp-handler", func(x *conn) {
		x.WriteLine("hello")
		x.CheckLine("hello from client")
		x.WriteLine("hello from server")
	})

	x := s.DialProxy(c)

	req := x.NewRequest("GET", "/chat", nil)
	req.Host = "tcp-handler"
	req.Header.Set("Upgrade", "tcp")
	req.Header.Set("Connection", "Upgrade")

	x.WriteRequest(req)

	x.CheckLine("hello")
	x.WriteLine("hello from client")
	x.CheckLine("hello from server")
}

func (s *ProxySuite) TestTransferEncodingChunked(c *C) {
	s.RegisterHandler(c, "chunk", func(responseDestination *conn) {
		r, w := io.Pipe()

		// Write 3 times on a 100ms interval
		go func() {
			t := time.NewTicker(100 * time.Millisecond)
			defer t.Stop()
			defer w.Close()

			for i := 0; i < 3; i++ {
				<-t.C
				_, err := w.Write([]byte("hello"))
				c.Assert(err, IsNil)
			}
		}()

		resp := newResponse(http.StatusOK)
		resp.TransferEncoding = []string{"chunked"}
		resp.Body = r
		resp.Write(responseDestination)
	})

	x := s.DialProxy(c)

	req := x.NewRequest("GET", "/", nil)
	req.Host = "chunk"

	err := req.Write(x)
	c.Assert(err, IsNil)

	resp, err := http.ReadResponse(x.reader, &http.Request{})
	c.Assert(err, IsNil)

	c.Assert(resp.StatusCode, Equals, http.StatusOK)
	c.Assert(resp.TransferEncoding, DeepEquals, []string{"chunked"})

	// Expect 3 individual reads to complete
	for i := 0; i < 3; i++ {
		var b [16]byte

		n, err := resp.Body.Read(b[0:])
		c.Assert(err, IsNil)
		c.Check(n, Equals, 5)
		c.Check(string(b[0:n]), Equals, "hello")
	}
}
