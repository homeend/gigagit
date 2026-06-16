package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/gigagit/gg/internal/engine"
	"github.com/gigagit/gg/internal/hunkpick"
)

var (
	pickerDim   = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	pickerFocus = lipgloss.NewStyle().Bold(true)
)

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
}

// newConflictPicker wires the conflict-resolution params.
func newConflictPicker(path string, doc *hunkpick.Doc) *hunkPicker {
	return &hunkPicker{
		title:      "Resolve conflicts: " + path,
		leftLabel:  "current",
		rightLabel: "incoming",
		requireAll: true,
		apply: func(m Model, content []byte) (Model, tea.Cmd) {
			m = m.popSurface()
			m.conflictPopup = nil
			m.reopenConflict = true
			return m.startOp(engine.ResolveConflictHunks{Path: path, Content: content})
		},
		doc: doc, blocks: doc.Blocks(), side: hunkpick.Current,
	}
}

// newStagePicker wires the hunk-staging params.
func newStagePicker(path string, doc *hunkpick.Doc) *hunkPicker {
	return &hunkPicker{
		title:      "Stage hunks: " + path,
		leftLabel:  "index",
		rightLabel: "working",
		requireAll: false,
		apply: func(m Model, content []byte) (Model, tea.Cmd) {
			m = m.popSurface()
			return m.startOp(engine.StageHunks{Path: path, Content: content})
		},
		doc: doc, blocks: doc.Blocks(), side: hunkpick.Current,
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
		return m.popSurface(), nil
	case "left":
		e.side = hunkpick.Current
		e.clampLine()
	case "right":
		e.side = hunkpick.Incoming
		e.clampLine()
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
				m.statusMsg = fmt.Sprintf("%d region(s) left to resolve", n)
				e.focusFirstUndecided()
				return m, nil
			}
		}
		out, ok := e.doc.Resolved()
		if !ok {
			m.statusMsg = "internal error: undecided regions"
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
		return "line-by-line"
	default:
		return "· undecided"
	}
}

func (e *hunkPicker) render(m Model) string {
	w := m.width
	if w <= 0 {
		w = 80
	}
	var b strings.Builder
	if e.requireAll {
		fmt.Fprintf(&b, "%s    %d regions · %d left\n", e.title, len(e.blocks), e.doc.Pending())
	} else {
		fmt.Fprintf(&b, "%s    %d hunks\n", e.title, len(e.blocks))
	}
	b.WriteString(strings.Repeat("─", min(w, 60)) + "\n")

	colW := (w - 5) / 2
	if colW < 8 {
		colW = 8
	}
	blockNo := 0
	for _, it := range e.doc.Items {
		if it.Block == nil {
			for _, l := range it.Literal {
				b.WriteString(pickerDim.Render("  " + truncate(l, w-2)))
				b.WriteString("\n")
			}
			continue
		}
		blk := it.Block
		focused := blockNo == e.bi
		marker := "  "
		if focused {
			marker = "▶ "
		}
		header := fmt.Sprintf("%shunk %d/%d — %s", marker, blockNo+1, len(e.blocks), e.badge(blk))
		if focused {
			b.WriteString(pickerFocus.Render(header))
		} else {
			b.WriteString(pickerDim.Render(header))
		}
		b.WriteString("\n")
		rows := len(blk.Current)
		if len(blk.Incoming) > rows {
			rows = len(blk.Incoming)
		}
		for r := 0; r < rows; r++ {
			left := cell(blk, hunkpick.Current, r, focused && e.side == hunkpick.Current && e.line == r, colW)
			right := cell(blk, hunkpick.Incoming, r, focused && e.side == hunkpick.Incoming && e.line == r, colW)
			b.WriteString(left + " ║ " + right + "\n")
		}
		if blk.Mode == hunkpick.LineByLine {
			b.WriteString(pickerDim.Render("  result:") + "\n")
			tmp := &hunkpick.Doc{Items: []hunkpick.Item{{Block: blk}}}
			if out, ok := tmp.Resolved(); ok {
				for _, l := range strings.Split(strings.TrimRight(string(out), "\n"), "\n") {
					b.WriteString("    " + truncate(l, w-4) + "\n")
				}
			}
		}
		blockNo++
	}
	fmt.Fprintf(&b, "\n[←/→] side  [↑/↓] line  [space] pick line  [c] %s  [i] %s  [C/I] all  [n/p] hunk  [enter] apply  [esc] cancel",
		e.leftLabel, e.rightLabel)
	return b.String()
}

// cell renders one candidate line with an optional checkbox (line-by-line) and
// cursor highlight.
func cell(blk *hunkpick.Block, side hunkpick.Side, r int, cursor bool, w int) string {
	var lines []string
	if side == hunkpick.Current {
		lines = blk.Current
	} else {
		lines = blk.Incoming
	}
	text := ""
	if r < len(lines) {
		text = lines[r]
	}
	box := ""
	if blk.Mode == hunkpick.LineByLine && r < len(lines) {
		if blk.Picked(side, r) {
			box = "[x] "
		} else {
			box = "[ ] "
		}
	}
	body := truncate(box+text, w)
	if cursor {
		return selectedRow.Render(padRight("> "+body, w+2))
	}
	if r >= len(lines) {
		return padRight("", w+2)
	}
	return padRight("  "+body, w+2)
}
