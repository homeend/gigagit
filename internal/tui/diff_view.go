package tui

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/i18n"
	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/syntax"
	"github.com/homeend/gigagit/internal/textdiff"
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
// offset is the free-scroll viewport; curLine is the independent line
// cursor (see diff_cursor.go) — arrows/wheel move only the former, j/k/pgup/
// pgdown/home/end/n/p move the latter and scroll minimally to follow it.
type diffView struct {
	title      string         // file path, shown in the header
	context    string         // "HEAD → working tree" or "@ <short-hash> <subject>"
	rev        string         // commit-ish the NEW side came from; "" = working tree (used by h→history)
	compare    bool           // two-sided compare (title is "a ↔ b", not a path): no single focused file to bookmark/shelve
	full       []textdiff.Row // immutable aligned rows (the comparison result)
	fullBlocks []int          // immutable change-block starts into full
	// oldTok/newTok alias the (shared, cached) domain.Diff runs — READ-ONLY.
	oldTok     [][]syntax.Tok  // syntax runs per OLD source line (index = LeftNo-1); nil = plain
	newTok     [][]syntax.Tok  // syntax runs per NEW source line (index = RightNo-1); nil = plain
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
	cur        int         // focused change-block index (the "X" in change X/N); set only by n/p/wrap/mode-toggle
	curLine    int         // cursor: index into lines (never a display row; never a fold) — see diff_cursor.go
	wrapArm    wrapDir     // boundary press primed a wrap-around (see wrapDir); cleared on any other key
	fileArm    fileArmDir  // top/bottom press primed a step to the prev/next file; cleared on any other key
	zCycle     cursorAlign // the alignment the NEXT z applies (center → top → bottom); reset by any other key
	// notes are the resolved review notes for this view's address, loaded
	// asynchronously (notesLoadedMsg) and re-loaded after every mutation and
	// srcNotes refresh. relayout turns them into synthetic display rows;
	// nothing here ever touches the shared cached textdiff rows.
	notes []domain.ResolvedNote
	// hideAgent mirrors Model.notesAgentOff onto the view, because relayout
	// (called by rebuild, ctrl+w and every resize) has no Model to ask.
	hideAgent bool
	// noteAddr is the address notes on THIS view hang off — the pair of texts
	// domain.noteSideLines will re-read for the sweep. Stamped by the loader
	// that knows which two sides it is comparing, never derived from Model
	// state at key time: a staged diff and an unstaged diff of the same path
	// look identical to the focus, yet name different old sides. A zero value
	// (Path == "") means "no address": notes are inert on this view, which is
	// what every two-sided compare loader leaves behind (a comparison's old
	// side is the compared revision, which no stored address can name).
	noteAddr model.FileAddress
}

// wrapDir records that a change-navigation key hit a boundary and primed a
// wrap-around: the next same-direction press jumps to the far end. Cleared by
// any other key (reset at the top of updateDiffViewKey).
type wrapDir int

const (
	wrapNone    wrapDir = iota
	wrapToStart         // at the last change: next n/ctrl+↓ wraps to the first
	wrapToEnd           // at the first change: next p/ctrl+↑ wraps to the last
)

