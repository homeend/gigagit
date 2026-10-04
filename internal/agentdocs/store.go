// Package agentdocs holds what an agent shows the user beside the code: the
// temporary notes it puts on lines of open working-tree files, and its
// overview documents whose links are anchors into the code. Memory only.
// One Store per process is shared by the TUI and the gg web page it hosts,
// so the two show one set of notes; a standalone gg web keeps its own.
// Callers file everything under a worktree root they normalised with
// domain.CheckoutKey. Reply prose is English protocol text: the TUI and the
// web answer an agent word for word alike.
// Spec: docs/superpowers/specs/2026-10-01-agent-docs-web-design.md.
package agentdocs

import "sync"

// Store is the shared state. All of it sits behind one mutex and leaves only
// as copies; a change signals every subscriber after the lock is released.
type Store struct {
	mu        sync.Mutex
	fileSeq   int64
	noteSeq   int64
	files     map[fileKey]*fileNotes
	overviews map[string]*ovEntry // by id
	tours     map[string]string   // FileTour key -> overview id (may name a closed one)
	tourMu    sync.Mutex          // one FileTour at a time: look up, then add or replace
	b         broadcaster
}

type fileKey struct{ root, path string }

// New is an empty store (a standalone gg web, every test).
func New() *Store {
	return &Store{files: map[fileKey]*fileNotes{}, overviews: map[string]*ovEntry{}, tours: map[string]string{}}
}

var (
	sharedOnce sync.Once
	shared     *Store
)

// Shared is the process's store: the TUI and the page it hosts both use it.
// Read only at the composition points (tui.New, web.NewHost).
func Shared() *Store {
	sharedOnce.Do(func() { shared = New() })
	return shared
}

// NextFileSeq numbers open files ("f<n>") for every list drawing from this
// store, so an id it hands out never collides with another.
func (s *Store) NextFileSeq() int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.fileSeq++
	return s.fileSeq
}

// Subscribe returns a channel that receives after every change since the
// last receive — coalesced, so a subscriber re-reads everything it shows —
// and a cancel that drops the subscription.
func (s *Store) Subscribe() (<-chan struct{}, func()) { return s.b.subscribe() }

// broadcaster is agentsession.Broadcaster's shape (frontends may not import
// that PTY package): a buffered-1 channel per subscriber, never blocking.
type broadcaster struct {
	mu   sync.Mutex
	subs map[chan struct{}]struct{}
}

func (b *broadcaster) subscribe() (<-chan struct{}, func()) {
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

func (b *broadcaster) signal() {
	b.mu.Lock()
	defer b.mu.Unlock()
	for ch := range b.subs {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
}
