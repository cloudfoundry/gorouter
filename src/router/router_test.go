package router

import (
	"bytes"
	"encoding/json"
	"fmt"
	nats "github.com/cloudfoundry/gonats"
	"io"
	"io/ioutil"
	. "launchpad.net/gocheck"
	"net/http"
	"regexp"
	"router/common"
	"router/common/spec"
	"strings"
	"time"
)

type RouterSuite struct {
	natsServer *spec.NatsServer
	natsClient *nats.Client
	router     *Router
}

var _ = Suite(&RouterSuite{})

func (s *RouterSuite) SetUpSuite(c *C) {
	s.natsServer = spec.NewNatsServer(8089, "/tmp/router_nats_test.pid")
	err := s.natsServer.Start()
	c.Assert(err, IsNil)

	InitConfig(&Config{
		Port:   8083,
		Index:  2,
		Nats:   NatsConfig{URI: "nats://localhost:8089"},
		Status: StatusConfig{8084, "user", "pass"},
		Log:    LogConfig{"info", "/dev/null", ""},
	})

	s.router = NewRouter()
	go s.router.Run()

	s.natsClient = startNATS("localhost:8089", "", "")
}

func (s *RouterSuite) TearDownSuite(c *C) {
	s.natsServer.Stop()
}

func (s *RouterSuite) TestDiscover(c *C) {
	// Test if router responses to discover message
	sig := make(chan common.VcapComponent)

	// Since the form of uptime is xxd:xxh:xxm:xxs, we should make
	// sure that router has run at least for one second
	time.Sleep(time.Second)

	s.natsClient.Request("vcap.component.discover", []byte{}, func(sub *nats.Subscription) {
		var component common.VcapComponent

		for m := range sub.Inbox {
			_ = json.Unmarshal(m.Payload, &component)

			break
		}
		sig <- component
	})

	cc := <-sig

	var emptyTime time.Time
	var emptyDuration common.Duration

	c.Check(cc.Type, Equals, "Router")
	c.Check(cc.Index, Equals, uint(2))
	c.Check(cc.UUID, Not(Equals), "")
	c.Check(cc.Start, Not(Equals), emptyTime)
	c.Check(cc.Uptime, Not(Equals), emptyDuration)

	// Check varz/healthz is accessible
	var b []byte
	var err error

	// Verify varz
	vbody := verifyZ(cc.Host, "/varz", cc.Credentials[0], cc.Credentials[1], c)
	defer vbody.Close()
	b, err = ioutil.ReadAll(vbody)
	c.Check(err, IsNil)
	varz := make(map[string]interface{})
	json.Unmarshal(b, &varz)

	c.Assert(varz["num_cores"], Not(Equals), 0)
	c.Assert(varz["type"], Equals, "Router")
	c.Assert(varz["uuid"], Not(Equals), "")

	// Verify healthz
	hbody := verifyZ(cc.Host, "/healthz", cc.Credentials[0], cc.Credentials[1], c)
	defer hbody.Close()
	b, err = ioutil.ReadAll(hbody)
	match, _ := regexp.Match("ok", b)

	c.Check(err, IsNil)
	c.Check(match, Equals, true)
}

func (s *RouterSuite) TestXFF(c *C) {
	var request http.Request
	// dummy backend that records the request
	app := NewTestApp([]Uri{"xff.vcap.me"}, uint16(8083), s.natsClient, nil)
	app.AddHandler("/", func(w http.ResponseWriter, r *http.Request) {
		request = *r
	})
	app.Listen()
	c.Assert(s.waitAppRegistered(app, time.Second*5), Equals, true)

	r, err := http.NewRequest("GET", fmt.Sprintf("http://%s:%d", "xff.vcap.me", 8083), nil)
	c.Assert(err, IsNil)
	r.Header.Set("X-Forwarded-For", "1.2.3.4")
	resp, err := http.DefaultClient.Do(r)
	c.Assert(err, IsNil)
	c.Check(resp.StatusCode, Equals, http.StatusOK)
	c.Check(strings.HasPrefix(request.Header.Get("X-Forwarded-For"), "1.2.3.4, "), Equals, true)
	app.Unregister()
	c.Assert(s.waitAppUnregistered(app, time.Second*5), Equals, true)
}

