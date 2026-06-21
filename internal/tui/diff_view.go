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

// longMode is how the diff view shows lines wider than a pane. The zero value
// (scroll) is the default. z cycles scroll → wrap → truncate → scroll.
type longMode int

const (
	longScroll   longMode = iota // horizontal pan (←/→), the default
	longWrap                     // word-wrap across display rows
	longTruncate                 // cut with a trailing …
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
	long       longMode        // mode: how lines wider than a pane are shown (default scroll)
	hOffset    int             // scroll mode: horizontal pan column (0 = left edge)
	maxCell    int             // scroll mode: widest cell width (pan clamp); set by relayout
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
		case v.long != longWrap || width <= 0:
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
	if v.long == longScroll {
		v.maxCell = maxCellWidth(v.lines)
		v.clampHOffset()
	}
	v.scroll(0, 1) // clamp offset into the new range (body-agnostic floor)
}

// clampHOffset keeps the horizontal pan within [0, maxCell - tw].
func (v *diffView) clampHOffset() {
	paneW := (v.width - 1) / 2
	if paneW < 4 {
		paneW = 4
	}
	tw := paneW - gutterWidth(v.full) - 1
	if tw < 1 {
		tw = 1
	}
	max := v.maxCell - tw
	if max < 0 {
		max = 0
	}
	if v.hOffset > max {
		v.hOffset = max
	}
	if v.hOffset < 0 {
		v.hOffset = 0
	}
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
// diffDiffer returns the Service's cached differ, or an uncached one for a
// Model built without a Service (minimal test fixtures). Production always
// has a Service via New/reRoot.
func (m Model) diffDiffer() domain.Differ {
	if m.svc == nil {
		return domain.NewDiffer(domain.DifferOptions{Enhanced: true}, nil)
	}
	return m.svc.Differ()
}

// statusDiffTag / statusDiffContext keep the tag and header label in sync
// between the enter handler (which pre-builds the loading view) and the
// loader. The Files panel diffs index → working tree (the unstaged delta); the
// Staged panel diffs HEAD → index (the staged delta). A partially-staged file
// appears in both panels, so the tags must differ to gate stale results.
func statusDiffTag(path string, staged bool) string {
	if staged {
		return "staged:" + path
	}
	return "status:" + path
}

func statusDiffContext(staged bool) string {
	if staged {
		return "HEAD → index (staged)"
	}
	return "index → working tree"
}

// loadStatusDiffCmd fetches both sides of f's pending change and compares them
// off the UI thread. With staged=true (Staged panel) it diffs HEAD → the index
// blob (`git show :<path>`, the staged delta). With staged=false (Files panel)
// it diffs the index blob → the file on disk (the UNSTAGED delta, matching the
// Files panel's status letter): a partially-staged file's staged hunk is
// excluded here and shows in the Staged panel instead. The disk path is rooted
// at the current worktree because porcelain paths are repo-root-relative and
// the process cwd may be a subdirectory. Both are uncached (Key: "") because
// the working tree and the index are mutable.
func (m Model) loadStatusDiffCmd(f model.FileStatus, staged bool) tea.Cmd {
	svc := m.svc
	differ := m.diffDiffer()
	root := m.currentWorktree
	body := m.diffBodyRows()
	width, _ := m.overlayDims()
	tag := statusDiffTag(f.Path, staged)
	v := &diffView{title: f.Path, context: statusDiffContext(staged), rev: "", partial: m.diffPartial, long: m.diffLong, width: width}

	// Staged (HEAD → index): old side is the HEAD blob, absent when the file
	// isn't in HEAD (untracked, or staged-new 'A'); renames fetch the old name.
	// New side is the index blob, absent for a staged delete ('D').
	if staged {
		var oldSrc domain.ByteSource
		if f.Kind != model.KindUntracked && f.Staged != 'A' {
			p := f.Path
			if f.OrigPath != "" {
				p = f.OrigPath
			}
			oldSrc = func(ctx context.Context) ([]byte, error) { return svc.ShowFile(ctx, "HEAD", p) }
		}
		var newSrc domain.ByteSource
		if f.Staged != 'D' {
			newSrc = func(ctx context.Context) ([]byte, error) { return svc.ShowFile(ctx, "", f.Path) }
		}
		return func() tea.Msg {
			out, err := differ.Diff(context.Background(), domain.Request{Key: "", Old: oldSrc, New: newSrc})
			if err != nil {
				v.err = err
				return diffMsg{tag: tag, view: v}
			}
			applyDiff(v, out, body)
			return diffMsg{tag: tag, view: v}
		}
	}

	// Files (index → working tree): old side is the INDEX blob at the file's
	// path, absent for an untracked file (no index entry → all-add). New side
	// is the file on disk (built below).
	var oldSrc domain.ByteSource
	if f.Kind != model.KindUntracked {
		oldSrc = func(ctx context.Context) ([]byte, error) { return svc.ShowFile(ctx, "", f.Path) }
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
	v := &diffView{title: line.path, context: "@ " + strings.TrimPrefix(m.filesTitle, "Files "), rev: hash, partial: m.diffPartial, long: m.diffLong, width: width}
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

// compareDiffKey is the per-file diff cache key for a comparison. It returns ""
// (cache bypass) whenever either side is a live endpoint (working tree/index),
// whose bytes change on disk; commit↔commit is immutable and stays cached.
func compareDiffKey(left, right model.Endpoint, path string) string {
	if left.IsLive() || right.IsLive() {
		return ""
	}
	return left.CacheTag() + ".." + right.CacheTag() + ":" + path
}

// loadCompareDiffCmd computes one file's diff between two endpoints. Each side
// resolves through ResolveBytes (commit / staged / unstaged); an "A" status has
// no old side, a "D" status no new side. The diff view is already constructed
// by the caller.
func (m Model) loadCompareDiffCmd(left, right model.Endpoint, line contentLine) tea.Cmd {
	svc := m.svc
	differ := m.diffDiffer()
	body := m.diffBodyRows()
	tag := "cmp:" + left.CacheTag() + ":" + right.CacheTag() + ":" + line.path
	v := m.diffView
	key := compareDiffKey(left, right, line.path)

	oldP := line.path
	if line.oldPath != "" {
		oldP = line.oldPath
	}
	var oldSrc, newSrc domain.ByteSource
	if line.status != "A" {
		ref := left.FileRef(oldP)
		oldSrc = func(ctx context.Context) ([]byte, error) { return svc.ResolveBytes(ctx, ref) }
	}
	if line.status != "D" {
		ref := right.FileRef(line.path)
		newSrc = func(ctx context.Context) ([]byte, error) { return svc.ResolveBytes(ctx, ref) }
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
	case ".":
		return m.openActionMenu(), nil
	case "g": // global bookmark quick-switcher
		return m.openBookmarkSwitcher()
	case "G": // global shelf quick-switcher
		return m.openShelfSwitcher()
	// q is inert here: only the base layout quits on q. esc is the back key;
	// ctrl+c (handled above) remains the universal quit.
	case "esc":
		m.diffView = nil
		m.diffTag = ""
		return m, nil
	case "h":
		ctx := navContext{path: v.title, rev: v.rev}
		hv := newHistoryView(ctx)
		m = m.pushLayer(hv)
		return m, m.loadHistoryListCmd(ctx, hv.listTag)
	case "b":
		ctx := navContext{path: v.title, rev: v.rev}
		bv := newBlameView(ctx)
		m = m.pushLayer(bv)
		return m, m.loadBlameCmd(ctx, bv.tag)
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
	case "z":
		ord := v.currentBlockOrdinal()
		v.long = (v.long + 1) % 3
		v.hOffset = 0
		v.relayout(v.width)
		m.diffLong = v.long
		if len(v.dispBlocks) > 0 {
			if ord >= len(v.dispBlocks) {
				ord = len(v.dispBlocks) - 1
			}
			v.jumpTo(v.dispBlocks[ord], m.diffBodyRows())
		} else {
			v.offset = 0
		}
	case "left":
		if v.long == longScroll {
			v.hOffset -= m.hscrollStep()
			v.clampHOffset()
		}
	case "right":
		if v.long == longScroll {
			v.hOffset += m.hscrollStep()
			v.clampHOffset()
		}
	case "0":
		if v.long == longScroll {
			v.hOffset = 0
		}
	}
	return m, nil
}
