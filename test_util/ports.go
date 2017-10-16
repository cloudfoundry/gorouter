package test_util

import (
	. "github.com/onsi/ginkgo/config"

	"sync"
)

var (
	lastPortUsed int
	portLock     sync.Mutex
	once         sync.Once
)

func NextAvailPort() uint16 {
	portLock.Lock()
	defer portLock.Unlock()

	if lastPortUsed == 0 {
		once.Do(func() {
			const portRangeStart = 61000
			lastPortUsed = portRangeStart + GinkgoConfig.ParallelNode
		})
	}

	lastPortUsed += GinkgoConfig.ParallelTotal
	return uint16(lastPortUsed)
}