func (s *RouterSuite) waitMsgReceived(a *TestApp, r bool, t time.Duration) bool {
	i := time.Millisecond * 50
	m := int(t / i)

	for j := 0; j < m; j++ {
		received := true
		for _, v := range a.urls {
			ms := s.router.registry.Lookup(&http.Request{Host: string(v)})
			status := (ms != nil)
			if status != r {
				received = false
				break
			}
		}
		if received {
			return true
		}
		time.Sleep(i)
	}

	return false
}

func (s *RouterSuite) waitAppRegistered(app *TestApp, timeout time.Duration) bool {
	return s.waitMsgReceived(app, true, timeout)
}

func (s *RouterSuite) waitAppUnregistered(app *TestApp, timeout time.Duration) bool {
	return s.waitMsgReceived(app, false, timeout)
}

func (s *RouterSuite) TestRegisterUnregister(c *C) {
	app := NewTestApp([]Uri{"test.vcap.me"}, uint16(8083), s.natsClient, nil)
	app.AddHandler("/", greetHandler)
	app.Listen()
	c.Assert(s.waitAppRegistered(app, time.Second*5), Equals, true)

	app.VerifyAppStatus(200, c)

	app.Unregister()
	c.Assert(s.waitAppUnregistered(app, time.Second*5), Equals, true)
	app.VerifyAppStatus(404, c)
}

func (s *RouterSuite) TestTraceHeader(c *C) {
	app := NewTestApp([]Uri{"test.vcap.me"}, uint16(8083), s.natsClient, nil)
	app.AddHandler("/", greetHandler)
	app.Listen()
	c.Assert(s.waitAppRegistered(app, time.Second*5), Equals, true)

	app.VerifyAppStatus(200, c)
	app.VerifyTraceHeader(c)

	app.Unregister()
	c.Assert(s.waitAppUnregistered(app, time.Second*5), Equals, true)
}

func (s *RouterSuite) readVarz() map[string]interface{} {
	x, err := s.router.varz.MarshalJSON()
	if err != nil {
		panic(err)
	}

	y := make(map[string]interface{})
	err = json.Unmarshal(x, &y)
	if err != nil {
		panic(err)
	}

	return y
}

func f(x interface{}, s ...string) interface{} {
	var ok bool

	for _, y := range s {
		z := x.(map[string]interface{})
		x, ok = z[y]
		if !ok {
			panic(fmt.Sprintf("no key: %s", s))
		}
	}

	return x
}

func (s *RouterSuite) TestVarz(c *C) {
	app := NewTestApp([]Uri{"count.vcap.me"}, uint16(8083), s.natsClient, map[string]string{"framework": "rails"})
	app.AddHandler("/", greetHandler)
	app.Listen()

	c.Assert(s.waitAppRegistered(app, time.Millisecond*500), Equals, true)
	// Send seed request
	sendRequests("count.vcap.me", uint16(8083), 1)
	vA := s.readVarz()

	// Send requests
	sendRequests("count.vcap.me", uint16(8083), 100)
	vB := s.readVarz()

	// Verify varz update
	RequestsA := int(f(vA, "requests").(float64))
	RequestsB := int(f(vB, "requests").(float64))
	allRequests := RequestsB - RequestsA
	c.Check(allRequests, Equals, 100)

	Responses2xxA := int(f(vA, "responses_2xx").(float64))
	Responses2xxB := int(f(vB, "responses_2xx").(float64))
	allResponses2xx := Responses2xxB - Responses2xxA
	c.Check(allResponses2xx, Equals, 100)

	RailsRequestsA := int(f(vA, "tags", "framework", "rails", "requests").(float64))
	RailsRequestsB := int(f(vB, "tags", "framework", "rails", "requests").(float64))
	allRailsRequests := RailsRequestsB - RailsRequestsA
	c.Check(allRailsRequests, Equals, 100)

	RailsResponses2xxA := int(f(vA, "tags", "framework", "rails", "requests").(float64))
	RailsResponses2xxB := int(f(vB, "tags", "framework", "rails", "requests").(float64))
	allRailsResponses2xx := RailsResponses2xxB - RailsResponses2xxA
	c.Check(allRailsResponses2xx, Equals, 100)

	app.Unregister()
}

