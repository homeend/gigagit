// Package filewatch watches a set of files and reports, debounced, which of
// them changed. It watches each file's directory (so a file deleted and
// written again — an editor's save — stays watched) and keeps only events
// for the files in the set: a busy directory in a large repo must not wake
// the consumer.
//
// fsnotify is a wake-up, never the source of truth: it misses edits on some
// mounts (WSL drvfs/9p edits made from Windows), so the consumer still polls.
// Every failure here is swallowed (fail-open) for the same reason.
package filewatch

import (
	"path/filepath"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

// Watcher watches a set of files. Set and Close are safe from any goroutine.
type Watcher struct {
	fsw      *fsnotify.Watcher
	debounce time.Duration
	out      chan string

	mu     sync.Mutex
	files  map[string]bool // the watched files, filepath.Clean form
	dirs   map[string]int  // a watched dir → how many watched files are in it
	timers map[string]*time.Timer
	closed bool
	done   chan struct{}
}

// New starts a watcher over no files; Set gives it some. debounce coalesces
// a burst of events on one file into one report.
func New(debounce time.Duration) (*Watcher, error) {
	fsw, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}
	w := &Watcher{
		fsw:      fsw,
		debounce: debounce,
		out:      make(chan string, 16),
		files:    map[string]bool{},
		dirs:     map[string]int{},
		timers:   map[string]*time.Timer{},
		done:     make(chan struct{}),
	}
	go w.loop()
	return w, nil
}

// Events reports a changed watched file (filepath.Clean form). It is closed
// by Close.
func (w *Watcher) Events() <-chan string { return w.out }

// Set makes paths (absolute) the watched set: directories no longer needed
// are dropped, new ones added. A directory that cannot be watched is skipped.
func (w *Watcher) Set(paths []string) {
	want := map[string]bool{}
	for _, p := range paths {
		want[filepath.Clean(p)] = true
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return
	}
	for f := range w.files {
		if !want[f] {
			delete(w.files, f)
			d := filepath.Dir(f)
			if w.dirs[d]--; w.dirs[d] <= 0 {
				delete(w.dirs, d)
				_ = w.fsw.Remove(d)
			}
		}
	}
	for f := range want {
		if w.files[f] {
			continue
		}
		w.files[f] = true
		d := filepath.Dir(f)
		if w.dirs[d]++; w.dirs[d] == 1 {
			_ = w.fsw.Add(d)
		}
	}
}

// loop keeps the events for watched files and debounces them.
func (w *Watcher) loop() {
	for {
		select {
		case <-w.done:
			return
		case ev, ok := <-w.fsw.Events:
			if !ok {
				return
			}
			w.schedule(filepath.Clean(ev.Name))
		case _, ok := <-w.fsw.Errors:
			if !ok {
				return
			}
		}
	}
}

// schedule (re)arms path's debounce timer when path is watched.
func (w *Watcher) schedule(path string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed || !w.files[path] {
		return
	}
	if t := w.timers[path]; t != nil {
		t.Stop()
	}
	w.timers[path] = time.AfterFunc(w.debounce, func() {
		w.mu.Lock()
		defer w.mu.Unlock()
		if !w.closed {
			select {
			case w.out <- path:
			default: // full: the consumer polls anyway
			}
		}
	})
}

// Close stops watching and closes Events. Safe to call more than once.
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
	close(w.out) // every timer checks closed under mu before sending
	w.mu.Unlock()
	close(w.done)
	return w.fsw.Close()
}
