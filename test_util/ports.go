package test_util

import (
	"sync"

	. "github.com/onsi/ginkgo/v2"
)

var (
	lastPortUsed uint16
	portLock     sync.Mutex
	once         sync.Once
)

func NextAvailPort() uint16 {
	portLock.Lock()
	defer portLock.Unlock()

	if lastPortUsed == 0 {
		once.Do(func() {
			const portRangeStart = 25000
			// #nosec G115 - if we have negative or > 65k parallel ginkgo threads there's something worse happening
			lastPortUsed = portRangeStart + uint16(GinkgoParallelProcess())
		})
	}

	suiteCfg, _ := GinkgoConfiguration()
	// #nosec G115 - if we have negative or > 65k parallel ginkgo threads there's something worse happening
	lastPortUsed += uint16(suiteCfg.ParallelTotal)
	return lastPortUsed
}
