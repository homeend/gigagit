package gitwatch

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

// Watcher watches a set of planned groups and emits a debounced Source whenever
// the .git files backing that source change. It is safe to Close once; the
// events channel is closed by Close() under the mutex (not by loop()), so no
// timer callback can send after the close.
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
		w.addGroup(g)
	}
	go w.loop()
	return w, nil
}

// Events delivers debounced source changes. It is closed when the Watcher stops.
func (w *Watcher) Events() <-chan Source { return w.out }

// loop translates fsnotify events into debounced Source emissions.
// It does NOT close w.out — Close() does that under the mutex.
func (w *Watcher) loop() {
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

	// A new directory under a recursive group must start being watched, and the
	// create itself is a change for that group's source.
	if ev.Op&(fsnotify.Create) != 0 {
		if fi, err := os.Stat(ev.Name); err == nil && fi.IsDir() {
			for _, g := range w.groups {
				if g.Recursive && (ev.Name == g.Dir || isUnder(ev.Name, g.Dir)) {
					_ = w.fsw.Add(ev.Name)
				}
			}
		}
	}

	// Non-recursive groups: exact dir match.
	for _, g := range w.groups {
		if g.Recursive || g.Dir != dir {
			continue
		}
		for _, s := range g.Match(base) {
			w.schedule(s)
		}
	}
	// Recursive groups: dir is the group root or a descendant.
	for _, s := range w.recursiveMatch(dir, base) {
		w.schedule(s)
	}
}

// addGroup adds the watch(es) for one group: just its dir for non-recursive, or
// the whole existing subtree for recursive groups.
func (w *Watcher) addGroup(g Group) {
	if !g.Recursive {
		if _, err := os.Stat(g.Dir); err == nil {
			_ = w.fsw.Add(g.Dir)
		}
		return
	}
	_ = filepath.WalkDir(g.Dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // skip unreadable entries; missing root is fine
		}
		if d.IsDir() {
			_ = w.fsw.Add(path)
		}
		return nil
	})
}

// recursiveMatch reports the sources of a recursive group whose tree contains dir.
func (w *Watcher) recursiveMatch(dir, base string) []Source {
	var out []Source
	for _, g := range w.groups {
		if !g.Recursive {
			continue
		}
		if dir == g.Dir || isUnder(dir, g.Dir) {
			out = append(out, g.Match(base)...)
		}
	}
	return out
}

// isUnder reports whether path is the dir parent or a descendant of base.
func isUnder(path, base string) bool {
	rel, err := filepath.Rel(base, path)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
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
		// Guard with the mutex: Close() closes w.out under the same mutex, so if
		// we reach here with closed=false the channel is guaranteed still open.
		// Use a non-blocking send so we never hold the lock during a channel op;
		// a dropped duplicate is harmless because debounce coalesces bursts.
		w.mu.Lock()
		if !w.closed {
			select {
			case w.out <- src:
			default: // buffer full — drop; the refresh lane deduplicates
			}
		}
		w.mu.Unlock()
	})
}

// Close stops watching and closes the events channel. Safe to call once.
// Ordering under lock: set closed=true → stop timers → close(w.out).
// Because every timer callback checks w.closed under the same mutex before
// sending, closing w.out here guarantees no callback can send after the close.
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
	close(w.out) // safe: w.closed=true seen by all callbacks under this mutex
	w.mu.Unlock()
	close(w.done)
	return w.fsw.Close()
}
