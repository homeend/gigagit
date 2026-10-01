package web

import (
	"errors"
	"io/fs"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/homeend/gigagit/internal/steer"
)

// The open-files list (open files on the web, plan 5b): per worktree, the
// files a viewer opened, most recently shown first — ONE list shared by every
// tab of this gg web, which each tab reads and edits over /api/open-files and
// hears about on /api/events. A tab shows at most one file (its viewer); a
// file is "shown" while any tab with a live event stream shows it. The pure
// core below knows nothing of HTTP; openfiles_http.go and openfiles_watch.go
// drive it. Lives as long as the process (the TUI's openFilesReg).

// maxOpenFiles is how many files one worktree keeps open (the TUI's cap).
const maxOpenFiles = 20

// ofKey is an entry's identity: a version of a path. Src is "worktree",
// "commit" (Rev = the sha) or "shelf" (Rev = the entry id).
type ofKey struct{ Src, Rev, Path string }

// diskStat is what a poll compares: a working-tree file's size and mtime, or
// its absence. known is false for a stat never taken (the TUI's diskStat).
type diskStat struct {
	size    int64
	mod     time.Time
	missing bool
	known   bool
}

// statDisk stats abs. Any error other than "does not exist" reads as unknown
// — never as a deletion.
func statDisk(abs string) diskStat {
	fi, err := os.Stat(abs)
	switch {
	case err == nil:
		return diskStat{size: fi.Size(), mod: fi.ModTime(), known: true}
	case errors.Is(err, fs.ErrNotExist):
		return diskStat{missing: true, known: true}
	}
	return diskStat{}
}

func (a diskStat) same(b diskStat) bool {
	return a.missing == b.missing && a.size == b.size && a.mod.Equal(b.mod)
}

type ofEntry struct {
	id      string
	key     ofKey
	line    int
	disk    diskStat
	checked time.Time
	// pinned: an agent's notes are on it, or it is an overview
	// (agentdocs_follow.go) — never evicted, and a plain close (esc)
	// backgrounds it instead.
	pinned bool
	title  string // an overview's
}

type openFiles struct {
	mu sync.Mutex
	// next numbers entries: the agent-docs store's NextFileSeq, so an id a
	// TUI hosting this page hands out never names a different file here.
	next     func() int64
	byWT     map[string][]*ofEntry
	tabShows map[string]string // tab → the id its viewer shows
	streams  map[string]int    // tab → its live /api/events streams
}

func newOpenFiles(next func() int64) *openFiles {
	return &openFiles{next: next, byWT: map[string][]*ofEntry{}, tabShows: map[string]string{}, streams: map[string]int{}}
}

// shownLocked reports whether any tab shows id.
func (r *openFiles) shownLocked(id string) bool {
	for _, v := range r.tabShows {
		if v == id {
			return true
		}
	}
	return false
}

func (r *openFiles) findLocked(wt, id string) (int, *ofEntry) {
	for i, e := range r.byWT[wt] {
		if e.id == id {
			return i, e
		}
	}
	return -1, nil
}

func (r *openFiles) wireLocked(e *ofEntry) steer.OpenFile {
	st := "background"
	if r.shownLocked(e.id) {
		st = "shown"
	}
	return steer.OpenFile{ID: e.id, Path: e.key.Path, Source: e.key.Src, Rev: e.key.Rev, Line: e.line, State: st, Title: e.title}
}

// frontLocked moves e first in wt's list (adding it when new) and, over the
// cap, drops the least recently shown entry no tab shows and no agent's
// notes pin; with none to drop the list grows past the cap.
func (r *openFiles) frontLocked(wt string, e *ofEntry) (evicted string) {
	l := []*ofEntry{e}
	for _, o := range r.byWT[wt] {
		if o != e {
			l = append(l, o)
		}
	}
	if len(l) > maxOpenFiles {
		for i := len(l) - 1; i > 0; i-- {
			if !r.shownLocked(l[i].id) && !l[i].pinned {
				evicted = l[i].key.Path
				l = append(l[:i], l[i+1:]...)
				break
			}
		}
	}
	r.byWT[wt] = l
	return evicted
}

