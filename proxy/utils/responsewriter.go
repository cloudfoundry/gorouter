package utils

import (
	"bufio"
	"errors"
	"net"
	"net/http"
	"reflect"
)

type Context interface {
	Value(key interface{}) interface{}
}

func WithValue(parent Context, key, val interface{}) Context {
	if key == nil {
		panic("nil key")
	}
	if !reflect.TypeOf(key).Comparable() {
		panic("key is not comparable")
	}
	return &valueCtx{parent, key, val}
}

type rootCtx struct{}

func (c *rootCtx) Value(key interface{}) interface{} {
	return nil
}

type valueCtx struct {
	Context
	key, val interface{}
}

func (c *valueCtx) Value(key interface{}) interface{} {
	if c.key == key {
		return c.val
	}
	return c.Context.Value(key)
}

type ProxyResponseWriter interface {
	Header() http.Header
	Hijack() (net.Conn, *bufio.ReadWriter, error)
	Write(b []byte) (int, error)
	WriteHeader(s int)
	Done()
	Flush()
	Status() int
	Size() int
	Context() Context
	AddToContext(key, value interface{})
}

type proxyResponseWriter struct {
	w      http.ResponseWriter
	status int
	size   int

	flusher http.Flusher
	done    bool
	context Context
}

func NewProxyResponseWriter(w http.ResponseWriter) *proxyResponseWriter {
	proxyWriter := &proxyResponseWriter{
		w:       w,
		flusher: w.(http.Flusher),
		context: &rootCtx{},
	}

	return proxyWriter
}

func (p *proxyResponseWriter) Header() http.Header {
	return p.w.Header()
}

func (p *proxyResponseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	hijacker, ok := p.w.(http.Hijacker)
	if !ok {
		return nil, nil, errors.New("response writer cannot hijack")
	}
	return hijacker.Hijack()
}

func (p *proxyResponseWriter) Write(b []byte) (int, error) {
	if p.done {
		return 0, nil
	}

	if p.status == 0 {
		p.WriteHeader(http.StatusOK)
	}
	size, err := p.w.Write(b)
	p.size += size
	return size, err
}

func (p *proxyResponseWriter) WriteHeader(s int) {
	if p.done {
		return
	}

	p.w.WriteHeader(s)

	if p.status == 0 {
		p.status = s
	}
}

func (p *proxyResponseWriter) Done() {
	p.done = true
}

func (p *proxyResponseWriter) Flush() {
	if p.flusher != nil {
		p.flusher.Flush()
	}
}

func (p *proxyResponseWriter) Status() int {
	return p.status
}

func (p *proxyResponseWriter) Size() int {
	return p.size
}

func (p *proxyResponseWriter) Context() Context {
	return p.context
}

func (p *proxyResponseWriter) AddToContext(key, value interface{}) {
	p.context = WithValue(p.context, key, value)
}
