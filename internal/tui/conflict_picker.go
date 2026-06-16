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

// conflictPicker is the region/line-level conflict resolver surface. Rows show
// the file top-to-bottom; conflict regions render side-by-side. A 2D cursor
// (block index, side, line) drives the picks.
type conflictPicker struct {
	path   string
	doc    *hunkpick.Doc
	blocks []*hunkpick.Block
	bi     int           // focused block index into blocks
	side   hunkpick.Side // focused column
	line   int           // cursor line within the focused side
}

func newConflictPicker(path string, doc *hunkpick.Doc) *conflictPicker {
	return &conflictPicker{path: path, doc: doc, blocks: doc.Blocks(), side: hunkpick.Current}
}

// cur returns the focused block, or nil when there are none.
func (e *conflictPicker) cur() *hunkpick.Block {
	if e.bi < 0 || e.bi >= len(e.blocks) {
		return nil
	}
	return e.blocks[e.bi]
}

// sideLen is the number of lines on the focused side of the focused block.
func (e *conflictPicker) sideLen() int {
	b := e.cur()
	if b == nil {
		return 0
	}
	if e.side == hunkpick.Incoming {
		return len(b.Incoming)
	}
	return len(b.Current)
}

// clampLine keeps the cursor within the focused side's line count.
func (e *conflictPicker) clampLine() {
	n := e.sideLen()
	if e.line >= n {
		e.line = n - 1
	}
	if e.line < 0 {
		e.line = 0
	}
}

func (e *conflictPicker) focusFirstUndecided() {
	for i, b := range e.blocks {
		if b.Mode == hunkpick.Undecided {
			e.bi, e.line, e.side = i, 0, hunkpick.Current
			return
		}
	}
}

func (e *conflictPicker) update(m Model, msg tea.KeyMsg) (Model, tea.Cmd) {
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
		if n := e.doc.Pending(); n > 0 {
			m.statusMsg = fmt.Sprintf("%d region(s) left to resolve", n)
			e.focusFirstUndecided()
			return m, nil
		}
		out, ok := e.doc.Resolved()
		if !ok {
			m.statusMsg = "internal error: unresolved regions"
			return m, nil
		}
		m = m.popSurface()
		m.conflictPopup = nil
		m.reopenConflict = true
		return m.startOp(engine.ResolveConflictHunks{Path: e.path, Content: out})
	}
	return m, nil
}

// badge labels a block's current decision.
func badge(b *hunkpick.Block) string {
	switch b.Mode {
	case hunkpick.TakeCurrent:
		return "✓ current"
	case hunkpick.TakeIncoming:
		return "✓ incoming"
	case hunkpick.LineByLine:
		return "line-by-line"
	default:
		return "· undecided"
	}
}

func (e *conflictPicker) render(m Model) string {
	w := m.width
	if w <= 0 {
		w = 80
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Resolve conflicts: %s    %d regions · %d left\n",
		e.path, len(e.blocks), e.doc.Pending())
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
		header := fmt.Sprintf("%sregion %d/%d — %s", marker, blockNo+1, len(e.blocks), badge(blk))
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
	b.WriteString("\n[←/→] side  [↑/↓] line  [space] pick line  [c]urrent [i]ncoming  [C/I] all  [n/p] region  [enter] apply  [esc] cancel")
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
