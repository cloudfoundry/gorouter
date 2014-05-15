package test_util

import (
	. "github.com/onsi/gomega"
	"io/ioutil"
	"net/url"
	"strings"

	"bufio"
	"io"
	"net"
	"net/http"
)

type HttpConn struct {
	net.Conn

	Reader *bufio.Reader
	Writer *bufio.Writer
}

func NewHttpConn(x net.Conn) *HttpConn {
	return &HttpConn{
		Conn:   x,
		Reader: bufio.NewReader(x),
		Writer: bufio.NewWriter(x),
	}
}

func (x *HttpConn) ReadRequest() (*http.Request, string) {
	req, err := http.ReadRequest(x.Reader)
	Ω(err).NotTo(HaveOccurred())

	b, err := ioutil.ReadAll(req.Body)
	Ω(err).NotTo(HaveOccurred())

	return req, string(b)
}

func (x *HttpConn) NewRequest(method, urlStr string, body io.Reader) *http.Request {
	req, err := http.NewRequest(method, urlStr, body)
	Ω(err).NotTo(HaveOccurred())
	req.URL = &url.URL{Host: req.URL.Host, Opaque: urlStr}
	return req
}

func (x *HttpConn) WriteRequest(req *http.Request) {
	err := req.Write(x.Writer)
	Ω(err).NotTo(HaveOccurred())
	x.Writer.Flush()
}

func (x *HttpConn) ReadResponse() (*http.Response, string) {
	resp, err := http.ReadResponse(x.Reader, &http.Request{})
	Ω(err).NotTo(HaveOccurred())

	b, err := ioutil.ReadAll(resp.Body)
	Ω(err).NotTo(HaveOccurred())

	return resp, string(b)
}

func NewResponse(status int) *http.Response {
	return &http.Response{
		StatusCode: status,
		ProtoMajor: 1,
		ProtoMinor: 1,
		Header:     make(http.Header),
	}
}

func (x *HttpConn) WriteResponse(resp *http.Response) {
	err := resp.Write(x.Writer)
	Ω(err).NotTo(HaveOccurred())
	x.Writer.Flush()
}

func (x *HttpConn) CheckLine(expected string) {
	l, err := x.Reader.ReadString('\n')
	Ω(err).NotTo(HaveOccurred())
	Ω(strings.TrimRight(l, "\r\n")).To(Equal(expected))
}

func (x *HttpConn) CheckLines(expected []string) {
	for _, e := range expected {
		x.CheckLine(e)
	}

	x.CheckLine("")
}

func (x *HttpConn) WriteLine(line string) {
	x.Writer.WriteString(line)
	x.Writer.WriteString("\r\n")
	x.Writer.Flush()
}

func (x *HttpConn) WriteLines(lines []string) {
	for _, e := range lines {
		x.WriteLine(e)
	}

	x.WriteLine("")
}
