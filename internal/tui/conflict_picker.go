package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/homeend/gigagit/internal/engine"
	"github.com/homeend/gigagit/internal/hunkpick"
	"github.com/homeend/gigagit/internal/i18n"
	"github.com/homeend/gigagit/internal/syntax"
)

// pickerFocus has no themed colour (bold only), so it stays a literal — it is
// not part of the theme role table.
var pickerFocus = lipgloss.NewStyle().Bold(true)

const pickerColSep = " ║ "

// hunkPicker is the shared region/line picker surface. It serves both the
// conflict resolver (current/incoming, every region must be decided) and hunk
// staging (index/working, no gate) — the difference is the injected labels,
// the requireAll gate, and the apply callback. A 2D cursor (block index, side,
// line) drives the picks.
type hunkPicker struct {
	title      string // header prefix, e.g. "Resolve conflicts: f" / "Stage hunks: f"
	leftLabel  string // "current" / "index"
	rightLabel string // "incoming" / "working"
	requireAll bool   // gate enter on Pending==0 (conflicts) vs apply freely (staging)
	apply      func(m Model, content []byte) (Model, tea.Cmd)

	// path is the repo-relative file the document came from; it selects the
	// syntax lexer (lexCmd, or withSyntax in tests) and nothing else.
	path string
	// curTok/incTok hold the syntax runs of the two assembled full-file sides,
	// indexed by line number − 1 (the tokAt numbering). nil = render plain.
	// The literal context lines are shared by both sides and take the CURRENT
	// side's runs; each block's lines take their own side's.
	curTok, incTok [][]syntax.Tok

	doc    *hunkpick.Doc
	blocks []*hunkpick.Block
	bi     int
	side   hunkpick.Side
	line   int

	mode    dispMode // display mode for candidate lines (default scroll)
	hscroll int      // modeScroll horizontal offset
	vshift  int      // free view-scroll: display-line delta from the cursor-anchored window

	outCollapsed bool // [o] hides the output pane; default shown
	outFocused   bool // tab moves the arrows to the output pane
	oshift       int  // output free-scroll: display-line delta from the follow-anchor window
	zoomed       bool // ctrl+t: the tab-focused half owns the whole body; zoom follows focus

	// Render caches. The doc's text is immutable while the picker is open,
	// so display sanitization runs once (sanBuilt); the assembled output
	// changes only when picks change, so it is keyed on pickRev (bumped by
	// every mutating key) — cursor motion and scrolling re-render from the
	// caches. Measured: the uncached render was O(document) per keystroke.
	sanBuilt bool
	sanLit   [][]sanLine // per doc.Items index: sanitized literal lines (nil for blocks)
	sanCur   [][]sanLine // per block index: sanitized current-side lines
	sanInc   [][]sanLine // per block index: sanitized incoming-side lines
	pickRev  int         // bumped on every pick mutation
	outBuilt bool
	outRev   int       // pickRev the output cache was built at
	outLines []sanLine // assembled output, reusing the grid's sanitized lines
	outStart []int     // per block index: first output line of the block's contribution

	lastGridH int // grid height at the last render — the pgup/pgdn page size
	lastOutH  int // output-pane height at the last render — its page size

	// search is the in-view text search (spec §4.3). Its rows are the FLAT
	// candidate list — per block, every Current line then every Incoming
	// line — so one hit names one (block, side, line) cursor position;
	// searchRows maps a row back, searchBase maps a cursor forward. Literal
	// context rows and the output pane are not in it and are therefore never
	// searched.
	search     textSearch
	searchOrig pickerOrigin
	searchRows []pickerAddr
	searchBase [][2]int // per block: the flat row each side's lines start at
}

// pickerAddr is where a flat search row lives in the 2D cursor.
type pickerAddr struct {
	bi   int
	side hunkpick.Side
	line int
}

// pickerOrigin is the cursor and viewport a live search restores on esc.
type pickerOrigin struct {
	bi      int
	side    hunkpick.Side
	line    int
	vshift  int
	hscroll int
}

const pickerHScrollStep = 8

// cyclePickerMode advances the picker's mode in the requested order
// scroll → wrap → cutoff → scroll. Given the enum (cutoff=0, wrap=1, scroll=2)
// that is a decrement; it is intentionally the reverse of dispMode.next() so
// the scroll default cycles to wrap next.
func cyclePickerMode(d dispMode) dispMode {
	return (d + dispModeCount - 1) % dispModeCount
}

