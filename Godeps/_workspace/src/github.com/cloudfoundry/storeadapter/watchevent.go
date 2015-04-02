package storeadapter

type WatchEvent struct {
	Type     EventType
	Node     *StoreNode
	PrevNode *StoreNode
}

type EventType int

const (
	InvalidEvent = EventType(iota)
	CreateEvent
	DeleteEvent
	ExpireEvent
	UpdateEvent
)
