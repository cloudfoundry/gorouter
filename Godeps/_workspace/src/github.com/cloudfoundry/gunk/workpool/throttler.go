package workpool

import (
	"fmt"
	"sync"
)

type Throttler struct {
	pool  *WorkPool
	works []func()
}

func NewThrottler(maxWorkers int, works []func()) (*Throttler, error) {
	if maxWorkers < 1 {
		return nil, fmt.Errorf("must provide positive maxWorkers; provided %d", maxWorkers)
	}

	var pool *WorkPool
	if len(works) < maxWorkers {
		pool = newWorkPoolWithPending(len(works), 0)
	} else {
		pool = newWorkPoolWithPending(maxWorkers, len(works)-maxWorkers)
	}

	return &Throttler{
		pool:  pool,
		works: works,
	}, nil
}

func (t *Throttler) Work() {
	defer t.pool.Stop()

	wg := sync.WaitGroup{}
	wg.Add(len(t.works))
	for _, work := range t.works {
		work := work
		t.pool.Submit(func() {
			defer wg.Done()
			work()
		})
	}
	wg.Wait()
}
