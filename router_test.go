package router

import (
	"encoding/json"
	"fmt"
	nats "github.com/cloudfoundry/gonats"
	"io"
	"io/ioutil"
	. "launchpad.net/gocheck"
	"log"
	"net/http"
	"os"
	"regexp"
	"router/common"
	"router/common/spec"
	"runtime"
	"strconv"
	"testing"
	"time"
)

func Test(t *testing.T) {
	file, _ := os.OpenFile("/dev/null", os.O_WRONLY, 0666)
	log.SetOutput(file)

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
		Port:       8083,
		Index:      2,
		Pidfile:    "/tmp/router_test.pid",
		SessionKey: "14fbc303b76bacd1e0a3ab641c11d114",
		Nats:       NatsConfig{URI: "nats://localhost:8089"},
		Status:     StatusConfig{8084, "user", "pass"},
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
	var varz common.Varz
	var emptyStats runtime.MemStats

	// Verify varz
	vbody := verifyZ(cc.Host, "/varz", cc.Credentials[0], cc.Credentials[1], c)
	defer vbody.Close()
	b, err = ioutil.ReadAll(vbody)
	c.Check(err, IsNil)
	json.Unmarshal(b, &varz)

	c.Check(varz.Start, Equals, cc.Start)
	c.Check(varz.Uptime > 0, Equals, true)
	c.Check(varz.NumCores > 0, Equals, true)
	c.Check(varz.Var, NotNil)
	c.Check(varz.Config, NotNil)
	c.Check(varz.MemStats, Not(Equals), emptyStats)

	// Verify healthz
	hbody := verifyZ(cc.Host, "/healthz", cc.Credentials[0], cc.Credentials[1], c)
	defer hbody.Close()
	b, err = ioutil.ReadAll(hbody)
	match, _ := regexp.Match("ok", b)

	c.Check(err, IsNil)
	c.Check(match, Equals, true)
}

func (s *RouterSuite) TestRegisterUnregister(c *C) {
	app := NewTestApp([]string{"test.vcap.me"}, uint16(8083), s.natsClient)
	app.Listen()
	app.VerifyAppStatus(200, c)

	app.Unregister()
	app.VerifyAppStatus(404, c)
}

func (s *RouterSuite) TestStickySession(c *C) {
	apps := make([]*TestApp, 10)
	for i := 0; i < len(apps); i++ {
		apps[i] = NewTestApp([]string{"sticky.vcap.me"}, uint16(8083), s.natsClient)
		apps[i].Listen()
	}

	session, port1 := sendRequest("sticky.vcap.me", uint16(8083), c)
	port2 := sendRequestWithSticky("sticky.vcap.me", uint16(8083), session, c)

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
	port       uint16   // app listening port
	urls       []string // host registered host name
	natsClient *nats.Client
	rPort      uint16 // router listening port
}

func NewTestApp(urls []string, rPort uint16, natsClient *nats.Client) *TestApp {
	app := new(TestApp)

	port, _ := common.GrabEphemeralPort()
	pi, _ := strconv.Atoi(port)
	app.port = uint16(pi)
	app.rPort = rPort
	app.urls = urls
	app.natsClient = natsClient

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
	var rm = registerMessage{
		"localhost", a.port, a.urls, nil, "dea", "0", "",
	}
	b, _ := json.Marshal(rm)
	a.natsClient.Publish("router.register", b)
}

func (a *TestApp) Unregister() {
	var rm = registerMessage{
		"localhost", a.port, a.urls, nil, "dea", "0", "",
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

func sendRequest(url string, rPort uint16, c *C) (string, string) {
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

func sendRequestWithSticky(url string, rPort uint16, session string, c *C) string {
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
