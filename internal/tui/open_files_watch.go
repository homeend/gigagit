package tui

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/filewatch"
	"github.com/homeend/gigagit/internal/i18n"
)

// diskStat is what a poll compares: a working-tree file's size and mtime,
// or its absence. known is false for a stat never taken.
type diskStat struct {
	size    int64
	mod     time.Time
	missing bool
	known   bool
}

// statDisk stats abs. Any error other than "does not exist" reads as the
// file being there with an unknown state — never as a deletion.
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

// same reports whether two stats describe the same file state.
func (st diskStat) same(o diskStat) bool {
	return st.missing == o.missing && st.size == o.size && st.mod.Equal(o.mod)
}

// docAbs is where a working-tree document lives on disk; "" for a commit or
// shelf version, or before the worktree is known.
func (m Model) docAbs(d *openFile) string {
	if d.src.kind != srcWorktree || m.currentWorktree == "" {
		return ""
	}
	return filepath.Join(m.currentWorktree, filepath.FromSlash(d.path))
}

// loadDoc (re)reads d from its source off the UI thread.
func (m Model) loadDoc(d *openFile) tea.Cmd { return m.loadDocWith(d, m.docLoader(d)) }

// loadDocWith reads d through load. A working-tree document is stat'ed
// BEFORE the read and the load carries that stat, so the document's disk
// state is never newer than its bytes (an edit racing the read shows as a
// change on the next poll). A file gone from disk loads as the deleted
// placeholder, not as a read error.
func (m Model) loadDocWith(d *openFile, load func(context.Context) ([]byte, error)) tea.Cmd {
	d.loading = true
	read := loadFileContentSrcCmd(d.tag, d.path, m.cfg.UI.SyntaxOn(), load)
	abs := m.docAbs(d)
	if abs == "" {
		return read
	}
	tag := d.tag
	return func() tea.Msg {
		st := statDisk(abs)
		if st.missing {
			return fileContentMsg{tag: tag, lines: []contentLine{{text: i18n.T("(file deleted on disk)")}}, disk: st}
		}
		msg := read().(fileContentMsg)
		msg.disk = st
		return msg
	}
}

// backgroundPollEvery is how often a background open file's disk is looked
// at; a file on screen is looked at on every heartbeat (~1 s).
const backgroundPollEvery = 5 * time.Second

// docWatchState is the open-files watcher's state on Model.
type docWatchState struct {
	gen     int  // bumped by reRoot: drops a poll or watcher from the old tree
	polling bool // a stat round is in flight (a 9p stat can outlast a tick)
	// The fsnotify wake-up (supported filesystems only): the watcher, the
	// paths it was last given (sorted), a build in flight, and a build that
	// failed (not retried until the next repo).
	w        *filewatch.Watcher
	paths    []string
	building bool
	broken   bool
}

// openFilesStatMsg is one stat round's results for worktree wt.
type openFilesStatMsg struct {
	gen   int
	wt    string
	stats []docStatResult
}

type docStatResult struct {
	tag  string
	disk diskStat
}

// watchedDocs is the current worktree's open working-tree files — the only
// documents whose bytes can change under them.
func (m Model) watchedDocs() []*openFile {
	var out []*openFile
	for _, d := range m.openFiles.list(m.currentWorktree) {
		if d.src.kind == srcWorktree {
			out = append(out, d)
		}
	}
	// F's live preview is on screen but not an open file (see
	// wtPreviewSettled): it is watched all the same.
	if d := m.filesPreview; d != nil && d.src.kind == srcWorktree && !slices.Contains(out, d) {
		out = append(out, d)
	}
	return out
}

// watchedDoc is the watched document tagged tag, or nil — the registry, or
// F's live preview, which is watched without being an open file.
func (m Model) watchedDoc(tag string) *openFile {
	if d := m.openFiles.findTag(tag); d != nil {
		return d
	}
	if d := m.filesPreview; d != nil && d.tag == tag {
		return d
	}
	return nil
}

// openFilesTick starts a stat round (off the UI thread) over the watched
// files that are due: a file on screen every tick, a background one every
// backgroundPollEvery. Nothing is polled mid-switch (svc already names the
// new tree while currentWorktree still names the old), while a round is in
// flight, or for a file whose load is in flight.
func (m Model) openFilesTick(now time.Time) (Model, tea.Cmd) {
	if m.openFiles == nil || m.currentWorktree == "" || m.loading {
		return m, nil
	}
	m, sync := m.syncDocWatch()
	if m.docWatch.polling {
		return m, sync
	}
	type due struct{ tag, abs string }
	var todo []due
	for _, d := range m.watchedDocs() {
		if d.loading || (!m.docShown(d) && now.Sub(d.checked) < backgroundPollEvery) {
			continue
		}
		d.checked = now
		todo = append(todo, due{d.tag, m.docAbs(d)})
	}
	if len(todo) == 0 {
		return m, sync
	}
	m.docWatch.polling = true
	gen, wt := m.docWatch.gen, m.currentWorktree
	return m, tea.Batch(sync, func() tea.Msg {
		msg := openFilesStatMsg{gen: gen, wt: wt}
		for _, t := range todo {
			msg.stats = append(msg.stats, docStatResult{t.tag, statDisk(t.abs)})
		}
		return msg
	})
}

