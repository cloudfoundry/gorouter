// Copyright 2009 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// HTTP server.  See RFC 2616.

// TODO(rsc):
//	logging

package http

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net"
	"net/http"
	"net/url"
	"path"
	"runtime/debug"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Errors introduced by the HTTP server.
var (
	ErrWriteAfterFlush = errors.New("Conn.Write called after Flush")
	ErrBodyNotAllowed  = errors.New("http: request method or response status code does not allow body")
	ErrHijacked        = errors.New("Conn has been hijacked")
	ErrContentLength   = errors.New("Conn.Write wrote more than the declared Content-Length")
)

// A ResponseWriter interface is used by an HTTP handler to
// construct an HTTP response.
type ResponseWriter interface {
	// Header returns the header map that will be sent by WriteHeader.
	// Changing the header after a call to WriteHeader (or Write) has
	// no effect.
	Header() http.Header

	// Write writes the data to the connection as part of an HTTP reply.
	// If WriteHeader has not yet been called, Write calls WriteHeader(http.StatusOK)
	// before writing the data.  If the Header does not contain a
	// Content-Type line, Write adds a Content-Type set to the result of passing
	// the initial 512 bytes of written data to DetectContentType.
	Write([]byte) (int, error)

	// WriteHeader sends an HTTP response header with status code.
	// If WriteHeader is not called explicitly, the first call to Write
	// will trigger an implicit WriteHeader(http.StatusOK).
	// Thus explicit calls to WriteHeader are mainly used to
	// send error codes.
	WriteHeader(int)
}

// The Flusher interface is implemented by ResponseWriters that allow
// an HTTP handler to flush buffered data to the client.
//
// Note that even for ResponseWriters that support Flush,
// if the client is connected through an HTTP proxy,
// the buffered data may not reach the client until the response
// completes.
type Flusher interface {
	// Flush sends any buffered data to the client.
	Flush()
}

// The Hijacker interface is implemented by ResponseWriters that allow
// an HTTP handler to take over the connection.
type Hijacker interface {
	// Hijack lets the caller take over the connection.
	// After a call to Hijack(), the HTTP server library
	// will not do anything else with the connection.
	// It becomes the caller's responsibility to manage
	// and close the connection.
	Hijack() (net.Conn, *bufio.ReadWriter, error)
}

// A conn represents the server side of an HTTP connection.
type conn struct {
	remoteAddr string            // network address of remote side
	server     *Server           // the Server on which the connection arrived
	rwc        net.Conn          // i/o connection
	lr         *io.LimitedReader // io.LimitReader(rwc)
	buf        *bufio.ReadWriter // buffered(lr,rwc), reading from bufio->limitReader->rwc
	hijacked   bool              // connection has been hijacked by handler
}

// A response represents the server side of an HTTP response.
type response struct {
	conn          *conn
	req           *http.Request // request for this response
	chunking      bool          // using chunked transfer encoding for reply body
	wroteHeader   bool          // reply header has been written
	wroteContinue bool          // 100 Continue response was written
	header        http.Header   // reply header parameters
	written       int64         // number of bytes written in body
	contentLength int64         // explicitly-declared Content-Length; or -1
	status        int           // status code passed to WriteHeader

	// close connection after this reply.  set on request and
	// updated after response from handler if there's a
	// "Connection: keep-alive" response header and a
	// Content-Length.
	closeAfterReply bool

	// requestBodyLimitHit is set by requestTooLarge when
	// maxBytesReader hits its max size. It is checked in
	// WriteHeader, to make sure we don't consume the the
	// remaining request body to try to advance to the next HTTP
	// request. Instead, when this is set, we stop doing
	// subsequent requests on this connection and stop reading
	// input from it.
	requestBodyLimitHit bool
}

// requestTooLarge is called by maxBytesReader when too much input has
// been read from the client.
func (w *response) requestTooLarge() {
	w.closeAfterReply = true
	w.requestBodyLimitHit = true
	if !w.wroteHeader {
		w.Header().Set("Connection", "close")
	}
}

type writerOnly struct {
	io.Writer
}

func (w *response) ReadFrom(src io.Reader) (n int64, err error) {
	// Call WriteHeader before checking w.chunking if it hasn't
	// been called yet, since WriteHeader is what sets w.chunking.
	if !w.wroteHeader {
		w.WriteHeader(http.StatusOK)
	}
	if !w.chunking && w.bodyAllowed() {
		w.Flush()
		if rf, ok := w.conn.rwc.(io.ReaderFrom); ok {
			n, err = rf.ReadFrom(src)
			w.written += n
			return
		}
	}
	// Fall back to default io.Copy implementation.
	// Use wrapper to hide w.ReadFrom from io.Copy.
	return io.Copy(writerOnly{w}, src)
}