// wrapCue is the header prompt shown while a wrap-around is primed ("" = none).
func wrapCue(d wrapDir) string {
	switch d {
	case wrapToStart:
		return i18n.T("↻ n again → top")
	case wrapToEnd:
		return i18n.T("↻ p again → bottom")
	}
	return ""
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

	note     *noteLine // non-nil: a synthetic note row belonging to `line`
	noteMark bool      // fold row: a note hides under this fold (◆ on the rule)
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
// lines for the current wrap mode and width. With no notes, wrap off (or width
// unset) is a 1:1 mapping — disp mirrors lines, dispBlocks == blocks — so
// rendering and navigation are byte-identical to the pre-wrap view. Wrap on
// expands each aligned row to max(leftSegs, rightSegs) display rows. Review
// notes append their synthetic rows after each line's content rows (and mark
// the fold that hides their anchor), so a view carrying notes is no longer 1:1.
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

	byLine, foldMark := v.noteRowIndex()

	for li := range v.lines {
		v.lineStart[li] = len(v.disp)
		ln := v.lines[li]
		switch {
		case ln.Fold > 0:
			v.disp = append(v.disp, dRow{line: li, fold: ln.Fold, noteMark: foldMark[li], first: true})
		case v.long != longWrap || width <= 0:
			v.disp = append(v.disp, dRow{line: li, row: ln.Row, first: true})
		default:
			leftSegs := wrapSide(ln.Row.Left, ln.Row.LeftSpans, tokAt(v.oldTok, ln.Row.LeftNo), ln.Row.Kind, false, tw)
			rightSegs := wrapSide(ln.Row.Right, ln.Row.RightSpans, tokAt(v.newTok, ln.Row.RightNo), ln.Row.Kind, true, tw)
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
		// The line's note rows close its block: the next logical line starts
		// after them, so lineStart, the cursor range and the block jumps all
		// keep pointing at content rows only.
		for i := range byLine[li] {
			nl := byLine[li][i]
			v.disp = append(v.disp, dRow{line: li, row: ln.Row, note: &nl})
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
func wrapSide(text string, spans []textdiff.Span, toks []syntax.Tok, kind textdiff.Kind, right bool, tw int) []cellSeg {
	if (!right && kind == textdiff.Add) || (right && kind == textdiff.Del) {
		return nil
	}
	disp, emph, cls := sanitizeCell(text, spans, toks)
	return wrapCells(disp, emph, cls, tw)
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

// scrollBy free-scrolls the viewport and resyncs the focused change to the new
// offset — but only when the offset actually moved, so a no-op scroll (e.g. j
// at the bottom, or any scroll in a diff smaller than the viewport) leaves the
// n/p focus intact. Used by the scroll keys + wheel; n/p/focusBlock never
// resync (cur stays authoritative for change navigation).
func (v *diffView) scrollBy(delta, body int) {
	before := v.offset
	v.scroll(delta, body)
	if v.offset != before {
		v.cur = v.deriveOrdinal()
	}
}

// deriveOrdinal is the change "under" the current scroll offset: the last block
// at or above the lead line. It's the lower-bound focus after a free scroll;
// n/p step up from there, so a bottom-bunched last change is still reachable.
func (v *diffView) deriveOrdinal() int {
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

// jumpTo positions block-start display row b with up to diffLead rows above it.
func (v *diffView) jumpTo(b, body int) {
	v.offset = b - diffLead
	if v.offset < 0 {
		v.offset = 0
	}
	v.scroll(0, body)
}

// focusBlock makes change-block index i the focused change (cur) and scrolls
// it into view. i is clamped to the valid range. cur — not the scroll offset —
// is the source of truth for "which change am I on", so a change that can't be
// top-anchored (bunched against EOF, or a diff smaller than the viewport) is
// still reachable and counted. No-op when there are no changes.
func (v *diffView) focusBlock(i, body int) {
	if len(v.dispBlocks) == 0 {
		return
	}
	if i < 0 {
		i = 0
	}
	if i > len(v.dispBlocks)-1 {
		i = len(v.dispBlocks) - 1
	}
	v.cur = i
	if i < len(v.blocks) {
		v.curLine = v.blocks[i] // blocks index v.lines; the block's first row
	}
	v.jumpTo(v.dispBlocks[i], body)
}

// nextBlock focuses the next change and reports whether it moved (false =
// already on the last change, the boundary the wrap arms on). offset may not
// change — the next change can be off-anchor — but cur always advances.
func (v *diffView) nextBlock(body int) bool {
	if len(v.dispBlocks) == 0 || v.cur >= len(v.dispBlocks)-1 {
		return false
	}
	v.focusBlock(v.cur+1, body)
	return true
}

// prevBlock focuses the previous change and reports whether it moved (false =
// already on the first change).
func (v *diffView) prevBlock(body int) bool {
	if v.cur <= 0 {
		return false
	}
	v.focusBlock(v.cur-1, body)
	return true
}

// currentBlockOrdinal is the focused change index (the "X" in change X/N),
// clamped defensively in case the block set shrank under cur.
func (v *diffView) currentBlockOrdinal() int {
	ord := v.cur
	if ord < 0 {
		ord = 0
	}
	if n := len(v.dispBlocks); n > 0 && ord > n-1 {
		ord = n - 1
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
		return i18n.T("HEAD → index (staged)")
	}
	return i18n.T("index → working tree")
}

// statusNoteAddress is the note address for a Status/Staged panel diff: the
// state naming exactly the pair of texts this view shows, so the sweep re-reads
// the same two sides later. The Staged panel is HEAD → index (StateStaged); the
// Files panel is index → working file (StateUnstaged), or nothing → working
// file for a file git has no index entry for (StateUntracked).
func (m Model) statusNoteAddress(f model.FileStatus, staged bool) model.FileAddress {
	st := model.StateUnstaged
	switch {
	case staged:
		st = model.StateStaged
	case f.Kind == model.KindUntracked:
		st = model.StateUntracked
	}
	return model.FileAddress{State: st, Worktree: m.currentWorktree, Branch: m.status.Branch, Path: f.Path}
}

// openStatusDiff opens the full-screen diff for a Status (staged=false) or
// Staged (staged=true) panel file and records diffNav so Home/End can step the
// panel. Shared by the panel enter handler (after canShowFileDiff) and the
// file-stepper. The width gate lives in canShowFileDiff at the enter boundary;
// stepping happens at an already-open width, so no re-check here.
func (m Model) openStatusDiff(f model.FileStatus, staged bool) (tea.Model, tea.Cmd) {
	m.diffNotice = "" // drop any stale notice; the stepper re-posts its arrival notice
	if staged {
		m.diffNav = diffNavStaged
	} else {
		m.diffNav = diffNavStatus
	}
	v := &diffView{title: f.Path, context: statusDiffContext(staged), rev: "", loading: true, partial: m.diffPartial, long: m.diffLong, noteAddr: m.statusNoteAddress(f, staged)}
	if dv := m.diffLayer(); dv != nil {
		*dv = *v // stepping: reuse the entry already on the stack
	} else {
		m = m.pushLayer(v)
	}
	m.diffTag = statusDiffTag(f.Path, staged)
	return m, m.loadStatusDiffCmd(f, staged)
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
	v := &diffView{title: f.Path, context: statusDiffContext(staged), rev: "", partial: m.diffPartial, long: m.diffLong, width: width, noteAddr: m.statusNoteAddress(f, staged)}

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
			out, err := differ.Diff(context.Background(), domain.Request{Key: "", Path: f.Path, OldPath: f.OrigPath, Old: oldSrc, New: newSrc})
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
		out, err := differ.Diff(context.Background(), domain.Request{Key: "", Path: f.Path, Old: oldSrc, New: newSrc})
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
		v.oldTok, v.newTok = out.OldTok, out.NewTok
		v.truncated = out.Result.Truncated
		v.rebuild()
		if len(v.dispBlocks) > 0 {
			v.focusBlock(0, body) // open on the first change (cur = 0, cursor on its first row)
		} else {
			v.setCursorLine(0, body)
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
	v := &diffView{title: line.path, context: "@ " + m.filesContext, rev: hash, partial: m.diffPartial, long: m.diffLong, width: width,
		// hash^ → hash is exactly StateCommitted's pair (noteSideLines).
		noteAddr: model.FileAddress{State: model.StateCommitted, Commit: hash, Path: line.path}}
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
		out, err := differ.Diff(context.Background(), domain.Request{Key: key, Path: line.path, OldPath: line.oldPath, Old: oldSrc, New: newSrc})
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
// no old side, a "D" status no new side. The result fills a PRIVATE view (the
// loadCommitDiffCmd pattern) applied by the tag-gated diffMsg handler — the
// closure must never touch the live layer, or a superseded load (file-stepping
// outrunning a slow read; easy when cached commit↔commit neighbors answer
// instantly) clobbers the file the view has since moved to. Scalars snapshot
// the layer the caller just built (context/rev vary per caller).
func (m Model) loadCompareDiffCmd(left, right model.Endpoint, line contentLine) tea.Cmd {
	svc := m.svc
	differ := m.diffDiffer()
	body := m.diffBodyRows()
	width, _ := m.overlayDims()
	tag := "cmp:" + left.CacheTag() + ":" + right.CacheTag() + ":" + line.path
	v := &diffView{title: line.path, partial: m.diffPartial, long: m.diffLong, width: width}
	if dv := m.diffLayer(); dv != nil {
		v.context, v.rev = dv.context, dv.rev
	}
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
		out, err := differ.Diff(context.Background(), domain.Request{Key: key, Path: line.path, OldPath: line.oldPath, Old: oldSrc, New: newSrc})
		if err != nil {
			v.err = err
			return diffMsg{tag: tag, view: v}
		}
		applyDiff(v, out, body)
		return diffMsg{tag: tag, view: v}
	}
}

// update lets a diffView live on the layer stack: it delegates to the existing
// Model-side key handler (which finds this diff via m.diffLayer()) and adapts the
// tea.Model return to the layer interface's Model.
func (v *diffView) update(m Model, msg tea.KeyMsg) (Model, tea.Cmd) {
	nm, cmd := m.updateDiffViewKey(msg)
	return nm.(Model), cmd
}

// render draws the full-screen diff. Like the other surfaces it owns the screen
// and ignores the backdrop.
func (v *diffView) render(m Model, below string) string {
	return m.renderDiffView()
}

// updateDiffViewKey routes keys while the diff view is open: scrolling,
// change-block jumps, close/quit. Everything else is swallowed — no action
// key can reach the panels behind a full-screen view.
func (m Model) updateDiffViewKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	v := m.diffLayer()
	if msg.Type == tea.KeyCtrlC {
		return m, tea.Quit
	}
	// A wrap-around is primed by a boundary press and survives only until the
	// very next key: capture it, then disarm. Any case other than the matching
	// n/p re-arm leaves it cleared (so scrolling away cancels a primed wrap).
	armed := v.wrapArm
	v.wrapArm = wrapNone
	// The file-step arm and the transient bottom-left notice both live exactly
	// until the next key: capture the arm, then clear both. Any case other than a
	// matching home/end re-arm leaves them cleared (so any other key cancels a
	// primed step and dismisses the notice).
	fileArmed := v.fileArm
	v.fileArm = fileArmNone
	m.diffNotice = ""
	zc := v.zCycle
	v.zCycle = alignCenter
	body := m.diffBodyRows()
	switch msg.String() {
	case ".":
		return m.openActionMenu(), nil
	case "g": // global bookmark quick-switcher
		return m.openBookmarkSwitcher()
	case "G": // global shelf quick-switcher
		return m.openShelfSwitcher()
	case "F": // global fuzzy file finder
		return m.openFileFinder()
	// q is inert here: only the base layout quits on q. esc is the back key;
	// ctrl+c (handled above) remains the universal quit.
	case "esc":
		m = m.popLayer()
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
	case "e":
		if r, ok := m.diffEditRow(); ok {
			nm, cmd := r.run(m)
			return nm.(Model), cmd
		}
	case "c":
		return m.openNotePopup(noteAdd)
	case "E":
		return m.openNotePopup(noteEdit)
	case "R":
		return m.openNotePopup(noteReply)
	case "a":
		// Session-scoped: the flag lives on the Model and is mirrored onto
		// every view that relayouts (relayout has no Model to ask).
		m.notesAgentOff = !m.notesAgentOff
		v.hideAgent = m.notesAgentOff
		cr, hadRow := v.cursorRow()
		wasVisible := v.cursorVisible(body)
		v.relayout(v.width)
		v.reanchorAfterRebuild(cr, hadRow, wasVisible, body)
	case "}":
		var moved bool
		if m, moved = m.jumpNote(1); !moved {
			switch {
			case m.diffNav == diffNavNone || !m.peekNotedFile(1):
				m.diffNotice = i18n.T("▸ no next file with notes")
			case fileArmed == fileArmNextNote:
				return m.stepNotedFile(1)
			default:
				v.fileArm = fileArmNextNote
			}
		}
	case "{":
		var moved bool
		if m, moved = m.jumpNote(-1); !moved {
			switch {
			case m.diffNav == diffNavNone || !m.peekNotedFile(-1):
				m.diffNotice = i18n.T("▸ no previous file with notes")
			case fileArmed == fileArmPrevNote:
				return m.stepNotedFile(-1)
			default:
				v.fileArm = fileArmPrevNote
			}
		}
	case "up":
		v.scrollBy(-1, body)
	case "down":
		v.scrollBy(1, body)
	case "k", "alt+up":
		v.moveCursor(-1, body)
	case "j", "alt+down":
		v.moveCursor(1, body)
	case "pgup":
		v.scrollBy(-body, body)
		v.pageCursor(-body, body)
	case "pgdown":
		v.scrollBy(body, body)
		v.pageCursor(body, body)
	case "z":
		v.alignCursor(zc, body)
		v.zCycle = (zc + 1) % 3
	case "home":
		// First press jumps to the top of this file. At the top, a press primes a
		// step (bottom-left cue); the next home performs it (with an arrival
		// notice). No previous file → a "no previous file" notice instead. The
		// top/bottom check reads offset from BEFORE the cursor jump below —
		// setCursorLine's own minimal scroll must not masquerade as "already
		// there" and skip straight to arming on the very first press.
		wasTop := v.offset <= 0
		v.setCursorLine(0, body)
		if !wasTop {
			v.scrollBy(-len(v.disp), body)
		} else if m.diffNav != diffNavNone {
			switch {
			case !m.peekDiffFile(-1):
				m.diffNotice = i18n.T("▸ no previous file")
			case fileArmed == fileArmPrev:
				return m.stepDiffFile(-1)
			default:
				v.fileArm = fileArmPrev
			}
		}
	case "end":
		// Mirror of home: first press jumps to the bottom, then prime, then step.
		wasBottom := v.offset >= len(v.disp)-body
		v.setCursorLine(len(v.lines)-1, body)
		if !wasBottom {
			v.scrollBy(len(v.disp), body)
		} else if m.diffNav != diffNavNone {
			switch {
			case !m.peekDiffFile(1):
				m.diffNotice = i18n.T("▸ no next file")
			case fileArmed == fileArmNext:
				return m.stepDiffFile(1)
			default:
				v.fileArm = fileArmNext
			}
		}
	case "n", "ctrl+down":
		if !v.nextBlock(body) { // already on the last change
			if armed == wrapToStart && len(v.dispBlocks) > 0 {
				v.focusBlock(0, body) // second press wraps to the first
			} else {
				v.wrapArm = wrapToStart // first press primes the wrap
			}
		}
	case "p", "ctrl+up":
		if !v.prevBlock(body) { // already on the first change
			if armed == wrapToEnd && len(v.dispBlocks) > 0 {
				v.focusBlock(len(v.dispBlocks)-1, body) // wraps to the last
			} else {
				v.wrapArm = wrapToEnd
			}
		}
	case "N":
		// From the last change only — the mirror of n's wrap, but stepping files.
		// Double-press: the first press primes fileArmNext (the same arm End uses),
		// the second steps to the next file. Inert on any earlier change.
		if v.onLastBlock() && m.diffNav != diffNavNone {
			switch {
			case !m.peekDiffFile(1):
				m.diffNotice = i18n.T("▸ no next file")
			case fileArmed == fileArmNext:
				return m.stepDiffFile(1)
			default:
				v.fileArm = fileArmNext
			}
		}
	case "P":
		// Mirror of N from the first change: double-press steps to the previous file.
		if v.onFirstBlock() && m.diffNav != diffNavNone {
			switch {
			case !m.peekDiffFile(-1):
				m.diffNotice = i18n.T("▸ no previous file")
			case fileArmed == fileArmPrev:
				return m.stepDiffFile(-1)
			default:
				v.fileArm = fileArmPrev
			}
		}
	case "f":
		ord := v.currentBlockOrdinal()
		cr, hadRow := v.cursorRow()
		wasVisible := v.cursorVisible(body)
		v.partial = !v.partial
		v.rebuild()
		m.diffPartial = v.partial
		if len(v.dispBlocks) > 0 {
			v.focusBlock(ord, body) // re-anchor the same change (count is mode-invariant)
		} else {
			v.cur, v.offset = 0, 0
		}
		v.reanchorAfterRebuild(cr, hadRow, wasVisible, body)
	case "ctrl+w":
		ord := v.currentBlockOrdinal()
		cr, hadRow := v.cursorRow()
		wasVisible := v.cursorVisible(body)
		v.long = (v.long + 1) % 3
		v.hOffset = 0
		v.relayout(v.width)
		m.diffLong = v.long
		if len(v.dispBlocks) > 0 {
			v.focusBlock(ord, body)
		} else {
			v.cur, v.offset = 0, 0
		}
		v.reanchorAfterRebuild(cr, hadRow, wasVisible, body)
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
