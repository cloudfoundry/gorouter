package test_util

import (
	"io"
	"io/ioutil"
	"strings"

	. "github.com/onsi/gomega"

	"bufio"
	"net"
	"net/http"
	"net/url"
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
	Expect(err).NotTo(HaveOccurred())
	defer req.Body.Close()

	b, err := ioutil.ReadAll(req.Body)
	Expect(err).NotTo(HaveOccurred())

	return req, string(b)
}

func (x *HttpConn) WriteRequest(req *http.Request) {
	err := req.Write(x.Writer)
	ExpectWithOffset(1, err).NotTo(HaveOccurred())
	x.Writer.Flush()
}

func (x *HttpConn) ReadResponse() (*http.Response, string) {
	resp, err := http.ReadResponse(x.Reader, &http.Request{})
	ExpectWithOffset(1, err).NotTo(HaveOccurred())
	defer resp.Body.Close()

	b, err := ioutil.ReadAll(resp.Body)
	ExpectWithOffset(1, err).NotTo(HaveOccurred())

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
	ExpectWithOffset(1, err).NotTo(HaveOccurred())
	x.Writer.Flush()
}

func (x *HttpConn) CheckLine(expected string) {
	l, err := x.Reader.ReadString('\n')
	Expect(err).NotTo(HaveOccurred())
	Expect(strings.TrimRight(l, "\r\n")).To(Equal(expected))
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

func NewRequest(method, host, rawPath string, body io.Reader) *http.Request {
	req, err := http.NewRequest(method, "http://"+host+rawPath, body)
	ExpectWithOffset(1, err).NotTo(HaveOccurred())

	req.URL = &url.URL{Scheme: "http", Host: host, Opaque: rawPath}
	req.Host = host
	return req
}