// open reuses the entry for k or creates one, moves it first and, for a
// non-empty tab, makes it what that tab shows. line > 0 is recorded.
func (r *openFiles) open(wt string, k ofKey, tab string, line int) (steer.OpenFile, string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var e *ofEntry
	for _, o := range r.byWT[wt] {
		if o.key == k {
			e = o
			break
		}
	}
	if e == nil {
		e = &ofEntry{id: "f" + strconv.FormatInt(r.next(), 10), key: k}
	}
	if line > 0 {
		e.line = line
	}
	if tab != "" {
		r.tabShows[tab] = e.id
	}
	ev := r.frontLocked(wt, e)
	return r.wireLocked(e), ev
}

// ensureOpen lists k when it is not listed yet — first, as every open is,
// with pinned set before any eviction is weighed — and reports the entry,
// the path an addition pushed out ("" = none) and whether it was added. A
// listed entry is left where it is (pinned is only ever added here).
func (r *openFiles) ensureOpen(wt string, k ofKey, pinned bool) (steer.OpenFile, string, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, e := range r.byWT[wt] {
		if e.key == k {
			e.pinned = e.pinned || pinned
			return r.wireLocked(e), "", false
		}
	}
	e := &ofEntry{id: "f" + strconv.FormatInt(r.next(), 10), key: k, pinned: pinned}
	ev := r.frontLocked(wt, e)
	return r.wireLocked(e), ev, true
}

// ensureOpenID is ensureOpen for an entry whose id is given (an overview's,
// from the store), pinned, titled title — kept up to date when listed.
func (r *openFiles) ensureOpenID(wt string, k ofKey, id, title string) (steer.OpenFile, string, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, e := r.findLocked(wt, id); e != nil {
		e.pinned, e.title = true, title
		return r.wireLocked(e), "", false
	}
	e := &ofEntry{id: id, key: k, pinned: true, title: title}
	ev := r.frontLocked(wt, e)
	return r.wireLocked(e), ev, true
}

// removeID drops wt's entry id, in every tab; false when there was none.
func (r *openFiles) removeID(wt, id string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	i, e := r.findLocked(wt, id)
	if e == nil {
		return false
	}
	for t, v := range r.tabShows {
		if v == id {
			delete(r.tabShows, t)
		}
	}
	r.byWT[wt] = append(r.byWT[wt][:i:i], r.byWT[wt][i+1:]...)
	return true
}

// ids are wt's entry ids from source src.
func (r *openFiles) ids(wt, src string) []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []string
	for _, e := range r.byWT[wt] {
		if e.key.Src == src {
			out = append(out, e.id)
		}
	}
	return out
}

// shown reports whether a tab shows id.
func (r *openFiles) shown(id string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.shownLocked(id)
}

// setPinned pins wt's working-tree entries whose path is in paths and
// unpins every other one (an overview stays pinned); true when anything
// changed.
func (r *openFiles) setPinned(wt string, paths map[string]bool) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	changed := false
	for _, e := range r.byWT[wt] {
		if want := e.key.Src == "overview" || e.key.Src == "worktree" && paths[e.key.Path]; e.pinned != want {
			e.pinned, changed = want, true
		}
	}
	return changed
}

// pinnedEntry is id's key and pinned flag; found is false for no such id.
func (r *openFiles) pinnedEntry(wt, id string) (k ofKey, pinned, found bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, e := r.findLocked(wt, id); e != nil {
		return e.key, e.pinned, true
	}
	return ofKey{}, false, false
}

// focus makes id what tab shows and moves it first.
func (r *openFiles) focus(wt, id, tab string) (steer.OpenFile, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	_, e := r.findLocked(wt, id)
	if e == nil {
		return steer.OpenFile{}, false
	}
	if tab != "" {
		r.tabShows[tab] = id
	}
	r.frontLocked(wt, e) // e is already listed: nothing can be evicted
	return r.wireLocked(e), true
}

// background: tab no longer shows id; line > 0 is recorded.
func (r *openFiles) background(wt, id, tab string, line int) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	_, e := r.findLocked(wt, id)
	if e == nil {
		return false
	}
	if line > 0 {
		e.line = line
	}
	if r.tabShows[tab] == id {
		delete(r.tabShows, tab)
	}
	return true
}

