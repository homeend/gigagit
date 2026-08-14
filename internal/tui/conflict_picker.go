package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/homeend/gigagit/internal/engine"
	"github.com/homeend/gigagit/internal/hunkpick"
	"github.com/homeend/gigagit/internal/i18n"
)

var (
	pickerDim   = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	pickerFocus = lipgloss.NewStyle().Bold(true)
	pickerLabel = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("245"))
)

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
	sanLit   [][]string // per doc.Items index: sanitized literal lines (nil for blocks)
	sanCur   [][]string // per block index: sanitized current-side lines
	sanInc   [][]string // per block index: sanitized incoming-side lines
	pickRev  int        // bumped on every pick mutation
	outBuilt bool
	outRev   int      // pickRev the output cache was built at
	outLines []string // sanitized assembled output
	outStart []int    // per block index: first output line of the block's contribution
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
		doc: doc, blocks: doc.Blocks(), side: hunkpick.Current, mode: modeScroll,
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
		doc: doc, blocks: doc.Blocks(), side: hunkpick.Current, mode: modeScroll,
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
		doc: doc, blocks: doc.Blocks(), side: hunkpick.Current, mode: modeScroll,
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
	if b.Mode == hunkpick.LineByLine && len(b.Picks) == 0 {
		return " — " + i18n.T("empty")
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

func (e *hunkPicker) focusFirstUndecided() {
	for i, b := range e.blocks {
		if b.Mode == hunkpick.Undecided {
			e.bi, e.line, e.side = i, 0, hunkpick.Current
			return
		}
	}
}

// ensureSan builds the once-per-picker sanitized copies of the doc's lines
// (display only — resolution reads the doc's raw lines and keeps CRLF).
func (e *hunkPicker) ensureSan() {
	if e.sanBuilt {
		return
	}
	e.sanBuilt = true
	e.sanLit = make([][]string, len(e.doc.Items))
	e.sanCur = make([][]string, len(e.blocks))
	e.sanInc = make([][]string, len(e.blocks))
	bi := 0
	for i, it := range e.doc.Items {
		if it.Block == nil {
			ls := make([]string, len(it.Literal))
			for k, l := range it.Literal {
				ls[k] = sanitizeLine(l)
			}
			e.sanLit[i] = ls
			continue
		}
		cur := make([]string, len(it.Block.Current))
		for k, l := range it.Block.Current {
			cur[k] = sanitizeLine(l)
		}
		inc := make([]string, len(it.Block.Incoming))
		for k, l := range it.Block.Incoming {
			inc[k] = sanitizeLine(l)
		}
		e.sanCur[bi], e.sanInc[bi] = cur, inc
		bi++
	}
}

// ensureOutput (re)assembles the sanitized output lines and each block's
// start offset — only when the picks changed since the last build.
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
		if ls, ok := it.Block.ResolvedLines(); ok {
			for _, l := range ls {
				e.outLines = append(e.outLines, sanitizeLine(l))
			}
		} else {
			e.outLines = append(e.outLines, i18n.T("‹region %d undecided›", bi+1))
		}
		bi++
	}
}