// newConflictPicker wires the conflict-resolution params.
func newConflictPicker(path string, doc *hunkpick.Doc) *hunkPicker {
	return &hunkPicker{
		title:      i18n.T("Resolve conflicts: %s", path),
		leftLabel:  i18n.T("current"),
		rightLabel: i18n.T("incoming"),
		requireAll: true,
		apply: func(m Model, content []byte) (Model, tea.Cmd) {
			m = m.popLayer()
			return m.startOp(engine.ResolveConflictHunks{Path: path, Content: content})
		},
		path: path,
		doc:  doc, blocks: doc.Blocks(), side: hunkpick.Current, mode: modeScroll,
	}
}

// newProcessConflictPicker is the conflict picker owned by the conflict process
// (not the surface stack): on apply it returns the process to Working and starts
// the resolve job; esc is intercepted by the process, so this picker never pops
// a surface.
func newProcessConflictPicker(path string, doc *hunkpick.Doc) *hunkPicker {
	e := newConflictPicker(path, doc)
	e.apply = func(m Model, content []byte) (Model, tea.Cmd) {
		if cp, ok := m.proc.(*conflictProcess); ok {
			cp.picker = nil
			cp.st = confWorking
		}
		return m.startOp(engine.ResolveConflictHunks{Path: path, Content: content})
	}
	return e
}

// newStagePicker wires the hunk-staging params.
func newStagePicker(path string, doc *hunkpick.Doc) *hunkPicker {
	return &hunkPicker{
		title:      i18n.T("Stage hunks: %s", path),
		leftLabel:  i18n.T("index"),
		rightLabel: i18n.T("working"),
		requireAll: false,
		apply: func(m Model, content []byte) (Model, tea.Cmd) {
			m = m.popLayer()
			return m.startOp(engine.StageHunks{Path: path, Content: content})
		},
		path: path,
		doc:  doc, blocks: doc.Blocks(), side: hunkpick.Current, mode: modeScroll,
	}
}

// newUnstagePicker wires the hunk-unstaging params: the grid runs index ↔
// HEAD and the assembled content goes back through StageHunks, so taking the
// HEAD side of a region reverts that region of the index (git reset -p).
func newUnstagePicker(path string, doc *hunkpick.Doc) *hunkPicker {
	return &hunkPicker{
		title:      i18n.T("Unstage hunks: %s", path),
		leftLabel:  i18n.T("staged"),
		rightLabel: i18n.T("HEAD"),
		requireAll: false,
		apply: func(m Model, content []byte) (Model, tea.Cmd) {
			m = m.popLayer()
			return m.startOp(engine.StageHunks{Path: path, Content: content})
		},
		path: path,
		doc:  doc, blocks: doc.Blocks(), side: hunkpick.Current, mode: modeScroll,
	}
}

func (e *hunkPicker) cur() *hunkpick.Block {
	if e.bi < 0 || e.bi >= len(e.blocks) {
		return nil
	}
	return e.blocks[e.bi]
}

func (e *hunkPicker) sideLen() int {
	b := e.cur()
	if b == nil {
		return 0
	}
	if e.side == hunkpick.Incoming {
		return len(b.Incoming)
	}
	return len(b.Current)
}

// cursorUp / cursorDown move the line cursor one step, crossing regions the
// way the arrow keys always have; false means the edge was reached. pgup/pgdn
// are these steps repeated a viewport page at a time.
func (e *hunkPicker) cursorUp() bool {
	if e.line > 0 {
		e.line--
		return true
	}
	if e.bi > 0 {
		e.bi--
		e.line = e.sideLen() - 1
		if e.line < 0 {
			e.line = 0
		}
		return true
	}
	return false
}

func (e *hunkPicker) cursorDown() bool {
	if e.line < e.sideLen()-1 {
		e.line++
		return true
	}
	if e.bi < len(e.blocks)-1 {
		e.bi++
		e.line = 0
		return true
	}
	return false
}

// pageSize is the grid page for pgup/pgdn: the last rendered grid height
// (minus one row of overlap), with a sane floor before the first render.
func (e *hunkPicker) pageSize() int {
	if e.lastGridH > 1 {
		return e.lastGridH - 1
	}
	return 10
}

func (e *hunkPicker) clampLine() {
	n := e.sideLen()
	if e.line >= n {
		e.line = n - 1
	}
	if e.line < 0 {
		e.line = 0
	}
}

// tickFor renders the three-state checkbox for an (all, any) side state.
func tickFor(all, any bool) string {
	switch {
	case all:
		return "[x]"
	case any:
		return "[~]"
	default:
		return "[ ]"
	}
}

