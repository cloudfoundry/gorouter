package main

import (
	"code.google.com/p/go.net/websocket"
	"encoding/json"
	"fmt"
	nats "github.com/cloudfoundry/gonats"
	"io"
	"log"
	"math/rand"
	"net"
	"net/http"
	"runtime"
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
	r map[string][]*registerMessage
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

func copyHeader(dst, src http.Header) {
	for k, vv := range src {
		for _, v := range vv {
			dst.Add(k, v)
		}
	}
}

func (p *Proxy) ServeHTTP(rw http.ResponseWriter, req *http.Request) {
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
		rw.WriteHeader(http.StatusInternalServerError)
		return
	}

	copyHeader(rw.Header(), res.Header)

	rw.WriteHeader(res.StatusCode)

	if res.Body != nil {
		var dst io.Writer = rw
		io.Copy(dst, res.Body)
	}
}

func StartNATS() *nats.Client {
	c := nats.NewClient()

	go func() {
		e := c.RunWithDefaults("127.0.0.1:4222", "", "")
		panic(e)
	}()

	return c
}

func MemStatsServer(ws *websocket.Conn) {
	var e error

	var t *time.Ticker = time.NewTicker(100 * time.Millisecond)
	defer t.Stop()

	enc := json.NewEncoder(ws)

	for {
		select {
		case <-t.C:
			var ms runtime.MemStats

			runtime.ReadMemStats(&ms)

			e = enc.Encode(ms)
			if e != nil {
				fmt.Printf("WebSocket error: %s\n", e)
				return
			}
		}
	}
}

func StartHTTP() {
	http.Handle("/ws", websocket.Handler(MemStatsServer))
	http.Handle("/", http.FileServer(http.Dir(".")))

	http.HandleFunc("/data.json", func(w http.ResponseWriter, r *http.Request) {
		var ms runtime.MemStats

		runtime.ReadMemStats(&ms)

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)

		enc := json.NewEncoder(w)
		enc.Encode(ms)
	})

	fmt.Printf("Starting...\n")
	log.Fatal(http.ListenAndServe(":8081", nil))
}

func main() {
	var p = NewProxy()

	n := StartNATS()

	reg := n.NewSubscription("router.register")
	reg.Subscribe()

	// Start message
	n.Publish("router.start", []byte(""))

	go func() {
		for m := range reg.Inbox {
			var rm registerMessage

			e := json.Unmarshal(m.Payload, &rm)
			if e != nil {
				continue
			}

			fmt.Printf("router.register: %#v\n", rm)
			p.Register(&rm)
		}
	}()

	go StartHTTP()

	err := http.ListenAndServe(":8080", p)
	if err != nil {
		log.Fatal("ListenAndServe: ", err)
	}
}
