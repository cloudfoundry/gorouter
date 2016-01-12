package common

import (
	"encoding/json"
	"strconv"
	"sync"

	"github.com/pivotal-golang/lager"
)

type LogCounter struct {
	sync.Mutex
	counts map[string]int
}

func NewLogCounter() *LogCounter {
	lc := &LogCounter{
		counts: make(map[string]int),
	}
	return lc
}

func (lc *LogCounter) Log(logLevel lager.LogLevel, payload []byte) {
	lc.Lock()
	lc.counts[strconv.Itoa(int(logLevel))] += 1
	lc.Unlock()
}

func (lc *LogCounter) GetCount(key string) int {
	lc.Lock()
	defer lc.Unlock()
	return lc.counts[key]
}

func (lc *LogCounter) MarshalJSON() ([]byte, error) {
	lc.Lock()
	defer lc.Unlock()
	return json.Marshal(lc.counts)
}
