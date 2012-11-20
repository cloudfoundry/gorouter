package stats

import (
	"container/heap"
	"sort"
	"sync"
	"time"
)

const (
	ActiveAppsTrimInterval  = 1 * time.Minute
	ActiveAppsEntryLifetime = 30 * time.Minute
)

type activeAppsEntry struct {
	t  time.Time // Last update
	ti int       // Index in time heap

	ApplicationId string
}

func (x *activeAppsEntry) Mark(t time.Time) {
	if x.t.Before(t) {
		x.t = t
	}
}

type byTimeHeap []*activeAppsEntry

func (x byTimeHeap) Len() int {
	return len(x)
}

func (x byTimeHeap) Less(i, j int) bool {
	return x[i].t.Before(x[j].t)
}

func (x byTimeHeap) Swap(i, j int) {
	x[i], x[j] = x[j], x[i]
	x[i].ti = i
	x[j].ti = j
}

func (x *byTimeHeap) Push(a interface{}) {
	y := *x
	b := a.(*activeAppsEntry)
	b.ti = len(y)
	*x = append(y, b)
}

func (x *byTimeHeap) Pop() interface{} {
	y := *x
	n := len(y)
	z := y[n-1]
	z.ti = -1
	*x = y[0 : n-1]
	return z
}

type ActiveApps struct {
	sync.Mutex

	t *time.Ticker

	m map[string]*activeAppsEntry
	h byTimeHeap
}

func NewActiveApps() *ActiveApps {
	x := &ActiveApps{}

	x.t = time.NewTicker(1 * time.Minute)

	x.m = make(map[string]*activeAppsEntry)

	go func() {
		for {
			select {
			case <-x.t.C:
				x.Trim(time.Now().Add(-ActiveAppsEntryLifetime))
			}
		}
	}()

	return x
}

func (x *ActiveApps) heapRemove(y *activeAppsEntry) {
	z := heap.Remove(&x.h, y.ti).(*activeAppsEntry)
	if z != y {
		panic("expected z == y")
	}
}

func (x *ActiveApps) heapAdd(y *activeAppsEntry) {
	heap.Push(&x.h, y)
}

func (x *ActiveApps) Mark(ApplicationId string, t time.Time) {
	x.Lock()
	defer x.Unlock()

	y := x.m[ApplicationId]
	if y != nil {
		x.heapRemove(y)
	} else {
		// New entry
		y = &activeAppsEntry{ApplicationId: ApplicationId}
		x.m[ApplicationId] = y
	}

	y.Mark(t)

	x.heapAdd(y)
}

func (x *ActiveApps) Trim(t time.Time) {
	var i, j int

	x.Lock()
	defer x.Unlock()

	// Find index of first entry with t' >= t
	i = sort.Search(len(x.h), func(i int) bool { return !x.h[i].t.Before(t) })

	// Remove entries with t' < t from map
	for j = 0; j < i; j++ {
		delete(x.m, x.h[0].ApplicationId)
		x.heapRemove(x.h[0])
	}
}

func (x *ActiveApps) ActiveSince(t time.Time) []string {
	var i, j int

	x.Lock()
	defer x.Unlock()

	// Find index of first entry with t' >= t
	i = sort.Search(len(x.h), func(i int) bool { return !x.h[i].t.Before(t) })

	// Collect active applications
	h := x.h[i:]
	y := make([]string, len(h))
	for j = 0; j < len(y); j++ {
		y[j] = h[j].ApplicationId
	}

	return y
}
