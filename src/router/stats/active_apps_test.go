package stats

import (
	. "launchpad.net/gocheck"
	"time"
)

type ActiveAppsSuite struct {
	*ActiveApps
}

var _ = Suite(&ActiveAppsSuite{})

func (s *ActiveAppsSuite) SetUpTest(c *C) {
	s.ActiveApps = NewActiveApps()
}

func (s *ActiveAppsSuite) TestMark(c *C) {
	s.Mark("a", time.Unix(1, 0))
	c.Check(len(s.h), Equals, 1)

	s.Mark("b", time.Unix(1, 0))
	c.Check(len(s.h), Equals, 2)

	s.Mark("b", time.Unix(2, 0))
	c.Check(len(s.h), Equals, 2)
}

func (s *ActiveAppsSuite) TestTrim(c *C) {
	for i, x := range []string{"a", "b", "c"} {
		s.Mark(x, time.Unix(int64(i), 0))
	}

	c.Check(len(s.h), Equals, 3)

	s.Trim(time.Unix(1, 0))
	c.Check(len(s.h), Equals, 2)

	s.Trim(time.Unix(2, 0))
	c.Check(len(s.h), Equals, 1)

	s.Trim(time.Unix(3, 0))
	c.Check(len(s.h), Equals, 0)

	s.Trim(time.Unix(4, 0))
	c.Check(len(s.h), Equals, 0)
}

func (s *ActiveAppsSuite) TestActiveSince(c *C) {
	s.Mark("a", time.Unix(1, 0))
	c.Check(s.ActiveSince(time.Unix(1, 0)), DeepEquals, []string{"a"})
	c.Check(s.ActiveSince(time.Unix(3, 0)), DeepEquals, []string{})
	c.Check(s.ActiveSince(time.Unix(5, 0)), DeepEquals, []string{})

	s.Mark("b", time.Unix(3, 0))
	c.Check(s.ActiveSince(time.Unix(1, 0)), DeepEquals, []string{"a", "b"})
	c.Check(s.ActiveSince(time.Unix(3, 0)), DeepEquals, []string{"b"})
	c.Check(s.ActiveSince(time.Unix(5, 0)), DeepEquals, []string{})
}
