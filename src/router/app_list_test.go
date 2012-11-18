package router

import (
	"encoding/json"
	. "launchpad.net/gocheck"
	"strconv"
	"testing"
)

type AppListSuite struct {
	appList *AppList
}

var _ = Suite(&AppListSuite{})

func (s *AppListSuite) SetUpTest(c *C) {
	s.appList = NewAppList()
}

func (s *AppListSuite) TestSize(c *C) {
	c.Check(s.appList.Size(), Equals, 0)

	s.appList.Insert("1")
	s.appList.Insert("2")
	s.appList.Insert("3")
	c.Check(s.appList.Size(), Equals, 3)
}

func (s *AppListSuite) TestRemove(c *C) {
	c.Check(s.appList.Size(), Equals, 0)

	s.appList.Insert("1")
	s.appList.Insert("2")
	s.appList.Insert("3")
	s.appList.Remove("3")
	s.appList.Remove("4")

	c.Check(s.appList.Size(), Equals, 2)
}

func (s *AppListSuite) TestMarshal(c *C) {
	s.appList.Insert("1")
	s.appList.Insert("2")
	s.appList.Insert("3")

	bytes, err := json.Marshal(s.appList)
	c.Assert(err, IsNil)

	appList := NewAppList()
	err = json.Unmarshal(bytes, appList)
	c.Assert(err, IsNil)

	c.Check(appList.Contain("1"), Equals, true)
	c.Check(appList.Contain("2"), Equals, true)
	c.Check(appList.Contain("3"), Equals, true)
	c.Check(appList.Contain("4"), Equals, false)
}

func (s *AppListSuite) TestEncode(c *C) {
	s.appList.Insert("1")
	s.appList.Insert("2")
	s.appList.Insert("3")

	_, err := s.appList.Encode()
	c.Assert(err, IsNil)
}

func (s *AppListSuite) TestEncodeAndReset(c *C) {
	s.appList.Insert("1")
	s.appList.Insert("2")
	s.appList.Insert("3")

	_, err := s.appList.EncodeAndReset()
	c.Assert(err, IsNil)
	c.Check(s.appList.Size(), Equals, 0)
}

func (s *AppListSuite) TestDecode(c *C) {
	s.appList.Insert("1")
	s.appList.Insert("2")
	s.appList.Insert("3")

	b, err := s.appList.Encode()
	c.Assert(err, IsNil)

	var appList *AppList
	appList, err = DecodeAppList(b)
	c.Assert(err, IsNil)

	c.Check(appList.Size(), Equals, 3)
	c.Check(appList.Contain("1"), Equals, true)
	c.Check(appList.Contain("2"), Equals, true)
	c.Check(appList.Contain("3"), Equals, true)
	c.Check(appList.Contain("4"), Equals, false)
}

func BenchmarkInsert(b *testing.B) {
	b.StopTimer()
	appList := NewAppList()
	b.StartTimer()

	for i := 0; i < b.N; i++ {
		appList.Insert(strconv.Itoa(i))
	}
}

// In this benchmark, we time the longest running goroutine.
// It's not perfect, but it is what it is now.
func BenchmarkConcurrentInsert(b *testing.B) {
	b.StopTimer()
	appList := NewAppList()
	b.StartTimer()

	c := make(chan bool)
	n := 10 // We use 10 goroutines

	for j := 0; j < n; j++ {
		go func() {
			for i := 0; i < b.N; i++ {
				appList.Insert(strconv.Itoa(i))
			}
			c <- true
		}()
	}

	// join all 5 routines
	for j := 0; j < n; j++ {
		<-c
	}
}
