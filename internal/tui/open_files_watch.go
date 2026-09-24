package tui

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	tea "github.com/charmbracelet/bubbletea"

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
	return out
}

// openFilesTick starts a stat round (off the UI thread) over the watched
// files that are due: a file on screen every tick, a background one every
// backgroundPollEvery. Nothing is polled mid-switch (svc already names the
// new tree while currentWorktree still names the old), while a round is in
// flight, or for a file whose load is in flight.
func (m Model) openFilesTick(now time.Time) (Model, tea.Cmd) {
	if m.openFiles == nil || m.currentWorktree == "" || m.loading || m.docWatch.polling {
		return m, nil
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
		return m, nil
	}
	m.docWatch.polling = true
	gen, wt := m.docWatch.gen, m.currentWorktree
	return m, func() tea.Msg {
		msg := openFilesStatMsg{gen: gen, wt: wt}
		for _, t := range todo {
			msg.stats = append(msg.stats, docStatResult{t.tag, statDisk(t.abs)})
		}
		return msg
	}
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
		d := m.openFiles.findTag(r.tag)
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