// noLimit is an effective infinite upper bound for io.LimitedReader
const noLimit int64 = (1 << 63) - 1

// Create new connection from rwc.
func (srv *Server) newConn(rwc net.Conn) (c *conn, err error) {
	c = new(conn)
	c.remoteAddr = rwc.RemoteAddr().String()
	c.server = srv
	c.rwc = rwc
	c.lr = io.LimitReader(rwc, noLimit).(*io.LimitedReader)
	br := bufio.NewReader(c.lr)
	bw := bufio.NewWriter(rwc)
	c.buf = bufio.NewReadWriter(br, bw)
	return c, nil
}

// DefaultMaxHeaderBytes is the maximum permitted size of the headers
// in an HTTP request.
// This can be overridden by setting Server.MaxHeaderBytes.
const DefaultMaxHeaderBytes = 1 << 20 // 1 MB

func (srv *Server) maxHeaderBytes() int {
	if srv.MaxHeaderBytes > 0 {
		return srv.MaxHeaderBytes
	}
	return DefaultMaxHeaderBytes
}

// wrapper around io.ReaderCloser which on first read, sends an
// HTTP/1.1 100 Continue header
type expectContinueReader struct {
	resp       *response
	readCloser io.ReadCloser
	closed     bool
}

func (ecr *expectContinueReader) Read(p []byte) (n int, err error) {
	if ecr.closed {
		return 0, errors.New("http: Read after Close on request Body")
	}
	if !ecr.resp.wroteContinue && !ecr.resp.conn.hijacked {
		ecr.resp.wroteContinue = true
		io.WriteString(ecr.resp.conn.buf, "HTTP/1.1 100 Continue\r\n\r\n")
		ecr.resp.conn.buf.Flush()
	}
	return ecr.readCloser.Read(p)
}

func (ecr *expectContinueReader) Close() error {
	ecr.closed = true
	return ecr.readCloser.Close()
}

// TimeFormat is the time format to use with
// time.Parse and time.Time.Format when parsing
// or generating times in HTTP headers.
// It is like time.RFC1123 but hard codes GMT as the time zone.
const TimeFormat = "Mon, 02 Jan 2006 15:04:05 GMT"

var errTooLarge = errors.New("http: request too large")

// Read next request from connection.
func (c *conn) readRequest() (w *response, err error) {
	if c.hijacked {
		return nil, ErrHijacked
	}
	c.lr.N = int64(c.server.maxHeaderBytes()) + 4096 /* bufio slop */
	var req *http.Request
	if req, err = ReadRequest(c.buf.Reader); err != nil {
		if c.lr.N == 0 {
			return nil, errTooLarge
		}
		return nil, err
	}
	c.lr.N = noLimit

	req.RemoteAddr = c.remoteAddr

	w = new(response)
	w.conn = c
	w.req = req
	w.header = make(http.Header)
	w.contentLength = -1
	return w, nil
}

func (w *response) Header() http.Header {
	return w.header
}

// maxPostHandlerReadBytes is the max number of http.Request.Body bytes not
// consumed by a handler that the server will read from the client
// in order to keep a connection alive.  If there are more bytes than
// this then the server to be paranoid instead sends a "Connection:
// close" response.
//
// This number is approximately what a typical machine's TCP buffer
// size is anyway.  (if we have the bytes on the machine, we might as
// well read them)
const maxPostHandlerReadBytes = 256 << 10