// applyDocStats reloads each document whose disk changed. The first stat of
// a document never read with one is only recorded. A file gone from disk
// needs no branch of its own: its load yields the deleted placeholder, and
// the reload keeps the reader's place for when the file returns.
func (m Model) applyDocStats(msg openFilesStatMsg) (Model, tea.Cmd) {
	m.docWatch.polling = false
	if msg.gen != m.docWatch.gen || msg.wt != m.currentWorktree {
		return m, nil
	}
	var cmds []tea.Cmd
	for _, r := range msg.stats {
		d := m.watchedDoc(r.tag)
		if d == nil || d.loading || d.src.kind != srcWorktree || !r.disk.known {
			continue
		}
		switch {
		case !d.disk.known:
			d.disk = r.disk
		case !d.disk.same(r.disk):
			d.disk = r.disk
			cmds = append(cmds, m.reloadDocCmd(d))
		}
	}
	return m, tea.Batch(cmds...)
}

// reloadDocCmd reloads d because its disk changed: the fill keeps the
// reader's place as it is when the new lines arrive.
func (m Model) reloadDocCmd(d *openFile) tea.Cmd {
	load := m.loadDoc(d)
	return func() tea.Msg {
		msg := load().(fileContentMsg)
		msg.reload = true
		return msg
	}
}

// docWatchDebounce coalesces an editor's save (write, rename, chmod) into
// one wake-up.
const docWatchDebounce = 150 * time.Millisecond

// docWatchReadyMsg carries a watcher built off the UI thread (nil: the build
// failed).
type docWatchReadyMsg struct {
	gen   int
	w     *filewatch.Watcher
	paths []string
}

// docWatchEventMsg is one watched file that fsnotify saw change.
type docWatchEventMsg struct {
	gen  int
	path string
}

// docWatchClosedMsg ends a listen loop: its watcher was closed.
type docWatchClosedMsg struct{}

// docWatchListenCmd waits for the watcher's next change.
func docWatchListenCmd(w *filewatch.Watcher, gen int) tea.Cmd {
	return func() tea.Msg {
		p, ok := <-w.Events()
		if !ok {
			return docWatchClosedMsg{}
		}
		return docWatchEventMsg{gen: gen, path: p}
	}
}

// closeDocWatch closes w off the UI thread (fsnotify's Close is syscalls).
func closeDocWatch(w *filewatch.Watcher) {
	if w != nil {
		go func() { _ = w.Close() }()
	}
}

// syncDocWatch keeps the fsnotify watcher over exactly the watched files:
// built with the first, told of every change to the set, closed with the
// last. Only where the repo's filesystem delivers events (watchSupported —
// not WSL's /mnt drives, where the poll alone serves). Every syscall runs
// off the UI thread.
func (m Model) syncDocWatch() (Model, tea.Cmd) {
	if !m.watchSupported || m.docWatch.broken {
		return m, nil
	}
	var paths []string
	for _, d := range m.watchedDocs() {
		paths = append(paths, m.docAbs(d))
	}
	slices.Sort(paths)
	switch {
	case len(paths) == 0:
		closeDocWatch(m.docWatch.w)
		m.docWatch.w, m.docWatch.paths = nil, nil
		return m, nil
	case m.docWatch.w == nil:
		if m.docWatch.building {
			return m, nil
		}
		m.docWatch.building = true
		gen := m.docWatch.gen
		return m, func() tea.Msg {
			w, err := filewatch.New(docWatchDebounce)
			if err != nil {
				return docWatchReadyMsg{gen: gen}
			}
			w.Set(paths)
			return docWatchReadyMsg{gen: gen, w: w, paths: paths}
		}
	case !slices.Equal(paths, m.docWatch.paths):
		m.docWatch.paths = paths
		w := m.docWatch.w
		return m, func() tea.Msg { w.Set(paths); return nil }
	}
	return m, nil
}

// docWatchReady stores a built watcher and starts listening to it; one built
// for a tree since left is closed.
func (m Model) docWatchReady(msg docWatchReadyMsg) (Model, tea.Cmd) {
	if msg.gen != m.docWatch.gen {
		closeDocWatch(msg.w)
		return m, nil
	}
	m.docWatch.building = false
	if msg.w == nil {
		m.docWatch.broken = true // the poll alone serves
		return m, nil
	}
	m.docWatch.w, m.docWatch.paths = msg.w, msg.paths
	return m, docWatchListenCmd(msg.w, msg.gen)
}

// docWatchEvent makes the changed file due now and polls: fsnotify only
// wakes the poll, which decides (by stat) whether anything changed.
func (m Model) docWatchEvent(msg docWatchEventMsg) (Model, tea.Cmd) {
	if msg.gen != m.docWatch.gen {
		return m, nil
	}
	for _, d := range m.watchedDocs() {
		if m.docAbs(d) == msg.path {
			d.checked = time.Time{}
		}
	}
	var listen tea.Cmd
	if m.docWatch.w != nil {
		listen = docWatchListenCmd(m.docWatch.w, m.docWatch.gen)
	}
	m, poll := m.openFilesTick(time.Now())
	return m, tea.Batch(poll, listen)
}