func (e *hunkPicker) update(m Model, msg tea.KeyMsg) (Model, tea.Cmd) {
	if msg.Type == tea.KeyCtrlC {
		return m, tea.Quit
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
		case "o":
			e.outCollapsed, e.outFocused, e.oshift = true, false, 0
			e.zoomed = false
			return m, nil
		case "esc", "enter", "z", "shift+left", "shift+right", "alt+up", "alt+down":
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
		case "up", "k", "down", "j":
			return m, nil
		}
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
	case "z":
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
		if e.line > 0 {
			e.line--
		} else if e.bi > 0 {
			e.bi--
			e.line = e.sideLen() - 1
			if e.line < 0 {
				e.line = 0
			}
		}
	case "down", "j":
		if e.line < e.sideLen()-1 {
			e.line++
		} else if e.bi < len(e.blocks)-1 {
			e.bi++
			e.line = 0
		}
	case "n":
		if e.bi < len(e.blocks)-1 {
			e.bi++
			e.line = 0
		}
	case "p":
		if e.bi > 0 {
			e.bi--
			e.line = 0
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
		if e.requireAll {
			if n := e.doc.Pending(); n > 0 {
				m.statusMsg = i18n.T("%d region(s) left to resolve", n)
				e.focusFirstUndecided()
				// The gate moved the grid cursor (and with it the pane's
				// follow-anchor): hand the arrows back to the grid so the
				// user lands on the region they must resolve.
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
// the "> " marker so the gutter width is constant (focused or not).
func pickerCell(blk *hunkpick.Block, san []string, side hunkpick.Side, r int, cursor bool) *winCell {
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
	c := &winCell{gutter: cur + tick, body: san[r]}
	if cursor {
		c.style = selectedRow
	}
	return c
}

func (e *hunkPicker) render(m Model, _ string) string {
	w, H := m.overlayDims()

	header := e.title + "    " + i18n.T("%d hunks", len(e.blocks))
	if e.requireAll {
		header = e.title + "    " + i18n.T("%d regions · %d left", len(e.blocks), e.doc.Pending())
	}

	// Column header: which physical column is left/right, with the active side
	// (the one the cursor edits) marked and highlighted.
	colLabels := e.columnLabels(w)

	// The hint wraps instead of truncating so no command is ever cut off;
	// the set follows the focus so only live keys are advertised.
	hintParts := []string{
		i18n.T("[←/→] side"), i18n.T("[shift+←/→] scroll"), i18n.T("[z] mode"), i18n.T("[↑/↓] line"), i18n.T("[alt+↑/↓] view"), i18n.T("[space] pick"),
		"[c] " + e.leftLabel, "[i] " + e.rightLabel, i18n.T("[C/I] all"), i18n.T("[n/p] hunk"), i18n.T("[o] output"), i18n.T("[tab] output"),
		i18n.T("[ctrl+t] full"), i18n.T("[enter] apply"), i18n.T("[esc] cancel"),
	}
	if e.outFocused {
		hintParts = []string{
			i18n.T("[↑/↓] scroll"), i18n.T("[tab] grid"), i18n.T("[o] hide"), i18n.T("[z] mode"), i18n.T("[shift+←/→] scroll"), i18n.T("[alt+↑/↓] view"),
			i18n.T("[ctrl+t] full"), i18n.T("[enter] apply"), i18n.T("[esc] cancel"),
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
	var rows []colRow
	anchor := 0
	blockNo := 0
	for ii, it := range e.doc.Items {
		if it.Block == nil {
			for _, l := range e.sanLit[ii] {
				rows = append(rows, colRow{full: &winCell{body: "  " + l, style: pickerDim}})
			}
			continue
		}
		blk := it.Block
		focused := blockNo == e.bi
		marker, hstyle := "  ", pickerDim
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
			rows = append(rows, colRow{
				left:  pickerCell(blk, e.sanCur[blockNo], hunkpick.Current, r, lCur),
				right: pickerCell(blk, e.sanInc[blockNo], hunkpick.Incoming, r, rCur),
			})
		}
		blockNo++
	}

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
	cell := func(label string, s hunkpick.Side) string {
		marker, style := "  ", pickerLabel
		if e.side == s {
			marker, style = "▶ ", selectedRow
		}
		return styleCell(style, marker+tickFor(e.doc.SideStateAll(s))+" "+label, colW)
	}
	return cell(e.leftLabel, hunkpick.Current) +
		pickerColSep + cell(e.rightLabel, hunkpick.Incoming)
}

// outputLines assembles the live result — literals verbatim, each region's
// picked lines, a placeholder for an undecided region — and returns the index
// of the focused region's first line so the pane can follow the cursor.
func (e *hunkPicker) outputLines() ([]string, int) {
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
// lines arrive pre-sanitized from the output cache; outside wrap mode the
// window is computed first and only the h visible lines are transformed.
func (e *hunkPicker) renderOutput(w, h int) []string {
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
			l := src[idx]
			if e.mode == modeScroll {
				l = hslice(l, e.hscroll, w)
			} else {
				l = truncate(l, w)
			}
			out = append(out, padRight(l, w))
		}
		return out
	}
	// Wrap expands lines unevenly, so the whole document is laid out before
	// windowing (the sanitize cost is already cached away).
	var dl []string
	anchor := 0
	for i, l := range src {
		if i == srcAnchor {
			anchor = len(dl)
		}
		ws := wrapWidth(l, w, 1<<20)
		if len(ws) == 0 {
			ws = []string{""}
		}
		dl = append(dl, ws...)
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
			out = append(out, padRight(dl[idx], w))
		} else {
			out = append(out, padRight("", w))
		}
	}
	return out
}

// outputRule is the pane's titled separator line; the title carries the
// focus marker while the pane owns the arrows.
func (e *hunkPicker) outputRule(w int) string {
	label, style := "── "+i18n.T("output")+" ", pickerDim
	if e.outFocused {
		label, style = "── ▶ "+i18n.T("output")+" ", pickerFocus
	}
	fill := w - lipgloss.Width(label)
	if fill < 0 {
		fill = 0
	}
	return style.Render(padRight(label+strings.Repeat("─", fill), w))
}
