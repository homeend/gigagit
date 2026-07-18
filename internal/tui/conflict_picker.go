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

func (e *hunkPicker) focusFirstUndecided() {
	for i, b := range e.blocks {
		if b.Mode == hunkpick.Undecided {
			e.bi, e.line, e.side = i, 0, hunkpick.Current
			return
		}
	}
}

func (e *hunkPicker) update(m Model, msg tea.KeyMsg) (Model, tea.Cmd) {
	if msg.Type == tea.KeyCtrlC {
		return m, tea.Quit
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
			b.Mode = hunkpick.TakeCurrent
		}
	case "i":
		if b != nil {
			b.Mode = hunkpick.TakeIncoming
		}
	case "C":
		e.doc.SetAll(hunkpick.TakeCurrent)
	case "I":
		e.doc.SetAll(hunkpick.TakeIncoming)
	case " ":
		if b != nil && e.sideLen() > 0 {
			if b.Mode != hunkpick.LineByLine {
				b.Mode = hunkpick.LineByLine
				b.Picks = nil
			}
			b.ToggleLine(e.side, e.line)
		}
	case "enter":
		if e.requireAll {
			if n := e.doc.Pending(); n > 0 {
				m.statusMsg = i18n.T("%d region(s) left to resolve", n)
				e.focusFirstUndecided()
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

// badge labels a block's current decision using the picker's side labels.
func (e *hunkPicker) badge(b *hunkpick.Block) string {
	switch b.Mode {
	case hunkpick.TakeCurrent:
		return "✓ " + e.leftLabel
	case hunkpick.TakeIncoming:
		return "✓ " + e.rightLabel
	case hunkpick.LineByLine:
		return i18n.T("line-by-line")
	default:
		return i18n.T("· undecided")
	}
}

// pickerCell builds the winCell for one candidate line; r past the side's line
// count yields a blank cell (the gap when sides differ in length). cursor adds
// the "> " marker so the gutter width is constant (focused or not).
func pickerCell(blk *hunkpick.Block, side hunkpick.Side, r int, cursor bool) *winCell {
	var lines []string
	if side == hunkpick.Current {
		lines = blk.Current
	} else {
		lines = blk.Incoming
	}
	if r >= len(lines) {
		return &winCell{}
	}
	cur := "  "
	if cursor {
		cur = "> "
	}
	tick := ""
	if blk.Mode == hunkpick.LineByLine {
		if blk.Picked(side, r) {
			tick = "[x] "
		} else {
			tick = "[ ] "
		}
	}
	c := &winCell{gutter: cur + tick, body: lines[r]}
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

	// The hint wraps instead of truncating so no command is ever cut off.
	hintLines := wrapParts([]string{
		i18n.T("[←/→] side"), i18n.T("[shift+←/→] scroll"), i18n.T("[z] mode"), i18n.T("[↑/↓] line"), i18n.T("[space] pick"),
		"[c] " + e.leftLabel, "[i] " + e.rightLabel, i18n.T("[C/I] all"), i18n.T("[n/p] hunk"),
		i18n.T("[enter] apply"), i18n.T("[esc] cancel"),
	}, w, "  ")

	// header, column labels, blank, hint(N).
	bodyH := H - 3 - len(hintLines)
	if bodyH < 1 {
		bodyH = 1
	}

	var rows []colRow
	anchor := 0
	blockNo := 0
	for _, it := range e.doc.Items {
		if it.Block == nil {
			for _, l := range it.Literal {
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
		rows = append(rows, colRow{full: &winCell{
			body:  marker + i18n.T("hunk %d/%d — %s", blockNo+1, len(e.blocks), e.badge(blk)),
			style: hstyle,
		}})
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
				left:  pickerCell(blk, hunkpick.Current, r, lCur),
				right: pickerCell(blk, hunkpick.Incoming, r, rCur),
			})
		}
		if blk.Mode == hunkpick.LineByLine {
			rows = append(rows, colRow{full: &winCell{body: "  " + i18n.T("result:"), style: pickerDim}})
			tmp := &hunkpick.Doc{Items: []hunkpick.Item{{Block: blk}}}
			if out, ok := tmp.Resolved(); ok {
				for _, l := range strings.Split(strings.TrimRight(string(out), "\n"), "\n") {
					rows = append(rows, colRow{full: &winCell{body: "    " + l, style: pickerDim}})
				}
			}
		}
		blockNo++
	}

	body := renderTwoCol(rows, twoColOpts{
		w: w, h: bodyH, sep: pickerColSep, mode: e.mode, hscroll: e.hscroll, anchor: anchor,
	})

	lines := []string{header, colLabels}
	lines = append(lines, body...)
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
	cell := func(label string, active bool) string {
		marker, style := "  ", pickerLabel
		if active {
			marker, style = "▶ ", selectedRow
		}
		return styleCell(style, marker+label, colW)
	}
	return cell(e.leftLabel, e.side == hunkpick.Current) +
		pickerColSep + cell(e.rightLabel, e.side == hunkpick.Incoming)
}
