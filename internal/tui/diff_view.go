package tui

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/gigagit/gg/internal/domain"
	"github.com/gigagit/gg/internal/model"
	"github.com/gigagit/gg/internal/textdiff"
)

// diffContext is the equal lines kept on each side of a change in partial
// mode; diffLead is the context kept above a change a jump lands on.
const (
	diffContext = 3
	diffLead    = 3
)

// diffView is the open full-screen side-by-side viewer; nil = closed.
// Pure scroll (offset) — there is no cursor row.
type diffView struct {
	title      string          // file path, shown in the header
	context    string          // "HEAD → working tree" or "@ <short-hash> <subject>"
	full       []textdiff.Row  // immutable aligned rows (the comparison result)
	fullBlocks []int           // immutable change-block starts into full
	partial    bool            // current display mode (false = full)
	lines      []textdiff.Line // displayed sequence for the current mode
	blocks     []int           // block-start indices into lines (jump targets)
	offset     int             // top visible line
	truncated  bool            // alignment skipped (size guard)
	binary     bool
	tooLarge   bool
	loading    bool
	err        error
}

// rebuild recomputes the displayed lines/blocks from the immutable rows for
// the current mode. Full mode wraps 1:1; partial mode collapses unchanged runs.
func (v *diffView) rebuild() {
	if v.partial {
		v.lines, v.blocks = textdiff.Collapse(v.full, v.fullBlocks, diffContext)
	} else {
		v.lines = textdiff.Expand(v.full)
		v.blocks = v.fullBlocks
	}
}

