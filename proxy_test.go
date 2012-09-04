package router

import (
	. "launchpad.net/gocheck"
	"net/http"
)

type ProxySuite struct {
	proxy *Proxy
}

var _ = Suite(&ProxySuite{})

var fooReg = &registerMessage{
	Host: "192.168.1.1",
	Port: 1234,
	Uris: []string{"foo.vcap.me", "fooo.vcap.me"},
	Tags: map[string]string{
		"runtime":   "ruby18",
		"framework": "sinatra",
	},
	Dea: "",
	App: 12345,
}

var barReg = &registerMessage{
	Host: "192.168.1.2",
	Port: 4321,
	Uris: []string{"bar.vcap.me", "barr.vcap.me"},
	Tags: map[string]string{
		"runtime":   "javascript",
		"framework": "node",
	},
	Dea: "",
	App: 54321,
}

var upperFooReg = &registerMessage{
	Host: "192.168.1.1",
	Port: 1234,
	Uris: []string{"FOO.VCAP.ME"},
	Tags: map[string]string{
		"runtime":   "ruby18",
		"framework": "sinatra",
	},
	Dea: "",
	App: 12345,
}

var bar2Reg = &registerMessage{
	Host: "192.168.1.3",
	Port: 1234,
	Uris: []string{"bar.vcap.me", "barr.vcap.me"},
	Tags: map[string]string{
		"runtime":   "javascript",
		"framework": "node",
	},
	Dea: "",
	App: 54321,
}

var emptyReg = &registerMessage{}

func (s *ProxySuite) SetUpTest(c *C) {
	s.proxy = NewProxy(nil)
}

func (s *ProxySuite) TestReg(c *C) {
	s.proxy.Register(fooReg)
	c.Check(len(s.proxy.r), Equals, 2)

	s.proxy.Register(barReg)
	c.Check(len(s.proxy.r), Equals, 4)

	s.proxy.Register(emptyReg)
	c.Check(len(s.proxy.r), Equals, 4)
}

func (s *ProxySuite) TestUnreg(c *C) {
	s.proxy.Register(fooReg)
	c.Check(len(s.proxy.r), Equals, 2)

	s.proxy.Unregister(fooReg)
	c.Check(len(s.proxy.r), Equals, 0)
}

func (s *ProxySuite) TestRegIgnoreDuplication(c *C) {
	s.proxy.Register(barReg)
	s.proxy.Register(barReg)
	s.proxy.Register(bar2Reg)

	c.Check(len(s.proxy.r), Equals, 2)

	req := &http.Request{
		Host: "bar.vcap.me",
	}
	rms := s.proxy.lookup(req)

	c.Assert(rms, NotNil)
	c.Check(len(rms), Equals, 2)
}

func (s *ProxySuite) TestRegUppercase(c *C) {
	s.proxy.Register(upperFooReg)

	req := &http.Request{
		Host: "foo.vcap.me",
	}

	m := s.proxy.Lookup(req)
	c.Assert(m, NotNil)
	c.Check(m.Host, Equals, "192.168.1.1")
	c.Check(m.Port, Equals, uint16(1234))
}

func (s *ProxySuite) TestLookup(c *C) {
	s.proxy.Register(fooReg)

	req := &http.Request{
		Host: "foo.vcap.me",
	}
	m := s.proxy.Lookup(req)
	c.Assert(m, NotNil)
	c.Check(m.Host, Equals, "192.168.1.1")
	c.Check(m.Port, Equals, uint16(1234))
}

func (s *ProxySuite) TestLookupUppercase(c *C) {
	s.proxy.Register(fooReg)

	req := &http.Request{
		Host: "FOO.VCAP.ME",
	}
	m := s.proxy.Lookup(req)
	c.Assert(m, NotNil)
	c.Check(m.Host, Equals, "192.168.1.1")
	c.Check(m.Port, Equals, uint16(1234))
}

func (s *ProxySuite) TestLookupDoubleRegister(c *C) {
	s.proxy.Register(barReg)
	s.proxy.Register(bar2Reg)

	req := &http.Request{
		Host: "bar.vcap.me",
	}
	rms := s.proxy.lookup(req)

	c.Assert(rms, NotNil)
	c.Check(len(rms), Equals, 2)
}
