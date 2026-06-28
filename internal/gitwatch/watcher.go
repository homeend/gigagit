package gitwatch

import (
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

// Watcher watches a set of planned groups and emits a debounced Source whenever
// the .git files backing that source change. It is safe to Close once; the
// events channel is closed on shutdown.
type Watcher struct {
	fsw    *fsnotify.Watcher
	groups []Group
	out    chan Source

	debounce time.Duration
	mu       sync.Mutex
	timers   map[Source]*time.Timer

	done   chan struct{}
	closed bool
}

// New builds a Watcher over groups and starts its event loop. A group whose Dir
// does not exist is skipped (not an error) — a repo may have no worktrees/ or
// logs/ yet. debounce coalesces a burst of events per source into one emission.
func New(groups []Group, debounce time.Duration) (*Watcher, error) {
	fsw, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}
	w := &Watcher{
		fsw:      fsw,
		groups:   groups,
		out:      make(chan Source, 16),
		debounce: debounce,
		timers:   map[Source]*time.Timer{},
		done:     make(chan struct{}),
	}
	for _, g := range groups {
		if _, statErr := os.Stat(g.Dir); statErr != nil {
			continue // dir absent — nothing to watch yet
		}
		_ = fsw.Add(g.Dir) // best-effort; a failed add just means no events from it
	}
	go w.loop()
	return w, nil
}

// Events delivers debounced source changes. It is closed when the Watcher stops.
func (w *Watcher) Events() <-chan Source { return w.out }

// loop translates fsnotify events into debounced Source emissions.
func (w *Watcher) loop() {
	defer close(w.out)
	for {
		select {
		case <-w.done:
			return
		case ev, ok := <-w.fsw.Events:
			if !ok {
				return
			}
			w.handle(ev)
		case _, ok := <-w.fsw.Errors:
			if !ok {
				return
			}
			// fsnotify error: ignore the individual error; the lane still serves
			// manual refresh. A persistent failure degrades to interval/manual.
		}
	}
}

// handle maps one event to its sources and schedules debounced emissions.
func (w *Watcher) handle(ev fsnotify.Event) {
	base := filepath.Base(ev.Name)
	dir := filepath.Dir(ev.Name)
	for _, g := range w.groups {
		if g.Dir != dir {
			continue
		}
		for _, s := range g.Match(base) {
			w.schedule(s)
		}
	}
}

// schedule (re)arms the per-source debounce timer; the source is emitted once
// the quiet window elapses.
func (w *Watcher) schedule(s Source) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return
	}
	if t := w.timers[s]; t != nil {
		t.Stop()
	}
	src := s
	w.timers[s] = time.AfterFunc(w.debounce, func() {
		select {
		case w.out <- src:
		case <-w.done:
		}
	})
}

// Close stops watching and closes the events channel. Safe to call once.
func (w *Watcher) Close() error {
	w.mu.Lock()
	if w.closed {
		w.mu.Unlock()
		return nil
	}
	w.closed = true
	for _, t := range w.timers {
		t.Stop()
	}
	w.mu.Unlock()
	close(w.done)
	return w.fsw.Close()
}