// scroll moves the viewport by delta, clamped to [0, len(lines)-body].
func (v *diffView) scroll(delta, body int) {
	v.offset += delta
	max := len(v.lines) - body
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

// jumpTo positions block-start line b with up to diffLead lines above it,
// clamped to the scroll range (scroll's clamp).
func (v *diffView) jumpTo(b, body int) {
	v.offset = b - diffLead
	if v.offset < 0 {
		v.offset = 0
	}
	v.scroll(0, body)
}

// nextBlock jumps to the first change strictly below the current one; no-op
// past the last. The +diffLead reference neutralizes the lead so the current
// change isn't re-selected.
func (v *diffView) nextBlock(body int) {
	for _, b := range v.blocks {
		if b > v.offset+diffLead {
			v.jumpTo(b, body)
			return
		}
	}
}

// prevBlock jumps to the first change strictly above the current one.
func (v *diffView) prevBlock(body int) {
	for i := len(v.blocks) - 1; i >= 0; i-- {
		if v.blocks[i] < v.offset+diffLead {
			v.jumpTo(v.blocks[i], body)
			return
		}
	}
}

// currentBlockOrdinal is the index of the change currently in view (for
// preserving position across a mode toggle).
func (v *diffView) currentBlockOrdinal() int {
	ord := 0
	for _, b := range v.blocks {
		if b <= v.offset+diffLead {
			ord++
		}
	}
	if ord > 0 {
		ord--
	}
	return ord
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

// diffDiffer returns the Service's diff engine. All code paths that call
// loadStatusDiffCmd or loadCommitDiffCmd hold a valid svc (set by New or
// reRoot); tests that use these loaders must wire svc (via footerModel).
func (m Model) diffDiffer() domain.Differ {
	return m.svc.Differ()
}

// loadStatusDiffCmd fetches both sides of f's working-tree change (HEAD →
// disk) and compares them off the UI thread. The disk path is rooted at the
// current worktree: porcelain paths are repo-root-relative and the process
// cwd may be a subdirectory.
func (m Model) loadStatusDiffCmd(f model.FileStatus) tea.Cmd {
	svc := m.svc
	differ := m.diffDiffer()
	root := m.currentWorktree
	body := m.diffBodyRows()
	tag := "status:" + f.Path
	v := &diffView{title: f.Path, context: "HEAD → working tree", partial: m.diffPartial}

	// Old side: absent when the file isn't in HEAD (untracked, or staged-new
	// 'A'). Renames fetch the old name.
	var oldSrc domain.ByteSource
	if f.Kind != model.KindUntracked && f.Staged != 'A' {
		p := f.Path
		if f.OrigPath != "" {
			p = f.OrigPath
		}
		oldSrc = func(ctx context.Context) ([]byte, error) { return svc.ShowFile(ctx, "HEAD", p) }
	}
	full := filepath.Join(root, f.Path)

	return func() tea.Msg {
		// New side: the working file. Stat first to size-guard without reading
		// a giant file into memory; not-exists means deleted (absorbs the
		// delete/re-create porcelain combinations and races).
		var newSrc domain.ByteSource
		switch st, err := os.Stat(full); {
		case err == nil && st.Size() > domain.MaxDiffBytes:
			v.tooLarge = true
			return diffMsg{tag: tag, view: v}
		case err == nil:
			newSrc = func(ctx context.Context) ([]byte, error) {
				b, rerr := os.ReadFile(full)
				if rerr != nil && !errors.Is(rerr, fs.ErrNotExist) {
					return nil, rerr
				}
				return b, nil // ErrNotExist ⇒ nil ⇒ deleted
			}
		case !errors.Is(err, fs.ErrNotExist):
			v.err = err
			return diffMsg{tag: tag, view: v}
		}
		// Working-tree diffs are never cached (Key: "").
		out, err := differ.Diff(context.Background(), domain.Request{Key: "", Old: oldSrc, New: newSrc})
		if err != nil {
			v.err = err
			return diffMsg{tag: tag, view: v}
		}
		applyDiff(v, out, body)
		return diffMsg{tag: tag, view: v}
	}
}

// applyDiff maps a domain.Diff outcome onto the view: size/binary state, or
// the aligned rows plus the open-at-first-difference jump.
func applyDiff(v *diffView, out domain.Diff, body int) {
	switch {
	case out.TooLarge:
		v.tooLarge = true
	case out.Binary:
		v.binary = true
	default:
		v.full = out.Result.Rows
		v.fullBlocks = out.Result.Blocks
		v.truncated = out.Result.Truncated
		v.rebuild()
		if len(v.blocks) > 0 {
			v.jumpTo(v.blocks[0], body)
		}
	}
}

// loadCommitDiffCmd fetches both sides of line's file in commit hash:
// first parent → commit. The tree was built by CommitFiles with
// --first-parent -m, and hash^ is the first parent, so the sides always
// match the tree's status letters (merge commits included). Root commits
// never dereference hash^ — all their files carry status "A".
func (m Model) loadCommitDiffCmd(hash string, line contentLine) tea.Cmd {
	svc := m.svc
	differ := m.diffDiffer()
	body := m.diffBodyRows()
	tag := "commit:" + hash + ":" + line.path
	v := &diffView{title: line.path, context: "@ " + strings.TrimPrefix(m.filesTitle, "Files "), partial: m.diffPartial}
	// Immutable: parent(hash)→hash for a path always yields the same bytes.
	key := hash + "^.." + hash + ":" + line.path

	var oldSrc, newSrc domain.ByteSource
	if line.status != "A" {
		p := line.path
		if line.oldPath != "" {
			p = line.oldPath
		}
		oldSrc = func(ctx context.Context) ([]byte, error) { return svc.ShowFile(ctx, hash+"^", p) }
	}
	if line.status != "D" {
		newSrc = func(ctx context.Context) ([]byte, error) { return svc.ShowFile(ctx, hash, line.path) }
	}
	return func() tea.Msg {
		out, err := differ.Diff(context.Background(), domain.Request{Key: key, Old: oldSrc, New: newSrc})
		if err != nil {
			v.err = err
			return diffMsg{tag: tag, view: v}
		}
		applyDiff(v, out, body)
		return diffMsg{tag: tag, view: v}
	}
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
		v.nextBlock(m.diffBodyRows())
	case "ctrl+up":
		v.prevBlock(m.diffBodyRows())
	case "n":
		v.nextBlock(m.diffBodyRows())
	case "p":
		v.prevBlock(m.diffBodyRows())
	case "f":
		ord := v.currentBlockOrdinal()
		v.partial = !v.partial
		v.rebuild()
		m.diffPartial = v.partial
		if len(v.blocks) > 0 {
			if ord >= len(v.blocks) {
				ord = len(v.blocks) - 1
			}
			v.jumpTo(v.blocks[ord], m.diffBodyRows())
		} else {
			v.offset = 0
		}
	}
	return m, nil
}
