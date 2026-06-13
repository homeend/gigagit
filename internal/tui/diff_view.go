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
	rev        string          // commit-ish the NEW side came from; "" = working tree (used by h→history)
	full       []textdiff.Row  // immutable aligned rows (the comparison result)
	fullBlocks []int           // immutable change-block starts into full
	partial    bool            // mode: collapse unchanged runs (false = full)
	wrap       bool            // mode: word-wrap long lines (false = truncate)
	width      int             // overlay width at last layout (0 = unset → wrap-off)
	lines      []textdiff.Line // logical (mode) stream that relayout consumes
	blocks     []int           // change-block starts into lines
	disp       []dRow          // display rows: what offset indexes and render draws
	dispBlocks []int           // change-block starts as display-row indices (jump targets)
	lineStart  []int           // logical line index → its first display-row index
	offset     int             // top visible display row
	truncated  bool            // alignment skipped (size guard)
	binary     bool
	tooLarge   bool
	loading    bool
	err        error
}

// dRow is one display row: a fold marker, or one (possibly continuation) slice
// of an aligned Row. line is the source index into v.lines (resize re-anchor).
// When wrap is off, a dRow carries the whole row (left/right unset) and the
// renderer draws it through diffCell; when wrap is on, left/right hold this
// row's pre-wrapped slice of each side.
type dRow struct {
	line  int          // source logical-line index
	fold  int          // >0: a fold separator of N unchanged lines (whole width)
	row   textdiff.Row // the source aligned row (Kind / gutter numbers / gap)
	left  cellSeg      // wrap-on: this display row's left slice (zero = blank)
	right cellSeg      // wrap-on: right slice
	first bool         // first display row of the source line (gutter shows here)
}

// rebuild recomputes the logical (mode) stream, then the display stream.
func (v *diffView) rebuild() {
	if v.partial {
		v.lines, v.blocks = textdiff.Collapse(v.full, v.fullBlocks, diffContext)
	} else {
		v.lines = textdiff.Expand(v.full)
		v.blocks = v.fullBlocks
	}
	v.relayout(v.width)
}

// relayout builds the display-row stream (disp/dispBlocks) from the logical
// lines for the current wrap mode and width. Wrap off (or width unset) is a
// 1:1 mapping — disp mirrors lines, dispBlocks == blocks — so rendering and
// navigation are byte-identical to the pre-wrap view. Wrap on expands each
// aligned row to max(leftSegs, rightSegs) display rows.
func (v *diffView) relayout(width int) {
	v.width = width
	v.disp = v.disp[:0]
	v.dispBlocks = v.dispBlocks[:0]
	if cap(v.lineStart) >= len(v.lines) {
		v.lineStart = v.lineStart[:len(v.lines)]
	} else {
		v.lineStart = make([]int, len(v.lines))
	}

	paneW := (width - 1) / 2
	if paneW < 4 {
		paneW = 4
	}
	gut := gutterWidth(v.full)
	tw := paneW - gut - 1
	if tw < 1 {
		tw = 1
	}

	for li := range v.lines {
		v.lineStart[li] = len(v.disp)
		ln := v.lines[li]
		switch {
		case ln.Fold > 0:
			v.disp = append(v.disp, dRow{line: li, fold: ln.Fold, first: true})
		case !v.wrap || width <= 0:
			v.disp = append(v.disp, dRow{line: li, row: ln.Row, first: true})
		default:
			leftSegs := wrapSide(ln.Row.Left, ln.Row.LeftSpans, ln.Row.Kind, false, tw)
			rightSegs := wrapSide(ln.Row.Right, ln.Row.RightSpans, ln.Row.Kind, true, tw)
			h := len(leftSegs)
			if len(rightSegs) > h {
				h = len(rightSegs)
			}
			if h < 1 {
				h = 1
			}
			for k := 0; k < h; k++ {
				v.disp = append(v.disp, dRow{line: li, row: ln.Row, first: k == 0,
					left: segAt(leftSegs, k), right: segAt(rightSegs, k)})
			}
		}
	}

	for _, b := range v.blocks {
		if b >= 0 && b < len(v.lineStart) {
			v.dispBlocks = append(v.dispBlocks, v.lineStart[b])
		}
	}
	v.scroll(0, 1) // clamp offset into the new range (body-agnostic floor)
}

// wrapSide sanitizes+wraps one side of a row into ≤tw segments, or returns nil
// for a gap side (the absent side of an Add/Del) so the renderer draws filler.
func wrapSide(text string, spans []textdiff.Span, kind textdiff.Kind, right bool, tw int) []cellSeg {
	if (!right && kind == textdiff.Add) || (right && kind == textdiff.Del) {
		return nil
	}
	disp, emph := sanitizeSpans(text, spans)
	return wrapCells(disp, emph, tw)
}

// segAt returns the kth segment or a blank cellSeg (a present-but-shorter side
// renders blank past its last segment).
func segAt(segs []cellSeg, k int) cellSeg {
	if k < len(segs) {
		return segs[k]
	}
	return cellSeg{}
}

// scroll moves the viewport by delta, clamped to [0, len(disp)-body].
func (v *diffView) scroll(delta, body int) {
	v.offset += delta
	max := len(v.disp) - body
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

// jumpTo positions block-start display row b with up to diffLead rows above it.
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
	for _, b := range v.dispBlocks {
		if b > v.offset+diffLead {
			v.jumpTo(b, body)
			return
		}
	}
}

// prevBlock jumps to the first change strictly above the current one.
func (v *diffView) prevBlock(body int) {
	for i := len(v.dispBlocks) - 1; i >= 0; i-- {
		if v.dispBlocks[i] < v.offset+diffLead {
			v.jumpTo(v.dispBlocks[i], body)
			return
		}
	}
}

// currentBlockOrdinal is the index of the change currently in view (for
// preserving position across a mode toggle).
func (v *diffView) currentBlockOrdinal() int {
	ord := 0
	for _, b := range v.dispBlocks {
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
	width, _ := m.overlayDims()
	tag := "status:" + f.Path
	v := &diffView{title: f.Path, context: "HEAD → working tree", rev: "", partial: m.diffPartial, wrap: m.diffWrap, width: width}

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
		if len(v.dispBlocks) > 0 {
			v.jumpTo(v.dispBlocks[0], body)
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
	width, _ := m.overlayDims()
	tag := "commit:" + hash + ":" + line.path
	v := &diffView{title: line.path, context: "@ " + strings.TrimPrefix(m.filesTitle, "Files "), rev: hash, partial: m.diffPartial, wrap: m.diffWrap, width: width}
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
	case "h":
		ctx := navContext{path: v.title, rev: v.rev}
		hv := newHistoryView(ctx)
		m = m.pushSurface(hv)
		return m, m.loadHistoryListCmd(ctx, hv.listTag)
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
		if len(v.dispBlocks) > 0 {
			if ord >= len(v.dispBlocks) {
				ord = len(v.dispBlocks) - 1
			}
			v.jumpTo(v.dispBlocks[ord], m.diffBodyRows())
		} else {
			v.offset = 0
		}
	case "w":
		ord := v.currentBlockOrdinal()
		v.wrap = !v.wrap
		v.relayout(v.width)
		m.diffWrap = v.wrap
		if len(v.dispBlocks) > 0 {
			if ord >= len(v.dispBlocks) {
				ord = len(v.dispBlocks) - 1
			}
			v.jumpTo(v.dispBlocks[ord], m.diffBodyRows())
		} else {
			v.offset = 0
		}
	}
	return m, nil
}
