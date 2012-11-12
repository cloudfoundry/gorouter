package common

import (
	. "launchpad.net/gocheck"
	"os"
	"testing"
)

func Test(t *testing.T) {
	TestingT(t)
}

type CommonSuite struct {
}

var _ = Suite(&CommonSuite{})

func (s *CommonSuite) TestProcessExist(c *C) {
	c.Check(ProcessExist(1), Equals, true)
	c.Check(ProcessExist(os.Getppid()), Equals, true)
	c.Check(ProcessExist(os.Getpid()), Equals, true)

	c.Check(ProcessExist(0), Equals, false)
	c.Check(ProcessExist(-1), Equals, false)
	c.Check(ProcessExist(999999), Equals, false)
}

func (s *CommonSuite) TestUUID(c *C) {
	uuid := GenerateUUID()

	c.Check(len(uuid), Equals, 32)
}
