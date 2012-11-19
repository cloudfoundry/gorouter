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

	r          map[string][]*registerMessage
	d          map[string]int
	varz       *Varz
	se         *SessionEncoder
	activeApps *AppList
}

func NewProxy(se *SessionEncoder, activeApps *AppList, varz *Varz, r *Registry) *Proxy {
	p := new(Proxy)

	p.Registry = r
	p.r = make(map[string][]*registerMessage)
	p.d = make(map[string]int)

	p.se = se
	p.varz = varz
	p.activeApps = activeApps

	return p
}

func (p *Proxy) Lookup(req *http.Request) (Endpoint, bool) {
	var e Endpoint
	var ok bool

	// Loop in case of a race between Lookup and LookupByInstanceId
	for {
		is := p.Registry.Lookup(req)

		if len(is) == 0 {
			return e, false
		}

		// If there's only one endpoint, choose that
		if len(is) == 1 {
			e, ok = p.Registry.LookupByInstanceId(is[0])
			if ok {
				return e, true
			} else {
				continue
			}
		}

		// Choose backend depending on sticky session
		sticky, err := req.Cookie(VcapCookieId)
		if err == nil {
			sh, sp := p.se.decryptStickyCookie(sticky.Value)
			if sh != "" && sp != 0 {
				es, ok := p.Registry.LookupByInstanceIds(is)
				if ok {
					// Return endpoint if host and port match
					for _, e := range es {
						if sh == e.Host && sp == e.Port {
							return e, true
						}
					}

					// No matching endpoint found
				}
			}
		}

		e, ok = p.Registry.LookupByInstanceId(is[rand.Intn(len(is))])
		if ok {
			return e, true
		} else {
			continue
		}
	}

	panic("not reached")

	return e, ok
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

	// Save the app_id of active app
	p.activeApps.Insert(e.ApplicationId)

	p.varz.IncRequestsWithTags(e.Tags)
	p.varz.IncAppRequests(getUrl(req))

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

	if needSticky {
		cookie := &http.Cookie{
			Name:  VcapCookieId,
			Value: p.se.getStickyCookie(e),
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
