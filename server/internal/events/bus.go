// Package events is a tiny in-process pub/sub bus. Phase 1 only publishes
// (fetch/save/commit/merge/pull/workspace); Phase 2's collab hub and a future
// websocket push subscribe to it.
package events

import "sync"

type Event struct {
	Repo    string `json:"repo"`
	Branch  string `json:"branch,omitempty"`
	Kind    string `json:"kind"` // fetch | save | commit | merge | pull | workspace | branch
	Payload any    `json:"payload,omitempty"`
}

type Bus struct {
	mu   sync.Mutex
	subs map[int]chan<- Event
	next int
}

func New() *Bus {
	return &Bus{subs: map[int]chan<- Event{}}
}

// Subscribe registers ch and returns an unsubscribe func. Delivery is
// non-blocking: slow subscribers drop events (they are cache-invalidation
// hints, not a source of truth).
func (b *Bus) Subscribe(ch chan<- Event) (unsubscribe func()) {
	b.mu.Lock()
	defer b.mu.Unlock()
	id := b.next
	b.next++
	b.subs[id] = ch
	return func() {
		b.mu.Lock()
		defer b.mu.Unlock()
		delete(b.subs, id)
	}
}

func (b *Bus) Publish(e Event) {
	if b == nil {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, ch := range b.subs {
		select {
		case ch <- e:
		default: // drop on slow subscriber
		}
	}
}
