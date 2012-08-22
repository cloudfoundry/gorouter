package router

import (
	"fmt"
	"io"
	"log"
	"math/rand"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

type registerMessage struct {
	Host string            `json:"host"`
	Port uint16            `json:"port"`
	Uris []string          `json:"uris"`
	Tags map[string]string `json:"tags"`
	Dea  string            `json:"dea"`
}

type Proxy struct {
	sync.Mutex
	r      map[string][]*registerMessage
	status *ServerStatus
}

func NewProxy() *Proxy {
	p := new(Proxy)
	p.r = make(map[string][]*registerMessage)
	return p
}

func (p *Proxy) Register(m *registerMessage) {
	p.Lock()
	defer p.Unlock()

	// Store in registry
	s := p.r[m.Uris[0]]
	if s == nil {
		s = make([]*registerMessage, 0)
	}

	s = append(s, m)
	p.r[m.Uris[0]] = s

	p.status.Urls = len(p.r)
}

func (p *Proxy) Lookup(req *http.Request) *registerMessage {
	host := req.Host

	// Remove :<port>
	i := strings.Index(host, ":")
	if i >= 0 {
		host = host[0:i]
	}

	p.Lock()
	defer p.Unlock()

	s := p.r[host]
	if s == nil {
		return nil
	}

	return s[rand.Intn(len(s))]
}

func (p *Proxy) ServeHTTP(rw http.ResponseWriter, req *http.Request) {
	start := time.Now()

	p.status.IncRequests()

	r := p.Lookup(req)
	if r == nil {
		rw.WriteHeader(http.StatusNotFound)
		return
	}

	outreq := new(http.Request)
	*outreq = *req // includes shallow copies of maps, but okay

	outreq.URL.Scheme = "http"
	outreq.URL.Host = fmt.Sprintf("%s:%d", r.Host, r.Port)

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
		log.Printf("http: proxy error: %v", err)
		p.recordStatus(500, start, r.Tags)
		rw.WriteHeader(http.StatusInternalServerError)
		return
	}

	p.recordStatus(res.StatusCode, start, r.Tags)

	copyHeader(rw.Header(), res.Header)

	rw.WriteHeader(res.StatusCode)

	if res.Body != nil {
		var dst io.Writer = rw
		io.Copy(dst, res.Body)
	}
}

func (p *Proxy) recordStatus(status int, start time.Time, tags map[string]string) {
	latency := int(time.Since(start).Nanoseconds() / 1000000)

	p.status.RecordResponse(status, latency, tags)
}

func copyHeader(dst, src http.Header) {
	for k, vv := range src {
		for _, v := range vv {
			dst.Add(k, v)
		}
	}
}
