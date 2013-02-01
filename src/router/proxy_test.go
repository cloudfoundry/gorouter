package router

import (
	"bufio"
	"encoding/json"
	"io"
	"io/ioutil"
	. "launchpad.net/gocheck"
	"net"
	"net/http"
	"router/config"
	"strconv"
	"strings"
	"time"
)

type connHandler func(*conn)

type nullVarz struct{}

func (_ nullVarz) MarshalJSON() ([]byte, error) { return json.Marshal(nil) }

func (_ nullVarz) CaptureBadRequest(req *http.Request)                                   {}
func (_ nullVarz) CaptureBackendRequest(b Backend, req *http.Request)                    {}
func (_ nullVarz) CaptureBackendResponse(b Backend, res *http.Response, d time.Duration) {}

type conn struct {
	x net.Conn
	c *C

	*bufio.Reader
	*bufio.Writer
}

func newConn(x net.Conn, c *C) *conn {
	return &conn{
		x:      x,
		c:      c,
		Reader: bufio.NewReader(x),
		Writer: bufio.NewWriter(x),
	}
}

func (x *conn) NewRequest(method, urlStr string, body io.Reader) *http.Request {
	req, err := http.NewRequest(method, urlStr, body)
	x.c.Assert(err, IsNil)
	return req
}

func (x *conn) WriteRequest(req *http.Request) {
	err := req.Write(x)
	x.c.Assert(err, IsNil)
	x.Flush()
}

func (x *conn) ReadResponse() (*http.Response, string) {
	resp, err := http.ReadResponse(x.Reader, &http.Request{})
	x.c.Assert(err, IsNil)

	b, err := ioutil.ReadAll(resp.Body)
	x.c.Assert(err, IsNil)

	return resp, string(b)
}

func (x *conn) CheckLine(expected string) {
	l, err := x.ReadString('\n')
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
	x.WriteString(line)
	x.WriteString("\r\n")
	x.Flush()
}

func (x *conn) WriteLines(lines []string) {
	for _, e := range lines {
		x.WriteLine(e)
	}

	x.WriteLine("")
}

type ProxySuite struct {
	*C

	r *Registry
	p *Proxy

	// This channel is closed when the test is done
	done chan bool
}

var _ = Suite(&ProxySuite{})

func (s *ProxySuite) SetUpTest(c *C) {
	s.r = NewRegistry(config.DefaultConfig())
	s.p = NewProxy(s.r, nullVarz{})

	s.done = make(chan bool)
}

func (s *ProxySuite) TearDownTest(c *C) {
	close(s.done)
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

	m := registerMessage{
		Host: h,
		Port: uint16(x),
		Uris: []Uri{Uri(u)},
	}

	s.r.Register(&m)
}

func (s *ProxySuite) RegisterHandler(u string, h connHandler) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		panic(err)
	}

	// Close listener when test is done
	go func() {
		<-s.done
		ln.Close()
	}()

	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				break
			}

			go h(newConn(conn, s.C))
		}
	}()

	s.registerAddr(u, ln.Addr())
}

func (s *ProxySuite) StartProxy() net.Addr {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		panic(err)
	}

	// Close listener when test is done
	go func() {
		<-s.done
		ln.Close()
	}()

	go func() {
		http.Serve(ln, s.p)
	}()

	return ln.Addr()
}

func (s *ProxySuite) DialProxy() *conn {
	y := s.StartProxy()

	x, err := net.Dial("tcp", y.String())
	if err != nil {
		panic(err)
	}

	return newConn(x, s.C)
}

func (s *ProxySuite) TestRespondsToHttp10(c *C) {
	s.C = c

	s.RegisterHandler("test", func(x *conn) {
		x.CheckLine("GET / HTTP/1.1")

		x.WriteLines([]string{
			"HTTP/1.1 200 OK",
			"Content-Length: 0",
		})
	})

	x := s.DialProxy()

	x.WriteLines([]string{
		"GET / HTTP/1.0",
		"Host: test",
	})

	x.CheckLine("HTTP/1.0 200 OK")
}

func (s *ProxySuite) TestRespondsToHttp11(c *C) {
	s.C = c

	s.RegisterHandler("test", func(x *conn) {
		x.CheckLine("GET / HTTP/1.1")

		x.WriteLines([]string{
			"HTTP/1.1 200 OK",
			"Content-Length: 0",
		})
	})

	x := s.DialProxy()

	x.WriteLines([]string{
		"GET / HTTP/1.1",
		"Host: test",
	})

	x.CheckLine("HTTP/1.1 200 OK")
}

func (s *ProxySuite) TestDoesNotRespondToUnsupportedHttp(c *C) {
	s.C = c

	x := s.DialProxy()

	x.WriteLines([]string{
		"GET / HTTP/0.9",
		"Host: test",
	})

	x.CheckLine("HTTP/1.0 400 Bad Request")
}

func (s *ProxySuite) TestRespondsToLoadBalancerCheck(c *C) {
	s.C = c

	x := s.DialProxy()

	req := x.NewRequest("GET", "/", nil)
	req.Header.Set("User-Agent", "HTTP-Monitor/1.1")
	x.WriteRequest(req)

	_, body := x.ReadResponse()
	s.Check(body, Equals, "ok\n")
}