// stateSuffix names what the checkboxes cannot show: an untouched region, a
// touched-empty one, or — with both sides on — which side's lines come first.
func (e *hunkPicker) stateSuffix(b *hunkpick.Block) string {
	if b.Mode == hunkpick.Undecided {
		return " — " + i18n.T("undecided")
	}
	if b.Skipped() {
		return " — " + i18n.T("skipped")
	}
	ca, _ := b.SideState(hunkpick.Current)
	ia, _ := b.SideState(hunkpick.Incoming)
	if ca && ia && len(b.Picks) > 0 {
		lbl := e.leftLabel
		if b.Picks[0].Side == hunkpick.Incoming {
			lbl = e.rightLabel
		}
		return " — " + i18n.T("%s first", lbl)
	}
	return ""
}

// focusNextUndecided moves the cursor to the next Undecided region after the
// current one, wrapping around (the current region itself is the last
// candidate). ok=false when every region is decided; the cursor stays.
func (e *hunkPicker) focusNextUndecided() bool {
	n := len(e.blocks)
	for k := 1; k <= n; k++ {
		i := (e.bi + k) % n
		if e.blocks[i].Mode == hunkpick.Undecided {
			e.bi, e.line, e.side = i, 0, hunkpick.Current
			return true
		}
	}
	return false
}

// stepBlock moves the cursor delta regions, wrapping at both ends.
func (e *hunkPicker) stepBlock(delta int) {
	if n := len(e.blocks); n > 0 {
		e.bi, e.line = ((e.bi+delta)%n+n)%n, 0
	}
}

func (e *hunkPicker) focusFirstUndecided() {
	for i, b := range e.blocks {
		if b.Mode == hunkpick.Undecided {
			e.bi, e.line, e.side = i, 0, hunkpick.Current
			return
		}
	}
}

// ensureSan builds the once-per-picker sanitized copies of the doc's lines and
// their paint masks (display only — resolution reads the doc's raw lines and
// keeps CRLF). The two assembled sides are numbered INDEPENDENTLY: they share
// the literal context, but each block contributes a different number of lines
// to each side, so curNo and incNo advance separately. Literal context takes
// the CURRENT side's runs — it is byte-identical on both sides.
func (e *hunkPicker) ensureSan() {
	if e.sanBuilt {
		return
	}
	e.sanBuilt = true
	e.sanLit = make([][]sanLine, len(e.doc.Items))
	e.sanCur = make([][]sanLine, len(e.blocks))
	e.sanInc = make([][]sanLine, len(e.blocks))
	bi, curNo, incNo := 0, 0, 0
	for i, it := range e.doc.Items {
		if it.Block == nil {
			ls := make([]sanLine, len(it.Literal))
			for k, l := range it.Literal {
				ls[k] = sanPickLine(l, tokAt(e.curTok, curNo+k+1))
			}
			e.sanLit[i] = ls
			curNo += len(it.Literal)
			incNo += len(it.Literal)
			continue
		}
		cur := make([]sanLine, len(it.Block.Current))
		for k, l := range it.Block.Current {
			cur[k] = sanPickLine(l, tokAt(e.curTok, curNo+k+1))
		}
		inc := make([]sanLine, len(it.Block.Incoming))
		for k, l := range it.Block.Incoming {
			inc[k] = sanPickLine(l, tokAt(e.incTok, incNo+k+1))
		}
		e.sanCur[bi], e.sanInc[bi] = cur, inc
		curNo += len(it.Block.Current)
		incNo += len(it.Block.Incoming)
		bi++
	}
}

// ensureOutput (re)assembles the output lines and each block's start offset —
// only when the picks changed since the last build. Every line is the very
// sanLine the grid already holds (looked up through the block's ResolvedPicks
// provenance), so the pane never re-sanitizes and never re-lexes a pick.
func (e *hunkPicker) ensureOutput() {
	if e.outBuilt && e.outRev == e.pickRev {
		return
	}
	e.ensureSan()
	e.outBuilt, e.outRev = true, e.pickRev
	e.outLines = e.outLines[:0]
	if e.outStart == nil {
		e.outStart = make([]int, len(e.blocks))
	}
	bi := 0
	for i, it := range e.doc.Items {
		if it.Block == nil {
			e.outLines = append(e.outLines, e.sanLit[i]...)
			continue
		}
		e.outStart[bi] = len(e.outLines)
		if ps, ok := it.Block.ResolvedPicks(); ok {
			for _, p := range ps {
				side := e.sanCur[bi]
				if p.Side == hunkpick.Incoming {
					side = e.sanInc[bi]
				}
				// Belt and braces, never a rescue: ResolvedPicks already drops
				// out-of-range picks, and sanCur/sanInc are sized from the very
				// slices those picks index, so the lengths agree by
				// construction. The guard only covers a sanBuilt cache left
				// stale by some future edit.
				if p.Line >= 0 && p.Line < len(side) {
					e.outLines = append(e.outLines, side[p.Line])
				}
			}
		} else {
			e.outLines = append(e.outLines, sanLine{text: i18n.T("‹region %d undecided›", bi+1)})
		}
		bi++
	}
}

