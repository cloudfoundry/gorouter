package threading

import (
	"sync"
)

type SharedBoolean struct {
	b     bool
	mutex sync.RWMutex
}

func (b *SharedBoolean) Set(boolean bool) {
	b.mutex.Lock()
	defer b.mutex.Unlock()
	b.b = boolean
}

func (b *SharedBoolean) Get() bool {
	b.mutex.RLock()
	defer b.mutex.RUnlock()
	return b.b
}
