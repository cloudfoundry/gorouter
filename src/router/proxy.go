package router

import (
	"fmt"
	"io"
	"math/rand"
	"net"
	"net/http"
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
	*Registry

	r    map[string][]*registerMessage
	d    map[string]int
	varz *Varz
}

func NewProxy(varz *Varz, r *Registry) *Proxy {
	p := new(Proxy)

	p.Registry = r
	p.r = make(map[string][]*registerMessage)
	p.d = make(map[string]int)

	p.varz = varz

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

	panic("not reached")

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

	p.varz.IncRequests()

	e, ok := p.Lookup(req)
	if !ok {
		p.recordStatus(400, start, nil)
		p.varz.IncBadRequests()

		rw.WriteHeader(http.StatusNotFound)
		return
	}

	p.Registry.CaptureBackendRequest(e, start)

	p.varz.IncRequestsWithTags(e.Tags)

	outreq := new(http.Request)
	*outreq = *req // includes shallow copies of maps, but okay

	outHost := fmt.Sprintf("%s:%d", e.Host, e.Port)
	outreq.URL.Scheme = "http"
	outreq.URL.Host = outHost

	outreq.Proto = "HTTP/1.1"
	outreq.ProtoMajor = 1
	outreq.ProtoMinor = 1
	outreq.Close = false

	// Remove the connection header to the backend.  We want a
	// persistent connection, regardless of what the client sent
	// to us.  This is modifying the same underlying map from req
	// (shallow copied above) so we only copy it if necessary.
	if outreq.Header.Get("Connection") != "" {
		outreq.Header = make(http.Header)
		copyHeader(outreq.Header, req.Header)
		outreq.Header.Del("Connection")
	}

	if clientIp, _, err := net.SplitHostPort(req.RemoteAddr); err == nil {
		outreq.Header.Set("X-Forwarded-For", clientIp)
	}

	res, err := http.DefaultTransport.RoundTrip(outreq)
	if err != nil {
		log.Errorf("http: proxy error: %v", err)

		p.recordStatus(500, start, e.Tags)
		p.varz.IncBadRequests()

		rw.WriteHeader(http.StatusInternalServerError)
		return
	}

	p.recordStatus(res.StatusCode, start, e.Tags)

	copyHeader(rw.Header(), res.Header)

	if req.Header.Get(VcapTraceHeader) != "" {
		rw.Header().Set(VcapRouterHeader, config.ip)
		rw.Header().Set(VcapBackendHeader, outHost)
	}

	needSticky := false
	for _, v := range res.Cookies() {
		if v.Name == StickyCookieKey {
			needSticky = true
			break
		}
	}

	if needSticky && e.PrivateInstanceId != "" {
		cookie := &http.Cookie{
			Name:  VcapCookieId,
			Value: e.PrivateInstanceId,
		}
		http.SetCookie(rw, cookie)
	}

	rw.WriteHeader(res.StatusCode)

	if res.Body != nil {
		var dst io.Writer = rw
		io.Copy(dst, res.Body)
	}
}

func (p *Proxy) recordStatus(status int, start time.Time, tags map[string]string) {
	latency := int(time.Since(start).Nanoseconds() / 1000000)
	p.varz.RecordResponse(status, latency, tags)
}

func copyHeader(dst, src http.Header) {
	for k, vv := range src {
		for _, v := range vv {
			dst.Add(k, v)
		}
	}
}

func getUrl(req *http.Request) string {
	host := req.Host

	// Remove :<port>
	i := strings.Index(host, ":")
	if i >= 0 {
		host = host[0:i]
	}

	return strings.ToLower(host)
}