func (w *response) WriteHeader(code int) {
	if w.conn.hijacked {
		log.Print("http: response.WriteHeader on hijacked connection")
		return
	}
	if w.wroteHeader {
		log.Print("http: multiple response.WriteHeader calls")
		return
	}
	w.wroteHeader = true
	w.status = code

	// Check for a explicit (and valid) Content-Length header.
	var hasCL bool
	var contentLength int64
	if clenStr := w.header.Get("Content-Length"); clenStr != "" {
		var err error
		contentLength, err = strconv.ParseInt(clenStr, 10, 64)
		if err == nil {
			hasCL = true
		} else {
			log.Printf("http: invalid Content-Length of %q sent", clenStr)
			w.header.Del("Content-Length")
		}
	}

	if w.req.wantsHttp10KeepAlive() && (w.req.Method == "HEAD" || hasCL) {
		_, connectionHeaderSet := w.header["Connection"]
		if !connectionHeaderSet {
			w.header.Set("Connection", "keep-alive")
		}
	} else if !w.req.ProtoAtLeast(1, 1) {
		// Client did not ask to keep connection alive.
		w.closeAfterReply = true
	}

	if w.header.Get("Connection") == "close" {
		w.closeAfterReply = true
	}

	// Per RFC 2616, we should consume the request body before
	// replying, if the handler hasn't already done so.  But we
	// don't want to do an unbounded amount of reading here for
	// DoS reasons, so we only try up to a threshold.
	if w.req.ContentLength != 0 && !w.closeAfterReply {
		ecr, isExpecter := w.req.Body.(*expectContinueReader)
		if !isExpecter || ecr.resp.wroteContinue {
			n, _ := io.CopyN(ioutil.Discard, w.req.Body, maxPostHandlerReadBytes+1)
			if n >= maxPostHandlerReadBytes {
				w.requestTooLarge()
				w.header.Set("Connection", "close")
			} else {
				w.req.Body.Close()
			}
		}
	}

	if code == http.StatusNotModified {
		// Must not have body.
		for _, header := range []string{"Content-Type", "Content-Length", "Transfer-Encoding"} {
			if w.header.Get(header) != "" {
				// TODO: return an error if WriteHeader gets a return parameter
				// or set a flag on w to make future Writes() write an error page?
				// for now just log and drop the header.
				log.Printf("http: StatusNotModified response with header %q defined", header)
				w.header.Del(header)
			}
		}
	}

	if _, ok := w.header["Date"]; !ok {
		w.Header().Set("Date", time.Now().UTC().Format(TimeFormat))
	}

	te := w.header.Get("Transfer-Encoding")
	hasTE := te != ""
	if hasCL && hasTE && te != "identity" {
		// TODO: return an error if WriteHeader gets a return parameter
		// For now just ignore the Content-Length.
		log.Printf("http: WriteHeader called with both Transfer-Encoding of %q and a Content-Length of %d",
			te, contentLength)
		w.header.Del("Content-Length")
		hasCL = false
	}

	if w.req.Method == "HEAD" || code == http.StatusNotModified {
		// do nothing
	} else if hasCL {
		w.contentLength = contentLength
		w.header.Del("Transfer-Encoding")
	} else if w.req.ProtoAtLeast(1, 1) {
		// HTTP/1.1 or greater: use chunked transfer encoding
		// to avoid closing the connection at EOF.
		// TODO: this blows away any custom or stacked Transfer-Encoding they
		// might have set.  Deal with that as need arises once we have a valid
		// use case.
		w.chunking = true
		w.header.Set("Transfer-Encoding", "chunked")
	} else {
		// HTTP version < 1.1: cannot do chunked transfer
		// encoding and we don't know the Content-Length so
		// signal EOF by closing connection.
		w.closeAfterReply = true
		w.header.Del("Transfer-Encoding") // in case already set
	}

	// Cannot use Content-Length with non-identity Transfer-Encoding.
	if w.chunking {
		w.header.Del("Content-Length")
	}
	if !w.req.ProtoAtLeast(1, 0) {
		return
	}

	if w.closeAfterReply && !hasToken(w.header.Get("Connection"), "close") {
		w.header.Set("Connection", "close")
	}

	proto := "HTTP/1.0"
	if w.req.ProtoAtLeast(1, 1) {
		proto = "HTTP/1.1"
	}
	codestring := strconv.Itoa(code)
	text, ok := statusText[code]
	if !ok {
		text = "status code " + codestring
	}
	io.WriteString(w.conn.buf, proto+" "+codestring+" "+text+"\r\n")
	w.header.Write(w.conn.buf)
	io.WriteString(w.conn.buf, "\r\n")
}

// bodyAllowed returns true if a Write is allowed for this response type.
// It's illegal to call this before the header has been flushed.
func (w *response) bodyAllowed() bool {
	if !w.wroteHeader {
		panic("")
	}
	return w.status != http.StatusNotModified && w.req.Method != "HEAD"
}

func (w *response) Write(data []byte) (n int, err error) {
	if w.conn.hijacked {
		log.Print("http: response.Write on hijacked connection")
		return 0, ErrHijacked
	}
	if !w.wroteHeader {
		w.WriteHeader(http.StatusOK)
	}
	if len(data) == 0 {
		return 0, nil
	}
	if !w.bodyAllowed() {
		return 0, ErrBodyNotAllowed
	}

	w.written += int64(len(data)) // ignoring errors, for errorKludge
	if w.contentLength != -1 && w.written > w.contentLength {
		return 0, ErrContentLength
	}

	// TODO(rsc): if chunking happened after the buffering,
	// then there would be fewer chunk headers.
	// On the other hand, it would make hijacking more difficult.
	if w.chunking {
		fmt.Fprintf(w.conn.buf, "%x\r\n", len(data)) // TODO(rsc): use strconv not fmt
	}
	n, err = w.conn.buf.Write(data)
	if err == nil && w.chunking {
		if n != len(data) {
			err = io.ErrShortWrite
		}
		if err == nil {
			io.WriteString(w.conn.buf, "\r\n")
		}
	}

	return n, err
}