// ensureSearchRows flattens the candidate lines into the search's row space:
// for each block in order, every Current line, then every Incoming line. The
// shape depends only on the document, which is immutable while the picker is
// open, so it is built once — the async lexer rebuilds the masks but never the
// line texts, so hits stay valid across it.
func (e *hunkPicker) ensureSearchRows() {
	e.ensureSan()
	if e.searchBase != nil {
		return
	}
	e.searchBase = make([][2]int, len(e.blocks))
	e.searchRows = e.searchRows[:0]
	for bi := range e.blocks {
		e.searchBase[bi][0] = len(e.searchRows)
		for r := range e.sanCur[bi] {
			e.searchRows = append(e.searchRows, pickerAddr{bi: bi, side: hunkpick.Current, line: r})
		}
		e.searchBase[bi][1] = len(e.searchRows)
		for r := range e.sanInc[bi] {
			e.searchRows = append(e.searchRows, pickerAddr{bi: bi, side: hunkpick.Incoming, line: r})
		}
	}
}

// searchLines is the searchable text: every candidate line as the display
// string the grid paints.
func (e *hunkPicker) searchLines() []searchLine {
	e.ensureSearchRows()
	out := make([]searchLine, len(e.searchRows))
	for i, a := range e.searchRows {
		san, side := e.sanCur[a.bi], 0
		if a.side == hunkpick.Incoming {
			san, side = e.sanInc[a.bi], 1
		}
		out[i] = searchLine{row: i, side: side, text: san[a.line].text}
	}
	return out
}

// searchRow is the flat search row of a 2D cursor position, or -1 when there
// is none (no blocks).
func (e *hunkPicker) searchRow(bi int, side hunkpick.Side, line int) int {
	e.ensureSearchRows()
	if bi < 0 || bi >= len(e.searchBase) {
		return -1
	}
	k := 0
	if side == hunkpick.Incoming {
		k = 1
	}
	return e.searchBase[bi][k] + line
}

// searchPos is where ] and [ measure from — the current hit while the cursor is
// still on it, else the head of the cursor's own candidate line.
func (e *hunkPicker) searchPos() searchPos {
	row := e.searchRow(e.bi, e.side, e.line)
	side := 0
	if e.side == hunkpick.Incoming {
		side = 1
	}
	if e.search.cur >= 0 && e.search.cur < len(e.search.hits) {
		if h := e.search.hits[e.search.cur]; h.row == row && h.side == side {
			return searchPos{row: h.row, side: h.side, col: h.start}
		}
	}
	return searchPos{row: row, side: side, col: -1}
}

// textWidth is one candidate column's text width: the two-column split minus
// the fixed "  [ ] " gutter (cursor marker + checkbox).
func (e *hunkPicker) textWidth(w int) int {
	colW := (w - lipgloss.Width(pickerColSep)) / 2
	tw := colW - 6
	if tw < 1 {
		tw = 1
	}
	return tw
}

// goToHit selects hits[i]: the 2D cursor jumps to it (the render anchors its
// window on the cursor, so the scroll follows), the free view-scroll is
// released, focus returns to the grid, and scroll mode pans to its column.
func (e *hunkPicker) goToHit(m Model, i int) {
	if i < 0 || i >= len(e.search.hits) {
		return
	}
	h := e.search.hits[i]
	if h.row < 0 || h.row >= len(e.searchRows) {
		return
	}
	a := e.searchRows[h.row]
	e.search.cur = i
	e.bi, e.side, e.line = a.bi, a.side, a.line
	e.vshift = 0
	e.outFocused = false
	if e.mode != modeScroll {
		return
	}
	san := e.sanCur[a.bi]
	if a.side == hunkpick.Incoming {
		san = e.sanInc[a.bi]
	}
	w, _ := m.overlayDims()
	cs, ce := hitCols(san[a.line].text, h)
	e.hscroll = panFor(e.hscroll, e.textWidth(w), cs, ce)
}

// restoreSearchOrigin puts the cursor and viewport back where / or @ found them.
func (e *hunkPicker) restoreSearchOrigin() {
	o := e.searchOrig
	e.bi, e.side, e.line, e.vshift, e.hscroll = o.bi, o.side, o.line, o.vshift, o.hscroll
	e.clampLine()
}

