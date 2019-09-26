package health

import (
	"sync"
)

type Status uint64

const (
	Initializing Status = iota
	Healthy
	Degraded
)

type onDegradeCallback func()

type Health struct {
	mu     sync.RWMutex // to lock health r/w
	health Status

	OnDegrade onDegradeCallback
}

func (h *Health) Health() Status {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.health
}

func (h *Health) SetHealth(s Status) {
	h.mu.Lock()

	if h.health == Degraded {
		h.mu.Unlock()
		return
	}

	h.health = s
	h.mu.Unlock()

	if h.OnDegrade != nil && s == Degraded {
		h.OnDegrade()
	}
}

func (h *Health) String() string {
	switch h.Health() {
	case Initializing:
		return "Initializing"
	case Healthy:
		return "Healthy"
	case Degraded:
		return "Degraded"
	default:
		panic("health: unknown status")
	}
}
