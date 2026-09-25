package domain

import (
	"bytes"
	"crypto/sha256"
	"os"
	"sync"
	"time"

	"github.com/homeend/gigagit/internal/filewatch"
)

// taskResultPoll is the watcher's polling period: fsnotify is only a
// wake-up (it misses writes on some mounts), the poll is the truth.
var taskResultPoll = 500 * time.Millisecond

// resultStableDelay is how long a new content must stay the same before it
// counts: a poll can land mid-write.
const resultStableDelay = 100 * time.Millisecond

// resultWatcher reports when one file holds new, non-empty content.
type resultWatcher struct {
	path  string
	wakeC chan struct{}
	stop  chan struct{}
	once  sync.Once
	fw    *filewatch.Watcher
	last  [32]byte
	seen  bool
}

// newResultWatcher watches path; "" gives a watcher that never wakes.
func newResultWatcher(path string) *resultWatcher {
	w := &resultWatcher{path: path, stop: make(chan struct{})}
	if path == "" {
		return w
	}
	w.wakeC = make(chan struct{}, 1)
	poll := taskResultPoll // read here: the goroutine must not race a test restoring it
	var fsEvents <-chan string
	if fw, err := filewatch.New(50 * time.Millisecond); err == nil {
		fw.Set([]string{path})
		w.fw = fw
		fsEvents = fw.Events()
	}
	go func() {
		tick := time.NewTicker(poll)
		defer tick.Stop()
		for {
			select {
			case <-w.stop:
				return
			case <-tick.C:
			case _, ok := <-fsEvents:
				if !ok {
					fsEvents = nil
					continue
				}
			}
			select {
			case w.wakeC <- struct{}{}:
			default:
			}
		}
	}()
	return w
}

// wake fires when the file may have changed (nil when there is no file).
func (w *resultWatcher) wake() <-chan struct{} { return w.wakeC }

// changed reports new non-empty content, stable across resultStableDelay,
// different from the last reported content.
func (w *resultWatcher) changed() bool {
	if w.path == "" {
		return false
	}
	b, err := os.ReadFile(w.path)
	if err != nil || len(bytes.TrimSpace(b)) == 0 {
		return false
	}
	sum := sha256.Sum256(b)
	if w.seen && sum == w.last {
		return false
	}
	time.Sleep(resultStableDelay)
	if b2, err := os.ReadFile(w.path); err != nil || !bytes.Equal(b, b2) {
		return false // still being written; the next wake re-reads
	}
	w.last, w.seen = sum, true
	return true
}

func (w *resultWatcher) close() {
	w.once.Do(func() {
		close(w.stop)
		if w.fw != nil {
			w.fw.Close()
		}
	})
}
