package health

import "sync/atomic"

type Status uint64

const (
	Initializing Status = iota
	Healthy
	Degraded
)

type Health struct {
	health uint64
}

func (h *Health) Health() Status {
	return Status(atomic.LoadUint64(&h.health))
}

func (h *Health) SetHealth(s Status) {
	atomic.StoreUint64(&h.health, uint64(s))
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
