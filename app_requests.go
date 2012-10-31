package router

import (
	"sort"
	"sync"
)

type AppRequests struct {
	sync.Mutex
	requests map[string]*Request
	Total    int
}

type Request struct {
	Url string
	Num int
}

type Requests []*Request

func (r Requests) Len() int {
	return len(r)
}

func (r Requests) Swap(i, j int) {
	r[i], r[j] = r[j], r[i]
}

// We want to sort reversely (bigger values in front),
// so we reverse the order.
func (r Requests) Less(i, j int) bool {
	return r[i].Num > r[j].Num
}

func NewAppRequests() *AppRequests {
	r := new(AppRequests)

	r.requests = make(map[string]*Request)
	r.Total = 0

	return r
}

func (r *AppRequests) Register(app string) {
	r.Lock()
	defer r.Unlock()

	if _, ok := r.requests[app]; !ok {
		r.requests[app] = &Request{app, 0}
	}
}

func (r *AppRequests) Unregister(app string) {
	r.Lock()
	defer r.Unlock()

	delete(r.requests, app)
}

func (r *AppRequests) Size() int {
	r.Lock()
	defer r.Unlock()

	return len(r.requests)
}

func (r *AppRequests) Inc(app string) bool {
	r.Lock()
	defer r.Unlock()

	if _, ok := r.requests[app]; !ok {
		return false
	}

	r.requests[app].Num += 1
	r.Total += 1

	return true
}

func (r *AppRequests) Reset() {
	r.Lock()
	defer r.Unlock()

	r.reset()
}

func (r *AppRequests) reset() {
	r.requests = make(map[string]*Request)
	r.Total = 0
}

func (r *AppRequests) SnapshotAndReset() *AppRequests {
	r.Lock()
	defer r.Unlock()

	snapshot := new(AppRequests)
	snapshot.requests = r.requests
	snapshot.Total = r.Total

	r.reset()

	return snapshot
}

// Sort everything is suboptimal. Since it it the way how router is
// doing, so we do it in the same way.
func (r *AppRequests) SortedSlice() Requests {
	r.Lock()
	defer r.Unlock()

	slice := make(Requests, len(r.requests))
	i := 0
	for _, requests := range r.requests {
		slice[i] = requests
		i++
	}
	sort.Sort(slice)
	return slice
}
