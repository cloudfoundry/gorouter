package router

import (
	. "launchpad.net/gocheck"
	"math/rand"
	"strconv"
	"testing"
	"time"
)

type RequestsSuite struct {
}

var _ = Suite(&RequestsSuite{})

func (s *RequestsSuite) SetUpSuite(c *C) {
	rand.Seed(time.Now().Unix())
}

func (s *RequestsSuite) TestRequestsRegister(c *C) {
	apps := NewAppRequests()
	c.Check(apps.Size(), Equals, 0)

	apps.Register("app1")
	c.Check(apps.Size(), Equals, 1)
	apps.Register("app2")
	c.Check(apps.Size(), Equals, 2)
	apps.Register("app1")
	c.Check(apps.Size(), Equals, 2)

	apps.Unregister("app1")
	c.Check(apps.Size(), Equals, 1)
}

func (s *RequestsSuite) TestRequestsInc(c *C) {
	apps := initRandomAppRequests(10, 100)

	total := 0
	for _, r := range apps.requests {
		total += r.Num
	}
	c.Check(apps.Size(), Equals, 10)
	c.Check(total, Equals, apps.Total)
	c.Check(total, Equals, 100)
}

func (s *RequestsSuite) TestRequestsSorted(c *C) {
	apps := initRandomAppRequests(10, 1000)

	requests := apps.SortedSlice()
	c.Check(len(requests), Equals, apps.Size())
	c.Check(len(requests), Equals, 10)

	// Make sure the slice is sorted
	for i := 0; i < len(requests)-1; i++ {
		c.Check(requests[i].Num >= requests[i+1].Num, Equals, true)
	}
}

func initRandomAppRequests(numApp, numRequests int) *AppRequests {
	apps := NewAppRequests()

	for i := 0; i < numApp; i++ {
		apps.Register(strconv.Itoa(i))
	}

	for i := 0; i < numRequests; i++ {
		num := rand.Intn(numApp)
		apps.Inc(strconv.Itoa(num))
	}

	return apps
}

func BenchmarkSort10000Items(b *testing.B) {

	for i := 0; i < b.N; i++ {
		b.StopTimer()
		apps := NewAppRequests()
		for i := 0; i < 10000; i++ {
			app := strconv.Itoa(i)
			apps.Register(app)
			apps.requests[app].Num = rand.Intn(100000)
		}
		b.StartTimer()

		apps.SortedSlice()
	}
}
