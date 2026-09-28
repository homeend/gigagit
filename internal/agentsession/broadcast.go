package agentsession

import "sync"

// Broadcaster wakes every subscriber on Signal. Each subscriber owns a
// one-slot channel, so a burst coalesces per subscriber instead of being
// stolen by whichever reader is fastest — the shape a TUI and a web page in
// ONE process need (a single shared channel gave the wakeup to one of them
// and left the other asleep). Exported so domain.TaskManager can hold one.
// The zero value is ready to use.
type Broadcaster struct {
	mu   sync.Mutex
	subs map[chan struct{}]struct{}
}

// Subscribe returns a channel that receives after every Signal since the
// last receive, and a cancel that drops the subscription (idempotent). A
// subscription is meant to live on the owner's state for as long as the
// owner does — never re-created per wakeup, so nothing is lost between two
// arms.
func (b *Broadcaster) Subscribe() (<-chan struct{}, func()) {
	ch := make(chan struct{}, 1)
	b.mu.Lock()
	if b.subs == nil {
		b.subs = map[chan struct{}]struct{}{}
	}
	b.subs[ch] = struct{}{}
	b.mu.Unlock()
	return ch, func() {
		b.mu.Lock()
		delete(b.subs, ch)
		b.mu.Unlock()
	}
}

// Signal marks every subscriber dirty without ever blocking the caller.
func (b *Broadcaster) Signal() {
	b.mu.Lock()
	defer b.mu.Unlock()
	for ch := range b.subs {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
}

// count reports the live subscriptions (a test hook).
func (b *Broadcaster) count() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.subs)
}
