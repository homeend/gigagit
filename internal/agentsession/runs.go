package agentsession

import (
	"fmt"
	"image/color"

	uv "github.com/charmbracelet/ultraviolet"
)

// Run is a stretch of equally styled cells in one row. Only attributes that
// differ from the default are set, so the wire carries nothing for plain
// text. Colours are "#rrggbb" (palette indices resolved), "" = default.
type Run struct {
	Text      string `json:"t"`
	Fg        string `json:"fg,omitempty"`
	Bg        string `json:"bg,omitempty"`
	Bold      bool   `json:"b,omitempty"`
	Italic    bool   `json:"i,omitempty"`
	Underline bool   `json:"u,omitempty"`
	Reverse   bool   `json:"r,omitempty"`
	Dim       bool   `json:"d,omitempty"`
	Strike    bool   `json:"s,omitempty"`
	// W is set on a run that is ONE wide glyph: the cells it fills (2). The
	// page boxes it at that width — a browser draws a fallback font's CJK or
	// emoji glyph at whatever width that font has.
	W int `json:"w,omitempty"`
}

// RunRow is one screen row. An empty Runs is a blank row.
type RunRow struct {
	Runs []Run `json:"runs"`
}

// ScreenRuns is the visible grid as styled runs — the web console's frame.
// The cursor is NOT painted into the runs (the page draws it), so one
// snapshot serves focused and unfocused viewers alike.
type ScreenRuns struct {
	Lines            []RunRow
	Cols, Rows       int
	CursorX, CursorY int
	CursorVisible    bool
	AltScreen        bool
}

// ScreenRuns snapshots the grid under the session lock (CellAt hands out a
// live pointer). Adjacent cells of equal style merge into one run; a wide
// glyph's right-half cell (zero, after a Width>1 cell) is skipped; trailing
// default-styled blanks are dropped, so a blank row has no runs.
func (s *Session) ScreenRuns() ScreenRuns {
	s.ioMu.Lock()
	defer s.ioMu.Unlock()
	w, h := s.emu.Width(), s.emu.Height()
	out := ScreenRuns{Lines: make([]RunRow, h), Cols: w, Rows: h, AltScreen: s.emu.IsAltScreen()}
	pos := s.emu.CursorPosition()
	out.CursorX, out.CursorY, out.CursorVisible = pos.X, pos.Y, !s.cursorHidden.Load()
	for y := range h {
		runs := []Run{}
		var curStyle uv.Style
		skip := 0
		for x := range w {
			if skip > 0 {
				skip--
				continue
			}
			c := s.emu.CellAt(x, y)
			text, style, wide := " ", uv.Style{}, 0
			if c != nil && !c.IsZero() {
				text, style = c.Content, c.Style
				if c.Width > 1 {
					skip, wide = c.Width-1, c.Width
				}
			}
			// A wide glyph never merges, and nothing merges into it.
			if n := len(runs); n > 0 && wide == 0 && runs[n-1].W == 0 && curStyle.Equal(&style) {
				runs[n-1].Text += text
				continue
			}
			r := runFor(text, style)
			r.W = wide
			runs = append(runs, r)
			curStyle = style
		}
		out.Lines[y] = RunRow{Runs: trimRow(runs)}
	}
	return out
}

// trimRow drops trailing default-styled blanks: the last plain run loses its
// trailing spaces, and a run left empty (or a row of nothing) goes.
func trimRow(runs []Run) []Run {
	for n := len(runs); n > 0; n = len(runs) {
		last := &runs[n-1]
		if *last != (Run{Text: last.Text}) {
			break // styled: keep even when blank (a background colour shows)
		}
		last.Text = trimRight(last.Text)
		if last.Text != "" {
			break
		}
		runs = runs[:n-1]
	}
	return runs
}

func trimRight(s string) string {
	i := len(s)
	for i > 0 && s[i-1] == ' ' {
		i--
	}
	return s[:i]
}

func runFor(text string, st uv.Style) Run {
	return Run{
		Text: text, Fg: hexColor(st.Fg), Bg: hexColor(st.Bg),
		Bold: st.Attrs&uv.AttrBold != 0, Italic: st.Attrs&uv.AttrItalic != 0,
		Underline: st.Underline != uv.UnderlineNone, Reverse: st.Attrs&uv.AttrReverse != 0,
		Dim: st.Attrs&uv.AttrFaint != 0, Strike: st.Attrs&uv.AttrStrikethrough != 0,
	}
}

// hexColor renders any colour (basic, indexed, true) as "#rrggbb"; nil is
// the default and renders as "".
func hexColor(c color.Color) string {
	if c == nil {
		return ""
	}
	r, g, b, _ := c.RGBA()
	return fmt.Sprintf("#%02x%02x%02x", r>>8, g>>8, b>>8)
}

// Text is the visible grid as plain text, one line per row.
func (s *Session) Text() string { return s.screenText() }