func (w *response) finishRequest() {
	// If the handler never wrote any bytes and never sent a Content-Length
	// response header, set the length explicitly to zero. This helps
	// HTTP/1.0 clients keep their "keep-alive" connections alive, and for
	// HTTP/1.1 clients is just as good as the alternative: sending a
	// chunked response and immediately sending the zero-length EOF chunk.
	if w.written == 0 && w.header.Get("Content-Length") == "" {
		w.header.Set("Content-Length", "0")
	}
	// If this was an HTTP/1.0 request with keep-alive and we sent a
	// Content-Length back, we can make this a keep-alive response ...
	if w.req.wantsHttp10KeepAlive() {
		sentLength := w.header.Get("Content-Length") != ""
		if sentLength && w.header.Get("Connection") == "keep-alive" {
			w.closeAfterReply = false
		}
	}
	if !w.wroteHeader {
		w.WriteHeader(http.StatusOK)
	}
	if w.chunking {
		io.WriteString(w.conn.buf, "0\r\n")
		// trailer key/value pairs, followed by blank line
		io.WriteString(w.conn.buf, "\r\n")
	}
	w.conn.buf.Flush()
	// Close the body, unless we're about to close the whole TCP connection
	// anyway.
	if !w.closeAfterReply {
		w.req.Body.Close()
	}
	if w.req.MultipartForm != nil {
		w.req.MultipartForm.RemoveAll()
	}

	if w.contentLength != -1 && w.contentLength != w.written {
		// Did not write enough. Avoid getting out of sync.
		w.closeAfterReply = true
	}
}

func (w *response) Flush() {
	if !w.wroteHeader {
		w.WriteHeader(http.StatusOK)
	}
	w.conn.buf.Flush()
}

// Close the connection.
func (c *conn) close() {
	if c.buf != nil {
		c.buf.Flush()
		c.buf = nil
	}
	if c.rwc != nil {
		c.rwc.Close()
		c.rwc = nil
	}
}

// Serve a new connection.
func (c *conn) serve() {
	defer func() {
		err := recover()
		if err == nil {
			return
		}

		var buf bytes.Buffer
		fmt.Fprintf(&buf, "http: panic serving %v: %v\n", c.remoteAddr, err)
		buf.Write(debug.Stack())
		log.Print(buf.String())

		if c.rwc != nil { // may be nil if connection hijacked
			c.rwc.Close()
		}
	}()

	for {
		w, err := c.readRequest()
		if err != nil {
			msg := "400 Bad Request"
			if err == errTooLarge {
				// Their HTTP client may or may not be
				// able to read this if we're
				// responding to them and hanging up
				// while they're still writing their
				// request.  Undefined behavior.
				msg = "413 Request Entity Too Large"
			} else if err == io.EOF {
				break // Don't reply
			} else if neterr, ok := err.(net.Error); ok && neterr.Timeout() {
				break // Don't reply
			}
			fmt.Fprintf(c.rwc, "HTTP/1.1 %s\r\n\r\n", msg)
			break
		}

		// Expect 100 Continue support
		req := w.req
		if req.expectsContinue() {
			if req.ProtoAtLeast(1, 1) {
				// Wrap the Body reader with one that replies on the connection
				req.Body = &expectContinueReader{readCloser: req.Body, resp: w}
			}
			if req.ContentLength == 0 {
				w.Header().Set("Connection", "close")
				w.WriteHeader(http.StatusBadRequest)
				w.finishRequest()
				break
			}
			req.Header.Del("Expect")
		} else if req.Header.Get("Expect") != "" {
			// TODO(bradfitz): let ServeHTTP handlers handle
			// requests with non-standard expectation[s]? Seems
			// theoretical at best, and doesn't fit into the
			// current ServeHTTP model anyway.  We'd need to
			// make the ResponseWriter an optional
			// "ExpectReplier" interface or something.
			//
			// For now we'll just obey RFC 2616 14.20 which says
			// "If a server receives a request containing an
			// Expect field that includes an expectation-
			// extension that it does not support, it MUST
			// respond with a 417 (Expectation Failed) status."
			w.Header().Set("Connection", "close")
			w.WriteHeader(http.StatusExpectationFailed)
			w.finishRequest()
			break
		}

		handler := c.server.Handler
		if handler == nil {
			handler = DefaultServeMux
		}

		// HTTP cannot have multiple simultaneous active requests.[*]
		// Until the server replies to this request, it can't read another,
		// so we might as well run the handler in this goroutine.
		// [*] Not strictly true: HTTP pipelining.  We could let them all process
		// in parallel even if their responses need to be serialized.
		handler.ServeHTTP(w, w.req)
		if c.hijacked {
			return
		}
		w.finishRequest()
		if w.closeAfterReply {
			break
		}
	}
	c.close()
}