func (s *RouterSuite) TestStickySession(c *C) {
	apps := make([]*TestApp, 10)
	for i := range apps {
		apps[i] = NewTestApp([]Uri{"sticky.vcap.me"}, uint16(8083), s.natsClient, nil)
		apps[i].AddHandler("/sticky", stickyHandler(apps[i].port))
		apps[i].Listen()
	}

	for _, app := range apps {
		c.Assert(s.waitAppRegistered(app, time.Millisecond*500), Equals, true)
	}
	session, port1, path := getSessionAndAppPort("sticky.vcap.me", uint16(8083), c)
	port2 := getAppPortWithSticky("sticky.vcap.me", uint16(8083), session, c)

	c.Check(port1, Equals, port2)
	c.Check(path, Equals, "/")

	for _, app := range apps {
		app.Unregister()
	}
}

func verifyZ(host, path, user, pass string, c *C) io.ReadCloser {
	var client http.Client
	var req *http.Request
	var resp *http.Response
	var err error

	// Request without username:password should be rejected
	req, err = http.NewRequest("GET", "http://"+host+path, nil)
	resp, err = client.Do(req)
	c.Check(err, IsNil)
	c.Check(resp.StatusCode, Equals, 401)

	// varz Basic auth
	req.SetBasicAuth(user, pass)
	resp, err = client.Do(req)
	c.Check(err, IsNil)
	c.Check(resp.StatusCode, Equals, 200)

	return resp.Body
}

func (s *RouterSuite) TestRouterRunErrors(c *C) {
	c.Assert(func() { s.router.Run() }, PanicMatches, "net.Listen.*")
}

func (s *RouterSuite) TestProxyPutRequest(c *C) {
	app := NewTestApp([]Uri{"greet.vcap.me"}, uint16(8083), s.natsClient, nil)

	var rr *http.Request
	var msg string
	app.AddHandler("/", func(w http.ResponseWriter, r *http.Request) {
		rr = r
		b, err := ioutil.ReadAll(r.Body)
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		msg = string(b)
	})
	app.Listen()
	c.Assert(s.waitAppRegistered(app, time.Second*5), Equals, true)

	url := fmt.Sprintf("http://%s:%d/", app.urls[0], app.rPort)

	buf := bytes.NewBufferString("foobar")
	r, err := http.NewRequest("PUT", url, buf)
	c.Assert(err, IsNil)

	resp, err := http.DefaultClient.Do(r)
	c.Assert(err, IsNil)
	c.Assert(resp.StatusCode, Equals, http.StatusOK)

	c.Assert(rr, NotNil)
	c.Assert(rr.Method, Equals, "PUT")
	c.Assert(rr.Proto, Equals, "HTTP/1.1")
	c.Assert(msg, Equals, "foobar")
}

type TestApp struct {
	port       uint16 // app listening port
	rPort      uint16 // router listening port
	urls       []Uri  // host registered host name
	natsClient *nats.Client
	tags       map[string]string
	mux        *http.ServeMux
}

func NewTestApp(urls []Uri, rPort uint16, natsClient *nats.Client, tags map[string]string) *TestApp {
	app := new(TestApp)

	port, _ := common.GrabEphemeralPort()

	app.port = port
	app.rPort = rPort
	app.urls = urls
	app.natsClient = natsClient
	app.tags = tags

	app.mux = http.NewServeMux()

	return app
}

func (a *TestApp) AddHandler(path string, handler func(http.ResponseWriter, *http.Request)) {
	a.mux.HandleFunc(path, handler)
}

