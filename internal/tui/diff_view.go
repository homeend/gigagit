package tui

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/gigagit/gg/internal/model"
	"github.com/gigagit/gg/internal/textdiff"
)

// maxDiffBytes caps each fetched side of a diff; bigger files render as
// "(file too large)" instead of being buffered into the compare engine.
const maxDiffBytes = 10 << 20

// diffView is the open full-screen side-by-side viewer; nil = closed.
// Pure scroll (offset) — there is no cursor row.
type diffView struct {
	title     string // file path, shown in the header
	context   string // "HEAD → working tree" or "@ <short-hash> <subject>"
	rows      []textdiff.Row
	blocks    []int // jump targets for ctrl+↑/↓
	offset    int   // top visible row
	truncated bool  // alignment skipped (size guard)
	binary    bool
	tooLarge  bool
	loading   bool
	err       error
}

// scroll moves the viewport by delta, clamped to [0, len(rows)-body].
func (v *diffView) scroll(delta, body int) {
	v.offset += delta
	max := len(v.rows) - body
	if max < 0 {
		max = 0
	}
	if v.offset > max {
		v.offset = max
	}
	if v.offset < 0 {
		v.offset = 0
	}
}

// diffMsg delivers a fully built view from a loader; tag gates stale results
// (same pattern as commitFilesMsg/filesHash).
type diffMsg struct {
	tag  string
	view *diffView
}

// diffBodyRows is the viewer's visible row capacity: full height minus the
// header and hint lines.
func (m Model) diffBodyRows() int {
	_, h := m.overlayDims()
	n := h - 2
	if n < 1 {
		n = 1
	}
	return n
}

// loadStatusDiffCmd fetches both sides of f's working-tree change (HEAD →
// disk) and compares them off the UI thread. The disk path is rooted at the
// current worktree: porcelain paths are repo-root-relative and the process
// cwd may be a subdirectory.
func (m Model) loadStatusDiffCmd(f model.FileStatus) tea.Cmd {
	repo := m.repo
	root := m.currentWorktree
	tag := "status:" + f.Path
	v := &diffView{title: f.Path, context: "HEAD → working tree"}
	return func() tea.Msg {
		var oldB, newB []byte
		// Old side: absent when the file isn't in HEAD (untracked, or a
		// staged-new 'A' — ShowFile would fail on it). Renames fetch the
		// old name.
		if f.Kind != model.KindUntracked && f.Staged != 'A' {
			p := f.Path
			if f.OrigPath != "" {
				p = f.OrigPath
			}
			b, err := repo.ShowFile(context.Background(), "HEAD", p)
			if err != nil {
				v.err = err
				return diffMsg{tag: tag, view: v}
			}
			oldB = b
		}
		// New side: the working file; not-exists means deleted (absorbs the
		// delete/re-create porcelain combinations and races).
		full := filepath.Join(root, f.Path)
		switch st, err := os.Stat(full); {
		case err == nil && st.Size() > maxDiffBytes:
			v.tooLarge = true
			return diffMsg{tag: tag, view: v}
		case err == nil:
			b, rerr := os.ReadFile(full)
			if rerr != nil && !errors.Is(rerr, fs.ErrNotExist) {
				v.err = rerr
				return diffMsg{tag: tag, view: v}
			}
			newB = b
		case !errors.Is(err, fs.ErrNotExist):
			v.err = err
			return diffMsg{tag: tag, view: v}
		}
		fillDiff(v, oldB, newB)
		return diffMsg{tag: tag, view: v}
	}
}

// fillDiff runs the size/binary guards and the comparison into v.
func fillDiff(v *diffView, oldB, newB []byte) {
	if len(oldB) > maxDiffBytes || len(newB) > maxDiffBytes {
		v.tooLarge = true
		return
	}
	if textdiff.IsBinary(oldB) || textdiff.IsBinary(newB) {
		v.binary = true
		return
	}
	res := textdiff.Compare(oldB, newB)
	v.rows = res.Rows
	v.blocks = res.Blocks
	v.truncated = res.Truncated
}

// updateDiffViewKey routes keys while the diff view is open: scrolling,
// change-block jumps, close/quit. Everything else is swallowed — no action
// key can reach the panels behind a full-screen view.
func (m Model) updateDiffViewKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	v := m.diffView
	if msg.Type == tea.KeyCtrlC {
		return m, tea.Quit
	}
	switch msg.String() {
	case "q":
		return m, tea.Quit // q quits the app (top-level key, files-view precedent)
	case "esc":
		m.diffView = nil
		m.diffTag = ""
		return m, nil
	case "up", "k":
		v.scroll(-1, m.diffBodyRows())
	case "down", "j":
		v.scroll(1, m.diffBodyRows())
	case "pgup":
		v.scroll(-m.diffBodyRows(), m.diffBodyRows())
	case "pgdown":
		v.scroll(m.diffBodyRows(), m.diffBodyRows())
	case "ctrl+down":
		for _, b := range v.blocks {
			if b > v.offset {
				v.offset = b
				v.scroll(0, m.diffBodyRows()) // clamp near the end
				break
			}
		}
	case "ctrl+up":
		for i := len(v.blocks) - 1; i >= 0; i-- {
			if v.blocks[i] < v.offset {
				v.offset = v.blocks[i]
				break
			}
		}
	}
	return m, nil
}
