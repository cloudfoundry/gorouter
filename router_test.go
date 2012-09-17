package router

import (
	"encoding/json"
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
