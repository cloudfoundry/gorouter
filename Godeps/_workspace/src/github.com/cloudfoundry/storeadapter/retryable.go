package storeadapter

import "time"

//go:generate counterfeiter . Sleeper

type Sleeper interface {
	Sleep(time.Duration)
}

//go:generate counterfeiter . RetryPolicy

type RetryPolicy interface {
	DelayFor(uint) (time.Duration, bool)
}

type retryable struct {
	StoreAdapter
	sleeper     Sleeper
	retryPolicy RetryPolicy
}

func NewRetryable(storeAdapter StoreAdapter, sleeper Sleeper, retryPolicy RetryPolicy) StoreAdapter {
	return &retryable{
		StoreAdapter: storeAdapter,
		sleeper:      sleeper,
		retryPolicy:  retryPolicy,
	}
}

func (adapter *retryable) Create(node StoreNode) error {
	return adapter.retry(func() error {
		return adapter.StoreAdapter.Create(node)
	})
}

func (adapter *retryable) Update(node StoreNode) error {
	return adapter.retry(func() error {
		return adapter.StoreAdapter.Update(node)
	})
}

func (adapter *retryable) CompareAndSwap(nodeA StoreNode, nodeB StoreNode) error {
	return adapter.retry(func() error {
		return adapter.StoreAdapter.CompareAndSwap(nodeA, nodeB)
	})
}

func (adapter *retryable) CompareAndSwapByIndex(index uint64, node StoreNode) error {
	return adapter.retry(func() error {
		return adapter.StoreAdapter.CompareAndSwapByIndex(index, node)
	})
}

func (adapter *retryable) SetMulti(nodes []StoreNode) error {
	return adapter.retry(func() error {
		return adapter.StoreAdapter.SetMulti(nodes)
	})
}

func (adapter *retryable) Get(key string) (StoreNode, error) {
	var node StoreNode
	err := adapter.retry(func() error {
		var err error
		node, err = adapter.StoreAdapter.Get(key)
		return err
	})

	return node, err
}

func (adapter *retryable) Delete(keys ...string) error {
	return adapter.retry(func() error {
		return adapter.StoreAdapter.Delete(keys...)
	})
}

func (adapter *retryable) DeleteLeaves(keys ...string) error {
	return adapter.retry(func() error {
		return adapter.StoreAdapter.DeleteLeaves(keys...)
	})
}

func (adapter *retryable) ListRecursively(key string) (StoreNode, error) {
	var node StoreNode
	err := adapter.retry(func() error {
		var err error
		node, err = adapter.StoreAdapter.ListRecursively(key)
		return err
	})

	return node, err
}

func (adapter *retryable) CompareAndDelete(nodes ...StoreNode) error {
	return adapter.retry(func() error {
		return adapter.StoreAdapter.CompareAndDelete(nodes...)
	})
}

func (adapter *retryable) CompareAndDeleteByIndex(nodes ...StoreNode) error {
	return adapter.retry(func() error {
		return adapter.StoreAdapter.CompareAndDeleteByIndex(nodes...)
	})
}

func (adapter *retryable) UpdateDirTTL(dir string, ttl uint64) error {
	return adapter.retry(func() error {
		return adapter.StoreAdapter.UpdateDirTTL(dir, ttl)
	})
}

func (adapter *retryable) retry(action func() error) error {
	var err error

	var failedAttempts uint
	for {
		err = action()
		if err != ErrorTimeout {
			break
		}

		failedAttempts++

		delay, keepRetrying := adapter.retryPolicy.DelayFor(failedAttempts)
		if !keepRetrying {
			break
		}

		adapter.sleeper.Sleep(delay)
	}

	return err
}
