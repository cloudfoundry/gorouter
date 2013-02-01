package router

import (
	"fmt"
	"io"
	"math/rand"
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

func NewProxy(r *Registry, v Varz) *Proxy {
	p := &Proxy{
		Registry: r,
		Varz:     v,
	}

	return p
}

func (p *Proxy) Lookup(req *http.Request) (Backend, bool) {
	var b Backend
	var ok bool

	// Loop in case of a race between Lookup and LookupByBackendId
	for {
		x := p.Registry.Lookup(req)

		if len(x) == 0 {
			return b, false
		}

		// If there's only one backend, choose that
		if len(x) == 1 {
			b, ok = p.Registry.LookupByBackendId(x[0])
			if ok {
				return b, true
			} else {
				continue
			}
		}

		// Choose backend depending on sticky session
		sticky, err := req.Cookie(VcapCookieId)
		if err == nil {
			y, ok := p.Registry.LookupByBackendIds(x)
			if ok {
				// Return backend if host and port match
				for _, b := range y {
					if sticky.Value == b.PrivateInstanceId {
						return b, true
					}
				}

				// No matching backend found
			}
		}

		b, ok = p.Registry.LookupByBackendId(x[rand.Intn(len(x))])
		if ok {
			return b, true
		} else {
			continue
		}
	}

	log.Fatal("not reached")

	return b, ok
}

func (p *Proxy) ServeHTTP(rw http.ResponseWriter, req *http.Request) {
	start := time.Now()

	// Return 200 OK for heartbeats from LB
	if req.UserAgent() == "HTTP-Monitor/1.1" {
		rw.WriteHeader(http.StatusOK)
		fmt.Fprintln(rw, "ok")
		return
	}

	x, ok := p.Lookup(req)
	if !ok {
		rw.WriteHeader(http.StatusNotFound)
		fmt.Fprintf(rw, "%d %s", http.StatusNotFound, http.StatusText(http.StatusNotFound))
		p.Varz.CaptureBadRequest(req)
		return
	}

	p.Registry.CaptureBackendRequest(x, start)
	p.Varz.CaptureBackendRequest(x, req)

	req.URL.Scheme = "http"
	req.URL.Host = x.CanonicalAddr()
	req.Proto = "HTTP/1.1"
	req.ProtoMajor = 1
	req.ProtoMinor = 1

	// Use a new connection for every request
	// Keep-alive can be bolted on later, if we want to
	req.Close = true
	req.Header.Del("Connection")

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

	res, err := http.DefaultTransport.RoundTrip(req)

	latency := time.Since(start)

	if err != nil {
		log.Warnf("Error from upstream: %s", err)
		rw.WriteHeader(http.StatusBadGateway)
		fmt.Fprintf(rw, "%d %s", http.StatusBadGateway, http.StatusText(http.StatusBadGateway))
		p.Varz.CaptureBackendResponse(x, res, latency)
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
