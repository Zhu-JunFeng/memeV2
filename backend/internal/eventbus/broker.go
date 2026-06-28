package eventbus

import "sync"

const (
	TopicCandidates = "candidates"
	TopicSignals    = "signals"
	TopicOrders     = "orders"
	TopicPositions  = "positions"

	EventSnapshot  = "snapshot"
	EventUpsert    = "upsert"
	EventDelete    = "delete"
	EventHeartbeat = "heartbeat"
)

type Event struct {
	Type string
	ID   string
	Data any
}

type Broker struct {
	mu          sync.RWMutex
	subscribers map[string]map[chan Event]struct{}
}

func NewBroker() *Broker {
	return &Broker{subscribers: map[string]map[chan Event]struct{}{}}
}

func (b *Broker) Subscribe(topic string) (<-chan Event, func()) {
	ch := make(chan Event, 32)
	b.mu.Lock()
	if b.subscribers[topic] == nil {
		b.subscribers[topic] = map[chan Event]struct{}{}
	}
	b.subscribers[topic][ch] = struct{}{}
	b.mu.Unlock()
	cancel := func() {
		b.mu.Lock()
		if subs := b.subscribers[topic]; subs != nil {
			delete(subs, ch)
			if len(subs) == 0 {
				delete(b.subscribers, topic)
			}
		}
		b.mu.Unlock()
		close(ch)
	}
	return ch, cancel
}

func (b *Broker) Publish(topic string, event Event) {
	if b == nil {
		return
	}
	b.mu.RLock()
	subs := make([]chan Event, 0, len(b.subscribers[topic]))
	for ch := range b.subscribers[topic] {
		subs = append(subs, ch)
	}
	b.mu.RUnlock()
	for _, ch := range subs {
		select {
		case ch <- event:
		default:
		}
	}
}
