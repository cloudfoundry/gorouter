package router

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	nats "github.com/cloudfoundry/gonats"
	"io"
	"io/ioutil"
	. "launchpad.net/gocheck"
	"net"
	"net/http"
	"regexp"
	"router/common"
	"router/common/spec"
	"router/config"
	"router/test"
	"strings"
	"time"
)

type RouterSuite struct {
	Config     *config.Config
	natsServer *spec.NatsServer
	natsClient *nats.Client
	router     *Router
	proxyPort  uint16
}

var _ = Suite(&RouterSuite{})

func (s *RouterSuite) SetUpSuite(c *C) {
	natsPort := nextAvailPort()
	s.natsServer = spec.NewNatsServer(natsPort, "/tmp/router_nats_test.pid")
	err := s.natsServer.Start()
	c.Assert(err, IsNil)

	s.proxyPort = nextAvailPort()
	statusPort := nextAvailPort()

	s.Config = config.DefaultConfig()

	s.Config.Port = s.proxyPort
	s.Config.Index = 2
	s.Config.TraceKey = "my_trace_key"

	// Hardcode the IP to localhost to avoid leaving the machine while running tests
	s.Config.Ip = "127.0.0.1"

	s.Config.PublishStartMessageInterval = 10 * time.Millisecond
	s.Config.PruneStaleDropletsInterval = 0
	s.Config.DropletStaleThreshold = 0
	s.Config.PublishActiveAppsInterval = 0

	s.Config.Status = config.StatusConfig{
		Port: statusPort,
		User: "user",
		Pass: "pass",
	}

	s.Config.Nats = config.NatsConfig{
		Host: fmt.Sprintf("localhost:%d", natsPort),
	}

	s.Config.Logging = config.LoggingConfig{
		File:  "/dev/null",
		Level: "info",
	}

	s.router = NewRouter(s.Config)
	go s.router.Run()

	s.natsClient = startNATS(fmt.Sprintf("localhost:%d", natsPort), "", "")
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

func (s *RouterSuite) waitMsgReceived(a *test.TestApp, r bool, t time.Duration) bool {
	i := time.Millisecond * 50
	m := int(t / i)

	for j := 0; j < m; j++ {
		received := true
		for _, v := range a.Urls() {
			_, ok := s.router.registry.Lookup(v)
			if ok != r {
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

func (s *RouterSuite) waitAppRegistered(app *test.TestApp, timeout time.Duration) bool {
	return s.waitMsgReceived(app, true, timeout)
}

func (s *RouterSuite) waitAppUnregistered(app *test.TestApp, timeout time.Duration) bool {
	return s.waitMsgReceived(app, false, timeout)
}

func (s *RouterSuite) TestRegisterUnregister(c *C) {
	app := test.NewGreetApp([]string{"test.vcap.me"}, s.proxyPort, s.natsClient, nil)
	app.Listen()
	c.Assert(s.waitAppRegistered(app, time.Second*5), Equals, true)

	app.VerifyAppStatus(200, c)

	app.Unregister()
	c.Assert(s.waitAppUnregistered(app, time.Second*5), Equals, true)
	app.VerifyAppStatus(404, c)
}

func (s *RouterSuite) TestTraceHeader(c *C) {
	app := test.NewGreetApp([]string{"test.vcap.me"}, s.proxyPort, s.natsClient, nil)
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
	app := test.NewGreetApp([]string{"count.vcap.me"}, s.proxyPort, s.natsClient, map[string]string{"framework": "rails"})
	app.Listen()

	c.Assert(s.waitAppRegistered(app, time.Millisecond*500), Equals, true)
	// Send seed request
	sendRequests(c, "count.vcap.me", s.proxyPort, 1)
	vA := s.readVarz()

	// Send requests
	sendRequests(c, "count.vcap.me", s.proxyPort, 100)
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
	apps := make([]*test.TestApp, 10)
	for i := range apps {
		apps[i] = test.NewStickyApp([]string{"sticky.vcap.me"}, s.proxyPort, s.natsClient, nil)
		apps[i].Listen()
	}

	for _, app := range apps {
		c.Assert(s.waitAppRegistered(app, time.Millisecond*500), Equals, true)
	}
	session, port1, path := getSessionAndAppPort("sticky.vcap.me", s.proxyPort, c)
	port2 := getAppPortWithSticky("sticky.vcap.me", s.proxyPort, session, c)

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
	c.Assert(resp, Not(IsNil))
	c.Check(resp.StatusCode, Equals, 401)

	// varz Basic auth
	req.SetBasicAuth(user, pass)
	resp, err = client.Do(req)
	c.Check(err, IsNil)
	c.Assert(resp, Not(IsNil))
	c.Check(resp.StatusCode, Equals, 200)

	return resp.Body
}

func (s *RouterSuite) TestRouterRunErrors(c *C) {
	c.Assert(func() { s.router.Run() }, PanicMatches, "net.Listen.*")
}

func (s *RouterSuite) TestProxyPutRequest(c *C) {
	app := test.NewTestApp([]string{"greet.vcap.me"}, s.proxyPort, s.natsClient, nil)

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

	url := app.Endpoint()

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

func (s *RouterSuite) Test100ContinueRequest(c *C) {
	app := test.NewTestApp([]string{"foo.vcap.me"}, s.proxyPort, s.natsClient, nil)
	rCh := make(chan *http.Request)
	app.AddHandler("/", func(w http.ResponseWriter, r *http.Request) {
		_, err := ioutil.ReadAll(r.Body)
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
		}
		rCh <- r
	})
	app.Listen()
	c.Assert(s.waitAppRegistered(app, time.Second*5), Equals, true)

	host := fmt.Sprintf("foo.vcap.me:%d", s.proxyPort)
	conn, err := net.Dial("tcp", host)
	c.Assert(err, IsNil)
	defer conn.Close()

	fmt.Fprintf(conn, "POST / HTTP/1.1\r\n"+
		"Host: %s\r\n"+
		"Connection: close\r\n"+
		"Content-Length: 1\r\n"+
		"Expect: 100-continue\r\n"+
		"\r\n", host)

	fmt.Fprintf(conn, "a")

	buf := bufio.NewReader(conn)
	line, err := buf.ReadString('\n')
	c.Assert(err, IsNil)
	c.Assert(strings.Contains(line, "100 Continue"), Equals, true)

	rr := <-rCh
	c.Assert(rr, NotNil)
	c.Assert(rr.Header.Get("Expect"), Equals, "")
}

func sendRequests(c *C, url string, rPort uint16, times int) {
	uri := fmt.Sprintf("http://%s:%d", url, rPort)

	for i := 0; i < times; i++ {
		r, err := http.Get(uri)
		if err != nil {
			panic(err)
		}

		c.Check(r.StatusCode, Equals, http.StatusOK)
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

func nextAvailPort() uint16 {
	p, e := common.GrabEphemeralPort()
	if e != nil {
		panic(e)
	}
	return p
}