func (e *hunkPicker) update(m Model, msg tea.KeyMsg) (Model, tea.Cmd) {
	if msg.Type == tea.KeyCtrlC {
		return m, tea.Quit
	}
	// While the search is capturing text it owns EVERY key: tab, ctrl+t and the
	// picks all wait. History recall runs inside searchTypingKey, which is why
	// alt+↑/↓ can still mean "scroll the view" below.
	if e.search.typing {
		nm, cmd, ev := m.searchTypingKey(&e.search, msg)
		m = nm
		switch ev {
		case searchChanged, searchCommitted:
			e.search.refindFrom(e.searchLines(), e.search.origin)
			if e.search.cur >= 0 {
				e.goToHit(m, e.search.cur)
			} else {
				e.restoreSearchOrigin()
			}
		case searchCancelled:
			e.restoreSearchOrigin()
		}
		return m, cmd
	}
	if msg.String() == "tab" {
		switch {
		case e.outCollapsed:
			e.outCollapsed, e.outFocused = false, true
		case e.outFocused:
			e.outFocused, e.oshift = false, 0
		default:
			e.outFocused = true
		}
		return m, nil
	}
	// ctrl+t zooms the tab-focused half (grid or output) to the whole body;
	// the zoomed half is not stored — render shows the focused one, so tab
	// swaps the zoom for free. esc restores the split before it can cancel.
	if msg.String() == "ctrl+t" {
		e.zoomed = !e.zoomed
		return m, nil
	}
	if e.zoomed && msg.String() == "esc" {
		e.zoomed = false
		return m, nil
	}
	if e.outFocused {
		// The output owns the plain arrows; global keys fall through, every
		// selection key waits until tab returns focus to the grid.
		switch msg.String() {
		case "up", "k":
			e.oshift--
			return m, nil
		case "down", "j":
			e.oshift++
			return m, nil
		case "pgup":
			e.oshift -= max(1, e.lastOutH)
			return m, nil
		case "pgdown":
			e.oshift += max(1, e.lastOutH)
			return m, nil
		case "o":
			e.outCollapsed, e.outFocused, e.oshift = true, false, 0
			e.zoomed = false
			return m, nil
		// / and @ are allowed through and move focus back to the grid: the
		// output pane is never searched, but a / pressed here must not look dead.
		case "esc", "enter", "ctrl+s", "ctrl+w", "shift+left", "shift+right", "alt+up", "alt+down", "/", "@", "[", "]":
		default:
			return m, nil
		}
	}
	switch msg.String() {
	case "alt+up":
		e.vshift--
		return m, nil
	case "alt+down":
		e.vshift++
		return m, nil
	}
	if e.vshift != 0 {
		// Any other key returns the viewport to the cursor first; a bare
		// up/down is consumed by that snap-back so the cursor stays put.
		e.vshift = 0
		switch msg.String() {
		case "up", "k", "down", "j", "pgup", "pgdown":
			return m, nil
		}
	}
	switch searchCommandKey(&e.search, msg) {
	case searchOpenFwd, searchOpenBack:
		e.outFocused = false // the search always lives in the grid
		e.searchOrig = pickerOrigin{bi: e.bi, side: e.side, line: e.line, vshift: e.vshift, hscroll: e.hscroll}
		e.search.open(msg.String() == "@", e.searchPos())
		return m.recallReset(), nil
	case searchNext:
		e.goToHit(m, stepHit(e.search.hits, e.searchPos(), 1))
		return m, nil
	case searchPrev:
		e.goToHit(m, stepHit(e.search.hits, e.searchPos(), -1))
		return m, nil
	case searchCleared:
		e.search.clear()
		return m, nil
	case searchIgnored:
		return m, nil
	}

	b := e.cur()
	switch msg.String() {
	case "esc":
		return m.popLayer(), nil
	case "left":
		e.side = hunkpick.Current
		e.clampLine()
	case "right":
		e.side = hunkpick.Incoming
		e.clampLine()
	case "ctrl+w":
		e.mode = cyclePickerMode(e.mode)
		e.hscroll = 0
	case "o":
		e.outCollapsed = !e.outCollapsed
		e.zoomed = false
	case "shift+left":
		if e.mode == modeScroll {
			if e.hscroll -= pickerHScrollStep; e.hscroll < 0 {
				e.hscroll = 0
			}
		}
	case "shift+right":
		if e.mode == modeScroll {
			e.hscroll += pickerHScrollStep
		}
	case "up", "k":
		e.cursorUp()
	case "down", "j":
		e.cursorDown()
	case "pgup":
		for i := 0; i < e.pageSize(); i++ {
			if !e.cursorUp() {
				break
			}
		}
	case "pgdown":
		for i := 0; i < e.pageSize(); i++ {
			if !e.cursorDown() {
				break
			}
		}
	case "n":
		e.stepBlock(+1)
	case "p":
		e.stepBlock(-1)
	case "s":
		// Skip: decide the region with nothing from either side and move on
		// (conflict picker: next undecided; staging pickers: reset the hunk
		// to its default — nothing staged / everything stays staged — and
		// step to the next hunk). s on a skipped conflict region un-skips it.
		if b == nil {
			break
		}
		e.pickRev++
		if !e.requireAll {
			b.Mode, b.Picks = hunkpick.TakeCurrent, nil
			e.stepBlock(+1)
			break
		}
		if b.Skipped() {
			b.Unskip()
			break
		}
		b.Skip()
		if !e.focusNextUndecided() {
			m.statusMsg = i18n.T("all regions resolved — [ctrl+s] apply")
		}
	case "c":
		if b != nil {
			b.ToggleSide(hunkpick.Current)
			e.pickRev++
		}
	case "i":
		if b != nil {
			b.ToggleSide(hunkpick.Incoming)
			e.pickRev++
		}
	case "C":
		e.doc.ToggleSideAll(hunkpick.Current)
		e.pickRev++
	case "I":
		e.doc.ToggleSideAll(hunkpick.Incoming)
		e.pickRev++
	case " ":
		if b != nil && e.sideLen() > 0 {
			b.EnsurePicks()
			b.ToggleLine(e.side, e.line)
			e.pickRev++
		}
	case "enter":
		// Enter walks, never applies (ctrl+s does): the conflict picker jumps
		// to the next region still undecided, the staging pickers to the next
		// hunk — both wrap from the last to the first.
		if e.requireAll {
			if !e.focusNextUndecided() {
				m.statusMsg = i18n.T("all regions resolved — [ctrl+s] apply")
				return m, nil
			}
		} else {
			e.stepBlock(+1)
		}
		// The cursor moved (and with it the pane's follow-anchor): hand the
		// arrows back to the grid so the user lands on the region.
		e.outFocused, e.oshift = false, 0
	case "ctrl+s":
		if e.requireAll {
			if n := e.doc.Pending(); n > 0 {
				m.statusMsg = i18n.T("%d region(s) left to resolve", n)
				e.focusFirstUndecided()
				e.outFocused, e.oshift = false, 0
				return m, nil
			}
		}
		out, ok := e.doc.Resolved()
		if !ok {
			m.statusMsg = i18n.T("internal error: undecided regions")
			return m, nil
		}
		return e.apply(m, out)
	}
	return m, nil
}

