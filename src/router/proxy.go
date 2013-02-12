package router

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"router/config"
	"strings"
	"sync"
	"time"
)

const (
	VcapBackendHeader = "X-Vcap-Backend"
	VcapRouterHeader  = "X-Vcap-Router"
	VcapTraceHeader   = "X-Vcap-Trace"

	VcapCookieId    = "__VCAP_ID__"
	StickyCookieKey = "JSESSIONID"
)

type Proxy struct {
	sync.RWMutex

	*config.Config
	*Registry
	Varz
}

func NewProxy(c *config.Config, r *Registry, v Varz) *Proxy {
	return &Proxy{
		Config:   c,
		Registry: r,
		Varz:     v,
	}
}

func hostWithoutPort(req *http.Request) string {
	host := req.Host

	// Remove :<port>
	pos := strings.Index(host, ":")
	if pos >= 0 {
		host = host[0:pos]
	}

	return host
}

func (p *Proxy) Lookup(req *http.Request) (*Backend, bool) {
	h := hostWithoutPort(req)

	// Try choosing a backend using sticky session
	sticky, err := req.Cookie(VcapCookieId)
	if err == nil {
		b, ok := p.Registry.LookupByPrivateInstanceId(h, sticky.Value)
		if ok {
			return b, ok
		}
	}

	// Choose backend using host alone
	return p.Registry.Lookup(h)
}

func (p *Proxy) ServeHTTP(rw http.ResponseWriter, req *http.Request) {
	if req.ProtoMajor != 1 && (req.ProtoMinor != 0 || req.ProtoMinor != 1) {
		hj := rw.(http.Hijacker)

		c, brw, err := hj.Hijack()
		if err != nil {
			panic(err)
		}

		fmt.Fprintf(brw, "HTTP/1.0 400 Bad Request\r\n\r\n")
		brw.Flush()
		c.Close()
		return
	}

	start := time.Now()

	// Return 200 OK for heartbeats from LB
	if req.UserAgent() == "HTTP-Monitor/1.1" {
		rw.WriteHeader(http.StatusOK)
		fmt.Fprintln(rw, "ok")
		return
	}

	x, ok := p.Lookup(req)
	if !ok {
		p.Varz.CaptureBadRequest(req)
		p.WriteNotFound(rw)
		return
	}

	p.Registry.CaptureBackendRequest(x, start)
	p.Varz.CaptureBackendRequest(x, req)

	req.URL.Scheme = "http"
	req.URL.Host = x.CanonicalAddr()

	// Add X-Forwarded-For
	if host, _, err := net.SplitHostPort(req.RemoteAddr); err == nil {
		// We assume there is a trusted upstream (L7 LB) that properly
		// strips client's XFF header

		// This is sloppy but fine since we don't share this request or
		// headers. Otherwise we should copy the underlying header and
		// append
		xff := append(req.Header["X-Forwarded-For"], host)
		req.Header.Set("X-Forwarded-For", strings.Join(xff, ", "))
	}

	// Check if the connection is going to be upgraded to a WebSocket connection
	if p.CheckWebSocket(rw, req) {
		p.ServeWebSocket(rw, req)
		return
	}

	// Use a new connection for every request
	// Keep-alive can be bolted on later, if we want to
	req.Close = true
	req.Header.Del("Connection")

	res, err := http.DefaultTransport.RoundTrip(req)

	latency := time.Since(start)

	if err != nil {
		p.Varz.CaptureBackendResponse(x, res, latency)
		p.WriteBadGateway(err, rw)
		return
	}

	p.Varz.CaptureBackendResponse(x, res, latency)

	for k, vv := range res.Header {
		for _, v := range vv {
			rw.Header().Add(k, v)
		}
	}

	if req.Header.Get(VcapTraceHeader) != "" {
		rw.Header().Set(VcapRouterHeader, p.Config.Ip)
		rw.Header().Set(VcapBackendHeader, x.CanonicalAddr())
	}

	needSticky := false
	for _, v := range res.Cookies() {
		if v.Name == StickyCookieKey {
			needSticky = true
			break
		}
	}

	if needSticky && x.PrivateInstanceId != "" {
		cookie := &http.Cookie{
			Name:  VcapCookieId,
			Value: x.PrivateInstanceId,
			Path:  "/",
		}
		http.SetCookie(rw, cookie)
	}

	rw.WriteHeader(res.StatusCode)

	if res.Body != nil {
		var dst io.Writer = rw
		io.Copy(dst, res.Body)
	}
}

func (p *Proxy) CheckWebSocket(rw http.ResponseWriter, req *http.Request) bool {
	return req.Header.Get("Connection") == "Upgrade" && req.Header.Get("Upgrade") == "websocket"
}

func (p *Proxy) ServeWebSocket(rw http.ResponseWriter, req *http.Request) {
	var err error

	hj := rw.(http.Hijacker)

	dc, _, err := hj.Hijack()
	if err != nil {
		p.WriteBadGateway(err, rw)
		return
	}

	defer dc.Close()

	// Dial backend
	uc, err := net.Dial("tcp", req.URL.Host)
	if err != nil {
		p.WriteBadGateway(err, rw)
		return
	}

	defer uc.Close()

	// Write request
	err = req.Write(uc)
	if err != nil {
		p.WriteBadGateway(err, rw)
		return
	}

	errch := make(chan error, 2)

	copy := func(dst io.Writer, src io.Reader) {
		_, err := io.Copy(dst, src)
		if err != nil {
			errch <- err
		}
	}

	go copy(uc, dc)
	go copy(dc, uc)

	// Don't care about error, both connections will be closed if necessary
	<-errch
}

func (p *Proxy) WriteStatus(rw http.ResponseWriter, code int) {
	body := fmt.Sprintf("%d %s", code, http.StatusText(code))
	http.Error(rw, body, code)
}

func (p *Proxy) WriteBadGateway(err error, rw http.ResponseWriter) {
	log.Warnf("Error: %s", err)
	p.WriteStatus(rw, http.StatusBadGateway)
}

func (p *Proxy) WriteNotFound(rw http.ResponseWriter) {
	p.WriteStatus(rw, http.StatusNotFound)
}
