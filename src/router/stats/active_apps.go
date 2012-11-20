package stats

import (
	"container/heap"
	"sync"
	"time"
)

const (
	ActiveAppsTrimInterval  = 1 * time.Minute
	ActiveAppsEntryLifetime = 30 * time.Minute
)

type activeAppsEntry struct {
	t  int64 // Last update
	ti int   // Index in time min-heap
	tj int   // Index in time max-heap

	ApplicationId string
}

func (x *activeAppsEntry) Mark(t int64) {
	if x.t < t {
		x.t = t
	}
}

type activeAppsHeap struct {
	s []*activeAppsEntry
}

func (x *activeAppsHeap) Len() int {
	return len(x.s)
}

func (x *activeAppsHeap) Swap(i, j int) {
	x.s[i], x.s[j] = x.s[j], x.s[i]
	x.SetIndex(i, i)
	x.SetIndex(j, j)
}

func (x *activeAppsHeap) Push(a interface{}) {
	x.s = append(x.s, a.(*activeAppsEntry))
	x.SetIndex(len(x.s)-1, len(x.s)-1)
}

func (x *activeAppsHeap) Pop() interface{} {
	x.SetIndex(len(x.s)-1, -1)
	z := x.s[len(x.s)-1]
	x.s = x.s[0 : len(x.s)-1]
	return z
}

func (x *activeAppsHeap) SetIndex(i, j int) {
	// No-op
}

func (x *activeAppsHeap) Copy() []*activeAppsEntry {
	y := make([]*activeAppsEntry, len(x.s))
	copy(y, x.s)
	return y
}

type byTimeMinHeap struct{ activeAppsHeap }

func (x *byTimeMinHeap) Less(i, j int) bool {
	return x.s[i].t < x.s[j].t
}

func (x *byTimeMinHeap) SetIndex(i, j int) {
	x.s[i].ti = j
}

type byTimeMinHeapReadOnly struct{ byTimeMinHeap }

func (x *byTimeMinHeapReadOnly) SetIndex(i, j int) {
	// No-op
}

type byTimeMaxHeap struct{ activeAppsHeap }

func (x *byTimeMaxHeap) Less(i, j int) bool {
	return x.s[i].t > x.s[j].t
}

func (x *byTimeMaxHeap) SetIndex(i, j int) {
	x.s[i].tj = j
}

type byTimeMaxHeapReadOnly struct{ byTimeMaxHeap }

func (x *byTimeMaxHeapReadOnly) SetIndex(i, j int) {
	// No-op
}

type ActiveApps struct {
	sync.Mutex

	t *time.Ticker

	m map[string]*activeAppsEntry
	i byTimeMinHeap
	j byTimeMaxHeap
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

func (x *ActiveApps) Mark(ApplicationId string, z time.Time) {
	t := z.Unix()

	x.Lock()
	defer x.Unlock()

	y := x.m[ApplicationId]
	if y != nil {
		heap.Remove(&x.i, y.ti)
		heap.Remove(&x.j, y.tj)
	} else {
		// New entry
		y = &activeAppsEntry{ApplicationId: ApplicationId}
		x.m[ApplicationId] = y
	}

	y.Mark(t)

	heap.Push(&x.i, y)
	heap.Push(&x.j, y)
}

func (x *ActiveApps) Trim(y time.Time) {
	t := y.Unix()

	x.Lock()
	defer x.Unlock()

	for x.i.Len() > 0 {
		// Pop from the min-heap
		z := heap.Pop(&x.i).(*activeAppsEntry)
		if z.t > t {
			// Push back to the min-heap
			heap.Push(&x.i, z)
			break
		}

		// Remove from max-heap
		heap.Remove(&x.j, z.tj)

		// Remove from map
		delete(x.m, z.ApplicationId)
	}
}

func (x *ActiveApps) ActiveSince(y time.Time) []string {
	t := y.Unix()

	x.Lock()
	defer x.Unlock()

	a := byTimeMaxHeapReadOnly{}
	a.s = x.j.Copy()

	// Collect active applications
	b := make([]string, 0)
	for a.Len() > 0 {
		z := heap.Pop(&a).(*activeAppsEntry)
		if z.t < t {
			break
		}

		// Add active application
		b = append(b, z.ApplicationId)
	}

	return b
}
