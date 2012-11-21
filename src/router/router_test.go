package router

import (
	"encoding/json"
	"fmt"
	nats "github.com/cloudfoundry/gonats"
	steno "github.com/cloudfoundry/gosteno"
	"io"
	"io/ioutil"
	. "launchpad.net/gocheck"
	"net/http"
	"regexp"
	"router/common"
	"router/common/spec"
	"testing"
	"time"
)

func Test(t *testing.T) {
	config := &steno.Config{
		Sinks: []steno.Sink{},
		Codec: steno.JSON_CODEC,
		Level: steno.LOG_INFO,
	}

	steno.Init(config)

	log = steno.NewLogger("test")

	TestingT(t)
}

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
		Port:    8083,
		Index:   2,
		Pidfile: "/tmp/router_test.pid",
		Nats:    NatsConfig{URI: "nats://localhost:8089"},
		Status:  StatusConfig{8084, "user", "pass"},
		Log:     LogConfig{"info", "/dev/null", ""},
	})

	s.router = NewRouter()
	go s.router.Run()

	s.natsClient = startNATS("localhost:8089", "", "")
}

func (s *RouterSuite) TearDownSuite(c *C) {
	s.router.pidfile.Unlink()
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

func waitMsgReceived(s *RouterSuite, a *TestApp, r bool, t time.Duration) bool {
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
	return waitMsgReceived(s, app, true, timeout)
}

func (s *RouterSuite) waitAppUnregistered(app *TestApp, timeout time.Duration) bool {
	return waitMsgReceived(s, app, false, timeout)
}

func (s *RouterSuite) TestRegisterUnregister(c *C) {
	app := NewTestApp([]Uri{"test.vcap.me"}, uint16(8083), s.natsClient, nil)
	app.Listen()
	c.Assert(s.waitAppUnregistered(app, time.Second*5), Equals, true)

	app.VerifyAppStatus(200, c)

	app.Unregister()
	c.Assert(s.waitAppUnregistered(app, time.Second*5), Equals, true)
	app.VerifyAppStatus(404, c)
}

func (s *RouterSuite) TestTraceHeader(c *C) {
	app := NewTestApp([]Uri{"test.vcap.me"}, uint16(8083), s.natsClient, nil)
	app.Listen()
	c.Assert(s.waitAppRegistered(app, time.Second*5), Equals, true)

	app.VerifyAppStatus(200, c)
	app.VerifyTraceHeader(c)

	app.Unregister()
	c.Assert(s.waitAppUnregistered(app, time.Second*5), Equals, true)
}

func (s *RouterSuite) TestVarz(c *C) {
	app := NewTestApp([]Uri{"count.vcap.me"}, uint16(8083), s.natsClient, map[string]string{"framework": "rails"})
	app.Listen()

	// Record original varz
	varz := s.router.varz
	requests := varz.Requests
	responses2xx := varz.Responses2xx

	metric := varz.Tags["framework"]["rails"]
	tagRequests := 0
	tagResponses2xx := 0
	if metric != nil {
		tagRequests = metric.Requests
		tagResponses2xx = metric.Responses2xx
	}

	// Send requests
	sendRequests("count.vcap.me", uint16(8083), 100)

	// Verify varz update
	requests = varz.Requests - requests
	responses2xx = varz.Responses2xx - responses2xx
	c.Check(requests, Equals, 100)
	c.Check(responses2xx, Equals, 100)

	metric = varz.Tags["framework"]["rails"]
	c.Assert(metric, NotNil)

	tagRequests = metric.Requests - tagRequests
	tagResponses2xx = metric.Responses2xx - tagResponses2xx
	c.Check(tagRequests, Equals, 100)
	c.Check(tagResponses2xx, Equals, 100)

	app.Unregister()
}

func (s *RouterSuite) TestStickySession(c *C) {
	apps := make([]*TestApp, 10)
	for i := 0; i < len(apps); i++ {
		apps[i] = NewTestApp([]Uri{"sticky.vcap.me"}, uint16(8083), s.natsClient, nil)
		apps[i].Listen()
	}

	session, port1 := getSessionAndAppPort("sticky.vcap.me", uint16(8083), c)
	port2 := getAppPortWithSticky("sticky.vcap.me", uint16(8083), session, c)

	c.Check(port1, Equals, port2)

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

type TestApp struct {
	port       uint16 // app listening port
	rPort      uint16 // router listening port
	urls       []Uri  // host registered host name
	natsClient *nats.Client
	tags       map[string]string
}

func NewTestApp(urls []Uri, rPort uint16, natsClient *nats.Client, tags map[string]string) *TestApp {
	app := new(TestApp)

	port, _ := common.GrabEphemeralPort()

	app.port = port
	app.rPort = rPort
	app.urls = urls
	app.natsClient = natsClient
	app.tags = tags

	return app
}

func (a *TestApp) Listen() {
	mux := http.NewServeMux()

	mux.HandleFunc("/", testHandler)
	mux.HandleFunc("/sticky", stickyHandler(a.port))

	server := &http.Server{
		Addr:    fmt.Sprintf(":%d", a.port),
		Handler: mux,
	}

	a.Register()

	go server.ListenAndServe()
}

func testHandler(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(200)
	io.WriteString(w, "Hello, world")
}

func stickyHandler(port uint16) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		cookie := &http.Cookie{
			Name:  "JSESSIONID",
			Value: "xxx",
		}
		http.SetCookie(w, cookie)
		w.WriteHeader(200)

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

func getSessionAndAppPort(url string, rPort uint16, c *C) (string, string) {
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
	for _, cookie := range resp.Cookies() {
		if cookie.Name == "__VCAP_ID__" {
			session = cookie.Value
		}
	}

	return session, string(port)
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
