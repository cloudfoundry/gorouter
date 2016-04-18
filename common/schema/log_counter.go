package schema

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

func (lc *LogCounter) Log(log lager.LogFormat) {
	lc.Lock()
	lc.counts[strconv.Itoa(int(log.LogLevel))] += 1
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
