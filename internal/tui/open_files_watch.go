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