// pickerCell builds the winCell for one candidate line; r past the side's line
// count yields a blank cell (the gap when sides differ in length). cursor adds
// the "> " marker so the gutter width is constant (focused or not) and puts the
// cell under selectedRow — reverse video, which renderPiece takes as the signal
// to drop the CLASS half of the paint mask; search hits survive it, because the
// current hit is always on the cursor row. hits are the in-view search's spans
// for this line, already display-rune offsets into san[r].text.
func pickerCell(blk *hunkpick.Block, san []sanLine, side hunkpick.Side, r int, cursor bool, hits []hitSpan) *winCell {
	if r >= len(san) {
		return &winCell{}
	}
	cur := "  "
	if cursor {
		cur = "> "
	}
	tick := "[ ] "
	if blk.LinePicked(side, r) {
		tick = "[x] "
	}
	c := &winCell{gutter: cur + tick, body: san[r].text, mask: san[r].mask}
	if len(hits) > 0 {
		// NEVER write into the cached mask: ensureOutput hands the very same
		// sanLine to the output pane. overlayHits paints a copy.
		c.mask = runMask{
			cls:  san[r].mask.cls,
			emph: overlayHits(san[r].mask.emph, 0, len([]rune(san[r].text)), hits),
		}
	}
	if cursor {
		c.style = st().selectedRow
	}
	return c
}

