package test_util

import (
	"bufio"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"

	. "github.com/onsi/gomega"
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
	ExpectWithOffset(1, err).NotTo(HaveOccurred())
	defer req.Body.Close()

	b, err := io.ReadAll(req.Body)
	ExpectWithOffset(1, err).NotTo(HaveOccurred())

	return req, string(b)
}

func (x *HttpConn) WriteRequest(req *http.Request) {
	err := req.Write(x.Writer)
	ExpectWithOffset(1, err).NotTo(HaveOccurred())
	// #nosec G104 - ignore errors when flushing HTTP responses because otherwise it masks our ability to validate the response that the handler is sending
	x.Writer.Flush()
}

func (x *HttpConn) ReadResponse() (*http.Response, string) {
	resp, err := http.ReadResponse(x.Reader, &http.Request{})
	ExpectWithOffset(1, err).NotTo(HaveOccurred())
	defer resp.Body.Close()

	b, err := io.ReadAll(resp.Body)
	ExpectWithOffset(1, err).NotTo(HaveOccurred())

	return resp, string(b)
}

func NewResponse(status int) *http.Response {
	headers := make(http.Header)
	// Our test handlers close the connection because they don't read multiple
	// requests from the stream.  But this leaves a dangling connection to a closed
	// network socket in the backend's connetion pool, unless we set Connection: close on our
	// response
	headers.Set("Connection", "close")
	return &http.Response{
		StatusCode: status,
		ProtoMajor: 1,
		ProtoMinor: 1,
		Header:     headers,
	}
}

func (x *HttpConn) WriteResponse(resp *http.Response) {
	err := resp.Write(x.Writer)
	ExpectWithOffset(1, err).NotTo(HaveOccurred())
	// #nosec G104 - ignore errors when flushing HTTP responses because otherwise it masks our ability to validate the response that the handler is sending
	x.Writer.Flush()
}

func (x *HttpConn) CheckLine(expected string) {
	l, err := x.Reader.ReadString('\n')
	ExpectWithOffset(1, err).NotTo(HaveOccurred())
	ExpectWithOffset(1, strings.TrimRight(l, "\r\n")).To(Equal(expected))
}

func (x *HttpConn) CheckLines(expected []string) {
	for _, e := range expected {
		x.CheckLine(e)
	}

	x.CheckLine("")
}

func (x *HttpConn) WriteLine(line string) error {
	_, err := x.Writer.WriteString(line)
	if err != nil {
		return err
	}
	_, err = x.Writer.WriteString("\r\n")
	if err != nil {
		return err
	}
	// #nosec G104 - ignore errors when flushing HTTP responses because otherwise it masks our ability to validate the response that the handler is sending
	return x.Writer.Flush()
}

func (x *HttpConn) WriteLines(lines []string) error {
	for _, e := range lines {
		err := x.WriteLine(e)
		if err != nil {
			return err
		}
	}

	return x.WriteLine("")
}

func NewRequest(method, host, rawPath string, body io.Reader) *http.Request {
	req, err := http.NewRequest(method, "http://"+host+rawPath, body)
	ExpectWithOffset(1, err).NotTo(HaveOccurred())

	req.URL = &url.URL{Scheme: "http", Host: host, Opaque: rawPath}
	req.Host = host
	return req
}
