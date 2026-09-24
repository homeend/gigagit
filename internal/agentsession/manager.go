package agentsession

import (
	"context"
	"errors"
	"sort"
	"strconv"
	"sync"
)

var (
	// ErrNoSession: the id names no session of this Manager.
	ErrNoSession = errors.New("agentsession: no such session")
	// ErrRunning: the operation needs an exited session.
	ErrRunning = errors.New("agentsession: session is still running")
)

// Manager owns every session of the process.
type Manager struct {
	mu       sync.Mutex
	next     int
	sessions map[ID]*Session
	changed  chan struct{}
}

// NewManager returns an empty Manager.
func NewManager() *Manager {
	return &Manager{sessions: map[ID]*Session{}, changed: make(chan struct{}, 1)}
}

func (m *Manager) signal() {
	select {
	case m.changed <- struct{}{}:
	default:
	}
}

// Changed receives a value when the session LIST changed (start, exit,
// remove); bursts coalesce.
func (m *Manager) Changed() <-chan struct{} { return m.changed }

// Start runs spec and registers the session under a fresh id.
func (m *Manager) Start(spec StartSpec) (*Session, error) {
	m.mu.Lock()
	m.next++
	id := ID("s" + strconv.Itoa(m.next))
	m.mu.Unlock()
	s, err := start(id, spec)
	if err != nil {
		return nil, err
	}
	m.mu.Lock()
	m.sessions[id] = s
	m.mu.Unlock()
	m.signal()
	go func() { <-s.Done(); m.signal() }()
	return s, nil
}

// Get returns the session with id.
func (m *Manager) Get(id ID) (*Session, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.sessions[id]
	return s, ok
}

// List snapshots every session, oldest first.
func (m *Manager) List() []Info {
	m.mu.Lock()
	out := make([]Info, 0, len(m.sessions))
	for _, s := range m.sessions {
		out = append(out, s.Info())
	}
	m.mu.Unlock()
	sort.Slice(out, func(i, j int) bool {
		if !out[i].Started.Equal(out[j].Started) {
			return out[i].Started.Before(out[j].Started)
		}
		return out[i].ID < out[j].ID
	})
	return out
}

// LiveCount is the number of running sessions.
func (m *Manager) LiveCount() int {
	n := 0
	for _, i := range m.List() {
		if i.State == Running {
			n++
		}
	}
	return n
}

// Kill ends the session's process tree. Killing an exited session is a no-op.
func (m *Manager) Kill(id ID) error {
	s, ok := m.Get(id)
	if !ok {
		return ErrNoSession
	}
	s.kill()
	return nil
}

// Remove forgets an exited session.
func (m *Manager) Remove(id ID) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.sessions[id]
	if !ok {
		return ErrNoSession
	}
	if s.Info().State == Running {
		return ErrRunning
	}
	delete(m.sessions, id)
	m.signal()
	return nil
}

// KillAll kills every session and returns once all have exited or ctx is done.
func (m *Manager) KillAll(ctx context.Context) {
	m.mu.Lock()
	all := make([]*Session, 0, len(m.sessions))
	for _, s := range m.sessions {
		all = append(all, s)
	}
	m.mu.Unlock()
	for _, s := range all {
		s.kill()
	}
	for _, s := range all {
		select {
		case <-s.Done():
		case <-ctx.Done():
			return
		}
	}
}
