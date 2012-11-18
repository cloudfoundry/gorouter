package common

import (
	. "launchpad.net/gocheck"
	"os"
	"syscall"
)

const PIDFILE = "./prog.pid"

type PidFileSuite struct {
}

var _ = Suite(&PidFileSuite{})

func (s *PidFileSuite) SetUpSuite(c *C) {
	syscall.Unlink(PIDFILE)
}

func (s *PidFileSuite) TestNewPidFile(c *C) {
	pidfile, err := NewPidFile(PIDFILE)
	c.Assert(err, IsNil)

	checkPid(pidfile, c)

	pidfile.Unlink()
}

func (s *PidFileSuite) TestNewPidFileAlreadyThere(c *C) {
	// Create an empty pidfile
	f, _ := os.Create(PIDFILE)
	f.Close()

	pidfile, err := NewPidFile(PIDFILE)
	c.Assert(err, IsNil)

	checkPid(pidfile, c)

	pidfile.Unlink()
}

func (s *PidFileSuite) TestNewPidFileTwoTimes(c *C) {
	pidfile1, err1 := NewPidFile(PIDFILE)
	pidfile2, err2 := NewPidFile(PIDFILE)

	c.Assert(err1, IsNil)
	c.Assert(err2, IsNil)

	c.Check(pidfile1, DeepEquals, pidfile2)

	checkPid(pidfile1, c)

	pidfile1.Unlink()
}

func (s *PidFileSuite) TestNewPidFileCreated(c *C) {
	// Create a pidfile with valid pid in it
	f, _ := os.Create(PIDFILE)
	f.Write([]byte("1"))
	f.Close()

	_, err := NewPidFile(PIDFILE)
	c.Assert(err, NotNil)

	syscall.Unlink(PIDFILE)
}

func checkPid(pidfile *PidFile, c *C) {
	file, _ := os.Open(pidfile.file)
	pid, err := readPid(file)
	c.Assert(err, IsNil)
	c.Check(pid, Equals, syscall.Getpid())
}
