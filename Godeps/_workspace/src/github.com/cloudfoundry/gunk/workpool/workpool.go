package workpool

import "sync"

type AroundWork interface {
	Around(func())
}

type AroundWorkFunc func(work func())

func (f AroundWorkFunc) Around(work func()) {
	f(work)
}

type WorkPool struct {
	workChan chan func()
	stopped  chan struct{}
	around   AroundWork
	wg       *sync.WaitGroup
}

var DefaultAround = AroundWorkFunc(func(work func()) {
	work()
})

func NewWorkPool(workers int) *WorkPool {
	// Pending = 1 to provide a weak FIFO guarantee
	return New(workers, 1, AroundWorkFunc(DefaultAround))
}

func New(workers, pending int, aroundWork AroundWork) *WorkPool {
	workChan := make(chan func(), pending)
	wg := new(sync.WaitGroup)

	w := &WorkPool{
		workChan: workChan,
		stopped:  make(chan struct{}),
		around:   aroundWork,
		wg:       wg,
	}

	for i := 0; i < workers; i++ {
		wg.Add(1)
		go worker(workChan, w)
	}

	return w
}

func (w *WorkPool) Submit(work func()) {
	select {
	case <-w.stopped:
	case w.workChan <- work:
	}
}

func (w *WorkPool) Stop() {
	select {
	case <-w.stopped:
	default:
		close(w.stopped)
	}

	w.wg.Wait()
}

func worker(workChan chan func(), w *WorkPool) {
	defer w.wg.Done()

	for {
		select {
		case <-w.stopped:
			return
		case work := <-w.workChan:
			w.around.Around(work)
		}
	}
}