// Hijack implements the Hijacker.Hijack method. Our response is both a ResponseWriter
// and a Hijacker.
func (w *response) Hijack() (rwc net.Conn, buf *bufio.ReadWriter, err error) {
	if w.conn.hijacked {
		return nil, nil, ErrHijacked
	}
	w.conn.hijacked = true
	rwc = w.conn.rwc
	buf = w.conn.buf
	w.conn.rwc = nil
	w.conn.buf = nil
	return
}

// Serve accepts incoming HTTP connections on the listener l,
// creating a new service thread for each.  The service threads
// read requests and then call handler to reply to them.
// Handler is typically nil, in which case the DefaultServeMux is used.
func Serve(l net.Listener, handler Handler) error {
	srv := &Server{Handler: handler}
	return srv.Serve(l)
}

// A Server defines parameters for running an HTTP server.
type Server struct {
	Addr           string        // TCP address to listen on, ":http" if empty
	Handler        Handler       // handler to invoke, http.DefaultServeMux if nil
	ReadTimeout    time.Duration // maximum duration before timing out read of the request
	WriteTimeout   time.Duration // maximum duration before timing out write of the response
	MaxHeaderBytes int           // maximum size of request headers, DefaultMaxHeaderBytes if 0
}

// ListenAndServe listens on the TCP network address srv.Addr and then
// calls Serve to handle requests on incoming connections.  If
// srv.Addr is blank, ":http" is used.
func (srv *Server) ListenAndServe() error {
	addr := srv.Addr
	if addr == "" {
		addr = ":http"
	}
	l, e := net.Listen("tcp", addr)
	if e != nil {
		return e
	}
	return srv.Serve(l)
}

// Serve accepts incoming connections on the Listener l, creating a
// new service thread for each.  The service threads read requests and
// then call srv.Handler to reply to them.
func (srv *Server) Serve(l net.Listener) error {
	defer l.Close()
	var tempDelay time.Duration // how long to sleep on accept failure
	for {
		rw, e := l.Accept()
		if e != nil {
			if ne, ok := e.(net.Error); ok && ne.Temporary() {
				if tempDelay == 0 {
					tempDelay = 5 * time.Millisecond
				} else {
					tempDelay *= 2
				}
				if max := 1 * time.Second; tempDelay > max {
					tempDelay = max
				}
				log.Printf("http: Accept error: %v; retrying in %v", e, tempDelay)
				time.Sleep(tempDelay)
				continue
			}
			return e
		}
		tempDelay = 0
		if srv.ReadTimeout != 0 {
			rw.SetReadDeadline(time.Now().Add(srv.ReadTimeout))
		}
		if srv.WriteTimeout != 0 {
			rw.SetWriteDeadline(time.Now().Add(srv.WriteTimeout))
		}
		c, err := srv.newConn(rw)
		if err != nil {
			continue
		}
		go c.serve()
	}
	panic("not reached")
}

// ListenAndServe listens on the TCP network address addr
// and then calls Serve with handler to handle requests
// on incoming connections.  Handler is typically nil,
// in which case the DefaultServeMux is used.
//
// A trivial example server is:
//
//	package main
//
//	import (
//		"io"
//		"net/http"
//		"log"
//	)
//
//	// hello world, the web server
//	func HelloServer(w http.ResponseWriter, req *http.Request) {
//		io.WriteString(w, "hello, world!\n")
//	}
//
//	func main() {
//		http.HandleFunc("/hello", HelloServer)
//		err := http.ListenAndServe(":12345", nil)
//		if err != nil {
//			log.Fatal("ListenAndServe: ", err)
//		}
//	}
func ListenAndServe(addr string, handler Handler) error {
	server := &Server{Addr: addr, Handler: handler}
	return server.ListenAndServe()
}
