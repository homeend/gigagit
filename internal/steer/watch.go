package steer

import (
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/fsnotify/fsnotify"
)

// Watcher wakes a consumer when a file it cares about changes in one
// directory. It is the FAST path only — every consumer also polls the dir on
// its own tick, so a Watch that fails (no inotify watches left, an exotic
// filesystem) degrades to a one- or two-second latency rather than to a
// broken feature.
type Watcher struct {
	fsw  *fsnotify.Watcher
	keep func(name string) bool
	out  chan struct{}
	done chan struct{}

	mu     sync.Mutex
	closed bool
}

// Watch creates dir if needed and starts watching it for command files.
func Watch(dir string) (*Watcher, error) { return WatchFilter(dir, isCommandFile) }

// isCommandFile is Watch's filter: presence files are re-touched once a
// second by every session on an inbox; only a command is worth a wake.
func isCommandFile(name string) bool { return strings.HasPrefix(name, cmdPrefix) }

// WatchFilter creates dir if needed and wakes on every create, write, rename
// or remove of a name keep accepts (the base name). The session-docs store
// watches its directory with it.
func WatchFilter(dir string, keep func(name string) bool) (*Watcher, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	fsw, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}
	if err := fsw.Add(dir); err != nil {
		fsw.Close()
		return nil, err
	}
	w := &Watcher{fsw: fsw, keep: keep, out: make(chan struct{}, 1), done: make(chan struct{})}
	go w.loop()
	return w, nil
}

// Events delivers coalesced wakes: one buffered slot, because "there is work in
// the inbox" is not a countable fact — the consumer drains everything it finds.
// The channel is closed by Close.
func (w *Watcher) Events() <-chan struct{} { return w.out }

func (w *Watcher) loop() {
	for {
		select {
		case <-w.done:
			return
		case ev, ok := <-w.fsw.Events:
			if !ok {
				return
			}
			if ev.Op&(fsnotify.Create|fsnotify.Write|fsnotify.Rename|fsnotify.Remove) == 0 {
				continue
			}
			if !w.keep(filepath.Base(ev.Name)) {
				continue
			}
			w.wake()
		case _, ok := <-w.fsw.Errors:
			if !ok {
				return
			}
		}
	}
}

// wake delivers one notification, dropping it when a wake is already pending.
// It takes the mutex so it can never send on a channel Close has closed.
func (w *Watcher) wake() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return
	}
	select {
	case w.out <- struct{}{}:
	default:
	}
}

// Close stops the watcher and closes Events. Calling it twice is a no-op.
func (w *Watcher) Close() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return
	}
	w.closed = true
	close(w.done)
	w.fsw.Close()
	close(w.out)
}