func (a *TestApp) Listen() {

	server := &http.Server{
		Addr:    fmt.Sprintf(":%d", a.port),
		Handler: a.mux,
	}

	a.Register()

	go server.ListenAndServe()
}

func greetHandler(w http.ResponseWriter, r *http.Request) {
	io.WriteString(w, "Hello, world")
}

func stickyHandler(port uint16) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		cookie := &http.Cookie{
			Name:  "JSESSIONID",
			Value: "xxx",
		}
		http.SetCookie(w, cookie)
		io.WriteString(w, fmt.Sprintf("%d", port))
	}
}

func (a *TestApp) Register() {
	rm := registerMessage{
		Host: "localhost",
		Port: a.port,
		Uris: a.urls,
		Tags: a.tags,
		Dea:  "dea",
		App:  "0",

		PrivateInstanceId: common.GenerateUUID(),
	}

	b, _ := json.Marshal(rm)
	a.natsClient.Publish("router.register", b)
}

func (a *TestApp) Unregister() {
	rm := registerMessage{
		Host: "localhost",
		Port: a.port,
		Uris: a.urls,
		Tags: nil,
		Dea:  "dea",
		App:  "0",
	}

	b, _ := json.Marshal(rm)
	a.natsClient.Publish("router.unregister", b)
}

func (a *TestApp) VerifyAppStatus(status int, c *C) {
	for _, url := range a.urls {
		uri := fmt.Sprintf("http://%s:%d", url, a.rPort)
		resp, err := http.Get(uri)
		c.Assert(err, IsNil)
		c.Check(resp.StatusCode, Equals, status)
	}
}

func (a *TestApp) VerifyTraceHeader(c *C) {
	var client http.Client
	var req *http.Request
	var resp *http.Response
	var err error

	routerIP, _ := common.LocalIP()

	for _, url := range a.urls {
		uri := fmt.Sprintf("http://%s:%d", url, a.rPort)

		req, err = http.NewRequest("GET", uri, nil)
		req.Header.Add(VcapTraceHeader, "anything")
		resp, err = client.Do(req)

		c.Assert(err, IsNil)
		c.Check(resp.StatusCode, Equals, 200)
		c.Check(resp.Header.Get(VcapBackendHeader), Equals, fmt.Sprintf("localhost:%d", a.port))
		c.Check(resp.Header.Get(VcapRouterHeader), Equals, routerIP)
	}
}

func sendRequests(url string, rPort uint16, times int) {
	uri := fmt.Sprintf("http://%s:%d", url, rPort)

	for i := 0; i < times; i++ {
		http.Get(uri)
	}
}

func getSessionAndAppPort(url string, rPort uint16, c *C) (string, string, string) {
	var client http.Client
	var req *http.Request
	var resp *http.Response
	var err error
	var port []byte

	uri := fmt.Sprintf("http://%s:%d/sticky", url, rPort)
	req, err = http.NewRequest("GET", uri, nil)

	resp, err = client.Do(req)
	c.Assert(err, IsNil)

	port, err = ioutil.ReadAll(resp.Body)

	var session string
	var path string
	for _, cookie := range resp.Cookies() {
		if cookie.Name == "__VCAP_ID__" {
			session = cookie.Value
			path = cookie.Path
		}
	}

	return session, string(port), path
}

func getAppPortWithSticky(url string, rPort uint16, session string, c *C) string {
	var client http.Client
	var req *http.Request
	var resp *http.Response
	var err error
	var port []byte

	uri := fmt.Sprintf("http://%s:%d/sticky", url, rPort)
	req, err = http.NewRequest("GET", uri, nil)

	cookie := &http.Cookie{
		Name:  "__VCAP_ID__",
		Value: session,
	}
	req.AddCookie(cookie)

	resp, err = client.Do(req)
	c.Assert(err, IsNil)

	port, err = ioutil.ReadAll(resp.Body)

	return string(port)
}
