package agentsession

// tapDepth is how many unread chunks a tap buffers before dropping.
const tapDepth = 64

// Tap returns a channel receiving copies of the child's raw output (for a
// future web attach) and a cancel func. A slow reader loses chunks — the
// pump never waits for it.
func (s *Session) Tap() (<-chan []byte, func()) {
	ch := make(chan []byte, tapDepth)
	s.mu.Lock()
	if s.taps == nil {
		s.taps = map[chan []byte]struct{}{}
	}
	s.taps[ch] = struct{}{}
	s.mu.Unlock()
	return ch, func() {
		s.mu.Lock()
		delete(s.taps, ch)
		s.mu.Unlock()
	}
}

func (s *Session) feedTaps(p []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for ch := range s.taps {
		select {
		case ch <- append([]byte(nil), p...):
		default:
		}
	}
}