// close: tab lets go of id; the entry is removed unless another tab still
// shows it — or always, when everywhere (the switcher's x).
func (r *openFiles) close(wt, id, tab string, everywhere bool) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	i, e := r.findLocked(wt, id)
	if e == nil {
		return false
	}
	if r.tabShows[tab] == id {
		delete(r.tabShows, tab)
	}
	if !everywhere && r.shownLocked(id) {
		return true
	}
	for t, v := range r.tabShows {
		if v == id {
			delete(r.tabShows, t)
		}
	}
	r.byWT[wt] = append(r.byWT[wt][:i:i], r.byWT[wt][i+1:]...)
	return true
}

// cursor records the last line a tab reported.
func (r *openFiles) cursor(wt, id string, line int) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	_, e := r.findLocked(wt, id)
	if e == nil {
		return false
	}
	if line > 0 {
		e.line = line
	}
	return true
}

// setShown records what tab shows ("" = nothing) without reordering — a
// tab's re-report after its event stream reconnected.
func (r *openFiles) setShown(wt, tab, id string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if id == "" {
		delete(r.tabShows, tab)
		return true
	}
	if _, e := r.findLocked(wt, id); e == nil {
		return false
	}
	r.tabShows[tab] = id
	return true
}

func (r *openFiles) streamOpened(tab string) {
	r.mu.Lock()
	r.streams[tab]++
	r.mu.Unlock()
}

// streamClosed ends one of tab's streams; the last one ending drops what the
// tab showed (true when it showed something).
func (r *openFiles) streamClosed(tab string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.streams[tab] == 0 {
		return false
	}
	r.streams[tab]--
	if r.streams[tab] > 0 {
		return false
	}
	delete(r.streams, tab)
	_, had := r.tabShows[tab]
	delete(r.tabShows, tab)
	return had
}

// lookup is the entry for version k, if one is open.
func (r *openFiles) lookup(wt string, k ofKey) (steer.OpenFile, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, e := range r.byWT[wt] {
		if e.key == k {
			return r.wireLocked(e), true
		}
	}
	return steer.OpenFile{}, false
}

// resolve finds an open file by id, else by path (the first — most recently
// shown — version of it). An id that matches nothing is tried as a path, as
// the TUI's findOpenFile does.
func (r *openFiles) resolve(wt, id, path string) (steer.OpenFile, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if id != "" {
		if _, e := r.findLocked(wt, id); e != nil {
			return r.wireLocked(e), true
		}
		path = id
	}
	for _, e := range r.byWT[wt] {
		if e.key.Path == path {
			return r.wireLocked(e), true
		}
	}
	return steer.OpenFile{}, false
}

// liveTabs is how many tabs have a live event stream.
func (r *openFiles) liveTabs() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.streams)
}

// list is wt's open files, most recently shown first, in the wire form.
func (r *openFiles) list(wt string) []steer.OpenFile {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := []steer.OpenFile{}
	for _, e := range r.byWT[wt] {
		out = append(out, r.wireLocked(e))
	}
	return out
}

func (r *openFiles) entryKey(wt, id string) (ofKey, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, e := r.findLocked(wt, id); e != nil {
		return e.key, true
	}
	return ofKey{}, false
}

// setBaseline records d as id's disk state (stat-before-read on open).
func (r *openFiles) setBaseline(wt, id string, d diskStat) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, e := r.findLocked(wt, id); e != nil && d.known {
		e.disk = d
	}
}

type ofDue struct{ ID, Path string }

// due is wt's working-tree entries to stat now: a shown one every round, a
// background one every bgEvery — every one when all (a filewatch wake).
func (r *openFiles) due(wt string, now time.Time, all bool, bgEvery time.Duration) []ofDue {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []ofDue
	for _, e := range r.byWT[wt] {
		if e.key.Src != "worktree" {
			continue
		}
		if !all && !r.shownLocked(e.id) && !e.checked.IsZero() && now.Sub(e.checked) < bgEvery {
			continue
		}
		e.checked = now
		out = append(out, ofDue{ID: e.id, Path: e.key.Path})
	}
	return out
}

// applyStat compares d with id's baseline: the first known stat only
// records; a different one is recorded and reported as a change.
func (r *openFiles) applyStat(wt, id string, d diskStat) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	_, e := r.findLocked(wt, id)
	if e == nil || !d.known {
		return false
	}
	if !e.disk.known {
		e.disk = d
		return false
	}
	if e.disk.same(d) {
		return false
	}
	e.disk = d
	return true
}