func (e *hunkPicker) render(m Model, _ string) string {
	w, H := m.overlayDims()

	header := e.title + "    " + i18n.T("%d hunks", len(e.blocks))
	if e.requireAll {
		header = e.title + "    " + i18n.T("%d regions · %d left", len(e.blocks), e.doc.Pending())
	}
	if bd := e.search.badge(); bd != "" { // the in-view search, after the counts
		header += "    " + bd
	}

	// Column header: which physical column is left/right, with the active side
	// (the one the cursor edits) marked and highlighted.
	colLabels := e.columnLabels(w)

	// The hint wraps instead of truncating so no command is ever cut off;
	// the set follows the focus so only live keys are advertised.
	enterHint := i18n.T("[enter] next hunk")
	if e.requireAll {
		enterHint = i18n.T("[enter] next unresolved")
	}
	hintParts := []string{
		i18n.T("[←/→] side"), i18n.T("[shift+←/→] scroll"), i18n.T("[ctrl+w] mode"), i18n.T("[↑/↓] line"), i18n.T("[/] find"), i18n.T("[pgup/pgdn] page"), i18n.T("[alt+↑/↓] view"), i18n.T("[space] pick"),
		"[c] " + e.leftLabel, "[i] " + e.rightLabel, i18n.T("[C/I] all"), i18n.T("[s] skip"), i18n.T("[n] next hunk"), i18n.T("[p] prev hunk"), i18n.T("[o] output"), i18n.T("[tab] output"),
		i18n.T("[ctrl+t] full"), enterHint, i18n.T("[ctrl+s] apply"), i18n.T("[esc] cancel"),
	}
	if e.outFocused {
		hintParts = []string{
			i18n.T("[↑/↓] scroll"), i18n.T("[pgup/pgdn] page"), i18n.T("[tab] grid"), i18n.T("[o] hide"), i18n.T("[ctrl+w] mode"), i18n.T("[shift+←/→] scroll"), i18n.T("[alt+↑/↓] view"),
			i18n.T("[ctrl+t] full"), enterHint, i18n.T("[ctrl+s] apply"), i18n.T("[esc] cancel"),
		}
	}
	hintLines := wrapParts(hintParts, w, "  ")

	// header, column labels, blank, hint(N).
	bodyH := H - 3 - len(hintLines)
	if bodyH < 1 {
		bodyH = 1
	}
	// Output-zoom: the rule replaces the column-labels row and the pane gets
	// the full body; no grid rows are built at all.
	if e.zoomed && e.outFocused {
		lines := []string{header, e.outputRule(w)}
		lines = append(lines, e.renderOutput(w, bodyH)...)
		lines = append(lines, "")
		lines = append(lines, hintLines...)
		return strings.Join(lines, "\n")
	}

	gridH, outH := bodyH, 0
	if !e.outCollapsed && !e.zoomed {
		outH = bodyH / 3
		if outH < 3 {
			outH = 3
		}
		if outH > bodyH-4 {
			outH = bodyH - 4 // keep ≥3 grid lines + the rule
		}
		if outH < 3 {
			outH = 0 // can't meet the 3-line minimum: hide rather than degrade
		} else {
			gridH = bodyH - outH - 1
		}
	}

	e.ensureSan()
	dim := st().dim
	var rows []colRow
	anchor := 0
	blockNo := 0
	for ii, it := range e.doc.Items {
		if it.Block == nil {
			for _, l := range e.sanLit[ii] {
				// The two-space indent is part of the body (it scrolls with
				// the text), so the mask gains two leading plain runes.
				rows = append(rows, colRow{full: &winCell{body: "  " + l.text, style: dim, mask: l.mask.pad(2)}})
			}
			continue
		}
		blk := it.Block
		focused := blockNo == e.bi
		marker, hstyle := "  ", dim
		if focused {
			marker, hstyle = "▶ ", pickerFocus
		}
		lAll, lAny := blk.SideState(hunkpick.Current)
		rAll, rAny := blk.SideState(hunkpick.Incoming)
		rows = append(rows, colRow{
			left: &winCell{gutter: marker, style: hstyle,
				body: tickFor(lAll, lAny) + " " + e.leftLabel + " · " + i18n.T("region %d/%d", blockNo+1, len(e.blocks))},
			right: &winCell{gutter: "  ", style: hstyle,
				body: tickFor(rAll, rAny) + " " + e.rightLabel + e.stateSuffix(blk)},
		})
		n := len(blk.Current)
		if len(blk.Incoming) > n {
			n = len(blk.Incoming)
		}
		for r := 0; r < n; r++ {
			lCur := focused && e.side == hunkpick.Current && e.line == r
			rCur := focused && e.side == hunkpick.Incoming && e.line == r
			if lCur || rCur {
				anchor = len(rows)
			}
			var lh, rh []hitSpan
			if e.search.active() {
				lh = e.search.hitsOn(e.searchRow(blockNo, hunkpick.Current, r), 0)
				rh = e.search.hitsOn(e.searchRow(blockNo, hunkpick.Incoming, r), 1)
			}
			rows = append(rows, colRow{
				left:  pickerCell(blk, e.sanCur[blockNo], hunkpick.Current, r, lCur, lh),
				right: pickerCell(blk, e.sanInc[blockNo], hunkpick.Incoming, r, rCur, rh),
			})
		}
		blockNo++
	}

	e.lastGridH = gridH
	body, eff := renderTwoCol(rows, twoColOpts{
		w: w, h: gridH, sep: pickerColSep, mode: e.mode, hscroll: e.hscroll, anchor: anchor, vshift: e.vshift,
	})
	e.vshift = eff

	lines := []string{header, colLabels}
	lines = append(lines, body...)
	if outH > 0 {
		lines = append(lines, e.outputRule(w))
		lines = append(lines, e.renderOutput(w, outH)...)
	}
	lines = append(lines, "")
	lines = append(lines, hintLines...)
	return strings.Join(lines, "\n")
}

