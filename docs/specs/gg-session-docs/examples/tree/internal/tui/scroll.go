package tui

import "sync"

// scroller coalesces wheel ticks.
type scroller struct{ mu sync.Mutex; delta int }

func (s *scroller) push(d int) {
	s.mu.Lock()
	s.delta += d
	s.mu.Unlock()
}

func (s *scroller) flush() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.delta != 0 {
		apply(s.delta)
		s.delta = 0
	}
}

func apply(int) {}
