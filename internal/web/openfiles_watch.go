package web

import (
	"time"

	"github.com/homeend/gigagit/internal/filewatch"
	"github.com/homeend/gigagit/internal/gitwatch"
)

// Following the disk (plan 5b): the server stat-polls the current worktree's
// open working-tree files — a shown one every tick, a background one every
// ofBackgroundEvery — and tells the tabs "file_changed". internal/filewatch
// only WAKES the poll where the filesystem delivers events (never on a 9p
// /mnt); the stat decides (the TUI's rule).

// Test seams, read once when the watch starts.
var (
	ofTick            = time.Second
	ofBackgroundEvery = 5 * time.Second
)

const ofWatchDebounce = 150 * time.Millisecond // the TUI's docWatchDebounce

// pollOpenFiles runs one stat round and broadcasts each change; it returns
// the changed ids. A round while an op runs is skipped whole — no stat, no
// baseline moved — so the op's edits are seen by the first round after it
// (ruling L6).
func (s *Server) pollOpenFiles(now time.Time, all bool, bgEvery time.Duration) []string {
	if s.opInFlight() {
		return nil
	}
	wt := s.service().Root()
	var changed []string
	for _, d := range s.ofs.due(wt, now, all, bgEvery) {
		if s.ofs.applyStat(wt, d.ID, statDisk(s.ofAbs(wt, d.Path))) {
			changed = append(changed, d.ID)
			s.broadcastFileChanged(d.ID)
		}
	}
	return changed
}

// worktreeAbs is wt's open working-tree files on disk, for the wake watcher.
func (s *Server) worktreeAbs(wt string) []string {
	var out []string
	for _, f := range s.ofs.list(wt) {
		if f.Source == "worktree" {
			out = append(out, s.ofAbs(wt, f.Path))
		}
	}
	return out
}

// startOpenFilesWatch runs the poll loop until Close.
func (s *Server) startOpenFilesWatch() {
	s.ofStop = make(chan struct{}) // Serve starts it once, before any Close
	stop, tick, bg, now := s.ofStop, ofTick, ofBackgroundEvery, liveNow
	go func() {
		t := time.NewTicker(tick)
		defer t.Stop()
		var w *filewatch.Watcher
		var wake <-chan string
		watchRoot, broken := "", false
		defer func() {
			if w != nil {
				_ = w.Close()
			}
		}()
		for {
			all := false
			select {
			case <-stop:
				return
			case <-s.closing:
				return
			case <-t.C:
			case _, ok := <-wake:
				if !ok {
					wake, w = nil, nil
					continue
				}
				all = true
			}
			wt := s.service().Root()
			if wt != watchRoot { // a re-root: the old tree's watcher goes
				if w != nil {
					_ = w.Close()
				}
				w, wake, watchRoot, broken = nil, nil, wt, false
			}
			if w == nil && !broken && gitwatch.Supported(wt) {
				if nw, err := filewatch.New(ofWatchDebounce); err == nil {
					w, wake = nw, nw.Events()
				} else {
					broken = true
				}
			}
			if w != nil {
				w.Set(s.worktreeAbs(wt))
			}
			s.pollOpenFiles(now(), all, bg)
		}
	}()
}

// stopOpenFilesWatch ends the loop (idempotent; nil-safe before a start).
func (s *Server) stopOpenFilesWatch() {
	if s.ofStop != nil {
		s.ofStopOnce.Do(func() { close(s.ofStop) })
	}
}