// columnLabels builds the two-column header row labelling the left/right sides,
// aligned to the same column split renderTwoCol uses. The active side (the one
// the cursor edits) is highlighted and gets a ▶ marker.
func (e *hunkPicker) columnLabels(w int) string {
	colW := (w - lipgloss.Width(pickerColSep)) / 2
	if colW < 1 {
		colW = 1
	}
	sty := st()
	cell := func(label string, s hunkpick.Side) string {
		marker, style := "  ", sty.pickerLabel
		if e.side == s {
			marker, style = "▶ ", sty.selectedRow
		}
		return styleCell(style, marker+tickFor(e.doc.SideStateAll(s))+" "+label, colW)
	}
	return cell(e.leftLabel, hunkpick.Current) +
		pickerColSep + cell(e.rightLabel, hunkpick.Incoming)
}

// outputLines assembles the live result — literals verbatim, each region's
// picked lines, a placeholder for an undecided region — and returns the index
// of the focused region's first line so the pane can follow the cursor.
func (e *hunkPicker) outputLines() ([]sanLine, int) {
	e.ensureOutput()
	anchor := 0
	if e.bi >= 0 && e.bi < len(e.outStart) {
		anchor = e.outStart[e.bi]
	}
	return e.outLines, anchor
}

// renderOutput windows the assembled result to h display lines of width w,
// keeping the focused region's first line in view; the picker's display mode
// applies per line (wrap expands, scroll pans with the shared hscroll). The
// lines arrive pre-sanitized (with their paint masks) from the output cache;
// outside wrap mode the window is computed first and only the h visible lines
// are laid out and painted.
func (e *hunkPicker) renderOutput(w, h int) []string {
	e.lastOutH = h
	src, srcAnchor := e.outputLines()
	if e.mode != modeWrap {
		// 1 line : 1 display line — window in line space, transform the window.
		anchor := srcAnchor
		if srcAnchor >= len(src) {
			anchor = len(src) // focused region is empty at EOF: pin to the end
		}
		start := windowStart(len(src), h, anchor)
		if e.oshift != 0 {
			maxStart := len(src) - h
			if maxStart < 0 {
				maxStart = 0
			}
			sh := start + e.oshift
			if sh > maxStart {
				sh = maxStart
			}
			if sh < 0 {
				sh = 0
			}
			e.oshift = sh - start
			start = sh
		}
		out := make([]string, 0, h)
		for i := 0; i < h; i++ {
			idx := start + i
			if idx >= len(src) {
				out = append(out, padRight("", w))
				continue
			}
			p := cellPieces(&winCell{body: src[idx].text, mask: src[idx].mask}, w, e.mode, e.hscroll)[0]
			out = append(out, renderPiece(lipgloss.Style{}, p, w))
		}
		return out
	}
	// Wrap expands lines unevenly, so the whole document is laid out before
	// windowing (the sanitize cost is already cached away).
	var dl []cellPiece
	anchor := 0
	for i, l := range src {
		if i == srcAnchor {
			anchor = len(dl)
		}
		dl = append(dl, cellPieces(&winCell{body: l.text, mask: l.mask}, w, modeWrap, e.hscroll)...)
	}
	if srcAnchor >= len(src) {
		anchor = len(dl)
	}
	start := windowStart(len(dl), h, anchor)
	if e.oshift != 0 {
		maxStart := len(dl) - h
		if maxStart < 0 {
			maxStart = 0
		}
		sh := start + e.oshift
		if sh > maxStart {
			sh = maxStart
		}
		if sh < 0 {
			sh = 0
		}
		e.oshift = sh - start
		start = sh
	}
	out := make([]string, 0, h)
	for i := 0; i < h; i++ {
		if idx := start + i; idx < len(dl) {
			out = append(out, renderPiece(lipgloss.Style{}, dl[idx], w))
		} else {
			out = append(out, padRight("", w))
		}
	}
	return out
}

// outputRule is the pane's titled separator line; the title carries the
// focus marker while the pane owns the arrows.
func (e *hunkPicker) outputRule(w int) string {
	label, style := "── "+i18n.T("output")+" ", st().dim
	if e.outFocused {
		label, style = "── ▶ "+i18n.T("output")+" ", pickerFocus
	}
	fill := w - lipgloss.Width(label)
	if fill < 0 {
		fill = 0
	}
	return style.Render(padRight(label+strings.Repeat("─", fill), w))
}
