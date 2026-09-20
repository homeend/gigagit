package tui

import (
	"strconv"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/homeend/gigagit/internal/i18n"
	"github.com/homeend/gigagit/internal/markdown"
	"github.com/homeend/gigagit/internal/syntax"
)

// Markdown in the TUI: forge text (a pull request's description, comments and
// review threads) arrives parsed (internal/markdown) and is laid out here as
// rows of text plus a per-rune class mask.
//
// The mask is the EXISTING syntax-class channel (contentLine.cls → winRow.cls →
// styledRuns → styles.syntaxStyle), extended with the TUI-local pseudo-classes
// below for inline styles. So a markdown row wraps, scrolls, takes search
// emphasis and obeys the reverse-video rule exactly like a coloured code line,
// with no mask plumbing of its own. A fenced block's token runs keep their
// real syntax classes.
//
// Inline styles are terminal ATTRIBUTES over the host's base style, so they
// read in every theme — the Terminal one included; only code borrows a colour,
// from the syntax palette. Every rune of forge text passes sanitizeLine before
// its mask is built: len(cls) == len([]rune(text)) always, and no escape
// sequence can reach the terminal.
const (
	mdStrong syntax.Class = 100 + iota
	mdEm
	mdStrongEm
	mdDel
	mdCode
	mdLink
	mdDim // a link's " (url)" tail, a suggestion's caption, table and thematic rules
	mdRef // @mention, #123
	mdHeading
	mdQuote
	mdClassEnd
)

// mdStyle is base with one markdown pseudo-class applied.
func (s *styles) mdStyle(base lipgloss.Style, c syntax.Class) lipgloss.Style {
	switch c {
	case mdStrong, mdRef:
		return base.Bold(true)
	case mdEm:
		return base.Italic(true)
	case mdStrongEm:
		return base.Bold(true).Italic(true)
	case mdDel:
		return base.Strikethrough(true)
	case mdCode:
		if col := s.syntaxColor(syntax.String); col != "" {
			return base.Foreground(lipgloss.Color(col))
		}
		return base.Bold(true)
	case mdLink:
		return base.Underline(true)
	case mdDim, mdQuote:
		return base.Faint(true)
	case mdHeading:
		return base.Bold(true).Underline(true)
	}
	return base
}

// mdRow is one laid-out line: len(cls) == len([]rune(text)).
type mdRow struct {
	text string
	cls  []syntax.Class
	// pre marks a PREFORMATTED row — a code line, a table row: a host that
	// wraps at draw time must cut it instead (contentLine.noWrap).
	pre bool
}

// mdRun is a stretch of runes of one class, the unit inline layout works in.
type mdRun struct {
	r   []rune
	cls syntax.Class
}

const mdRuleWidth = 20 // a thematic break when nothing bounds it

// mdRows lays a parsed text out. width > 0 word-wraps prose to it (code and
// tables are clipped, never reflowed); width <= 0 emits one row per logical
// line and leaves wrapping to the host (renderWindow's modeWrap, which hangs a
// continuation under its row's text).
func mdRows(doc markdown.Doc, width int) []mdRow {
	rows := mdBlocks(doc.Blocks, width, false)
	for i := range rows { // a prefix on a degenerate width: never past the bound
		rows[i] = mdClip(rows[i], width)
	}
	return rows
}

// mdInlineRows lays one inline run out behind a plain lead ("↳ carol: ").
func mdInlineRows(in []markdown.Inline, width int, lead string) []mdRow {
	runs := append([]mdRun{{r: []rune(sanitizeLine(lead)), cls: syntax.Plain}}, mdInlineRuns(in, syntax.Plain)...)
	return mdWrapLines(runs, width)
}

// mdBlocks lays a block list out; tight suppresses the blank row between
// blocks (the inside of a list item).
func mdBlocks(blocks []markdown.Block, width int, tight bool) []mdRow {
	var out []mdRow
	for i, b := range blocks {
		if i > 0 && !tight {
			out = append(out, mdRow{})
		}
		out = append(out, mdBlock(b, width)...)
	}
	return out
}

func mdBlock(b markdown.Block, width int) []mdRow {
	switch b.Kind {
	case markdown.KindHeading:
		return mdWrapLines(mdInlineRuns(b.Inline, mdHeading), width)
	case markdown.KindList:
		return mdList(b, width)
	case markdown.KindQuote:
		inner := mdBlocks(b.Blocks, mdInnerWidth(width, 2), false)
		for i := range inner {
			for j, c := range inner[i].cls {
				if c == syntax.Plain {
					inner[i].cls[j] = mdQuote
				}
			}
		}
		return mdPrefix(inner, "│ ", "│ ", mdQuote)
	case markdown.KindCode:
		return mdCodeBlock(b, width)
	case markdown.KindTable:
		return mdTable(b, width)
	case markdown.KindRule:
		n := mdRuleWidth
		if width > 0 {
			n = min(width, 40)
		}
		return []mdRow{mdPlainRow(strings.Repeat("─", n), mdDim)}
	default: // a paragraph, and any kind a newer parser might add
		return mdWrapLines(mdInlineRuns(b.Inline, syntax.Plain), width)
	}
}

// mdInnerWidth is the width left inside a prefix of n columns; an unbounded
// width stays unbounded, and a bounded one never drops below 1.
func mdInnerWidth(width, n int) int {
	if width <= 0 {
		return width
	}
	return max(width-n, 1)
}

func mdPlainRow(text string, c syntax.Class) mdRow {
	text = sanitizeLine(text)
	cls := make([]syntax.Class, len([]rune(text)))
	for i := range cls {
		cls[i] = c
	}
	return mdRow{text: text, cls: cls}
}

// mdPrefix puts first before rows[0] and rest before every other row.
func mdPrefix(rows []mdRow, first, rest string, c syntax.Class) []mdRow {
	for i := range rows {
		p := rest
		if i == 0 {
			p = first
		}
		lead := mdPlainRow(p, c)
		rows[i] = mdRow{text: lead.text + rows[i].text, cls: append(lead.cls, rows[i].cls...), pre: rows[i].pre}
	}
	return rows
}

func mdList(b markdown.Block, width int) []mdRow {
	var out []mdRow
	for i, it := range b.Items {
		marker := "• "
		switch {
		case it.Task == markdown.TaskDone:
			marker = "☑ "
		case it.Task == markdown.TaskOpen:
			marker = "☐ "
		case b.Ordered:
			marker = strconv.Itoa(b.Start+i) + ". "
		}
		mw := lipgloss.Width(marker)
		rows := mdBlocks(it.Blocks, mdInnerWidth(width, mw), true)
		if len(rows) == 0 {
			rows = []mdRow{{}}
		}
		out = append(out, mdPrefix(rows, marker, strings.Repeat(" ", mw), syntax.Plain)...)
	}
	return out
}

// mdCodeBlock: a caption for a suggestion, then each line indented two
// columns with its token runs. Never wrapped: clipped to width.
func mdCodeBlock(b markdown.Block, width int) []mdRow {
	var out []mdRow
	if strings.EqualFold(b.Lang, markdown.LangSuggestion) {
		out = append(out, mdPlainRow(i18n.T("suggestion"), mdDim))
	}
	for _, l := range b.Lines {
		disp, at := mdSanitizeMapped(l.Text)
		cls := make([]syntax.Class, len(disp))
		src := len(at) - 1 // source rune count
		for _, tk := range l.Toks {
			if tk.Start < 0 || tk.End > src || tk.Start >= tk.End {
				continue
			}
			c := mdTokClass(tk.Class)
			for k := at[tk.Start]; k < at[tk.End]; k++ {
				cls[k] = c
			}
		}
		row := mdRow{text: "  " + string(disp), cls: append([]syntax.Class{syntax.Plain, syntax.Plain}, cls...), pre: true}
		out = append(out, mdClip(row, width))
	}
	return out
}

// mdSanitizeMapped is sanitizeLine that also reports where each SOURCE rune
// landed: at[i] is the display index of source rune i, at[len] the end. A tab
// widens, so the parser's rune offsets have to be carried across.
func mdSanitizeMapped(s string) ([]rune, []int) {
	src := []rune(strings.TrimSuffix(s, "\r"))
	at := make([]int, 0, len(src)+1)
	var out []rune
	for _, r := range src {
		at = append(at, len(out))
		switch {
		case r == '\t':
			for n := 4 - len(out)%4; n > 0; n-- {
				out = append(out, ' ')
			}
		case r < 0x20 || r == 0x7f:
			out = append(out, '·')
		default:
			out = append(out, r)
		}
	}
	return out, append(at, len(out))
}

var mdTokClasses = func() map[string]syntax.Class {
	m := map[string]syntax.Class{}
	for c := syntax.Plain; c <= syntax.Attr; c++ {
		if n := c.String(); n != "" {
			m[n] = c
		}
	}
	return m
}()

func mdTokClass(name string) syntax.Class { return mdTokClasses[name] }

// mdClip cuts a row to width display columns (width <= 0: untouched).
func mdClip(row mdRow, width int) mdRow {
	if width <= 0 || lipgloss.Width(row.text) <= width {
		return row
	}
	r := []rune(row.text)
	w, n := 0, 0
	for n < len(r) {
		cw := lipgloss.Width(string(r[n]))
		if w+cw > width {
			break
		}
		w += cw
		n++
	}
	return mdRow{text: string(r[:n]), cls: row.cls[:n], pre: row.pre}
}

// mdTable lays a table out in aligned columns sized to their widest cell.
func mdTable(b markdown.Block, width int) []mdRow {
	cols := len(b.Head)
	if cols == 0 {
		return nil
	}
	cell := func(in []markdown.Inline, base syntax.Class) mdRow {
		var row mdRow
		for _, run := range mdInlineRuns(in, base) {
			for _, r := range run.r {
				if r == '\n' {
					r = ' '
				}
				row.text += string(r)
				row.cls = append(row.cls, run.cls)
			}
		}
		return row
	}
	grid := [][]mdRow{make([]mdRow, cols)}
	for c := range b.Head {
		grid[0][c] = cell(b.Head[c], mdStrong)
	}
	for _, r := range b.Rows {
		line := make([]mdRow, cols)
		for c := 0; c < cols && c < len(r); c++ {
			line[c] = cell(r[c], syntax.Plain)
		}
		grid = append(grid, line)
	}
	widths := make([]int, cols)
	for _, line := range grid {
		for c, x := range line {
			widths[c] = max(widths[c], lipgloss.Width(x.text))
		}
	}
	pad := func(x mdRow, c int) mdRow {
		gap := widths[c] - lipgloss.Width(x.text)
		left := 0
		if c < len(b.Align) {
			switch b.Align[c] {
			case "right":
				left = gap
			case "center":
				left = gap / 2
			}
		}
		l, r := mdPlainRow(strings.Repeat(" ", left), syntax.Plain), mdPlainRow(strings.Repeat(" ", gap-left), syntax.Plain)
		return mdRow{text: l.text + x.text + r.text, cls: append(append(l.cls, x.cls...), r.cls...)}
	}
	join := func(parts []mdRow, sep string) mdRow {
		var row mdRow
		for i, p := range parts {
			if i > 0 {
				s := mdPlainRow(sep, mdDim)
				row.text += s.text
				row.cls = append(row.cls, s.cls...)
			}
			row.text += p.text
			row.cls = append(row.cls, p.cls...)
		}
		return row
	}
	var out []mdRow
	for i, line := range grid {
		parts := make([]mdRow, cols)
		for c := range line {
			parts[c] = pad(line[c], c)
		}
		line := join(parts, " │ ")
		line.pre = true
		out = append(out, mdClip(line, width))
		if i == 0 {
			rules := make([]mdRow, cols)
			for c := range rules {
				rules[c] = mdPlainRow(strings.Repeat("─", widths[c]), mdDim)
			}
			rule := join(rules, "─┼─")
			rule.pre = true
			out = append(out, mdClip(rule, width))
		}
	}
	return out
}

// ---- inline ----------------------------------------------------------------

// mdInlineRuns flattens an inline tree to runs. base is the class plain text
// wears here (mdHeading inside a heading); a hard break is a "\n" run.
func mdInlineRuns(in []markdown.Inline, base syntax.Class) []mdRun {
	var out []mdRun
	text := func(s string, c syntax.Class) {
		if r := []rune(sanitizeLine(s)); len(r) > 0 {
			out = append(out, mdRun{r: r, cls: c})
		}
	}
	for _, n := range in {
		switch n.Kind {
		case markdown.InText:
			text(n.Text, base)
		case markdown.InBreak:
			out = append(out, mdRun{r: []rune{'\n'}, cls: base})
		case markdown.InCode:
			text(n.Text, mdCode)
		case markdown.InRef:
			text(n.Text, mdRef)
		case markdown.InStrong:
			out = append(out, mdInlineRuns(n.In, mdMix(base, mdStrong))...)
		case markdown.InEm:
			out = append(out, mdInlineRuns(n.In, mdMix(base, mdEm))...)
		case markdown.InDel:
			out = append(out, mdInlineRuns(n.In, mdDel)...)
		case markdown.InLink:
			label := mdFlat(n.In)
			out = append(out, mdInlineRuns(n.In, mdLink)...)
			if label != n.URL {
				text(" ("+n.URL+")", mdDim)
			}
		case markdown.InImage:
			label := i18n.T("[image]")
			if n.Text != "" {
				label = i18n.T("[image: %s]", n.Text)
			}
			text(label, mdLink)
			text(" ("+n.URL+")", mdDim)
		default:
			text(n.Text, base)
			out = append(out, mdInlineRuns(n.In, base)...)
		}
	}
	return out
}

// mdMix nests emphasis: strong inside em (or the reverse) is both.
func mdMix(outer, inner syntax.Class) syntax.Class {
	if (outer == mdStrong && inner == mdEm) || (outer == mdEm && inner == mdStrong) || outer == mdStrongEm {
		return mdStrongEm
	}
	return inner
}

func mdFlat(in []markdown.Inline) string {
	var sb strings.Builder
	for _, n := range in {
		sb.WriteString(n.Text)
		sb.WriteString(mdFlat(n.In))
	}
	return sb.String()
}

// mdWrapLines splits runs at their hard breaks and word-wraps each line.
func mdWrapLines(runs []mdRun, width int) []mdRow {
	var out []mdRow
	var text []rune
	var cls []syntax.Class
	flush := func() {
		out = append(out, mdWrap(text, cls, width)...)
		text, cls = nil, nil
	}
	for _, run := range runs {
		for _, r := range run.r {
			if r == '\n' {
				flush()
				continue
			}
			text = append(text, r)
			cls = append(cls, run.cls)
		}
	}
	flush()
	return out
}

// mdWrap word-wraps one line to width display columns, carrying the mask. A
// word wider than the width is hard-split; the spaces a break falls on are
// dropped. width <= 0: the line as it is. An empty line is one empty row.
func mdWrap(text []rune, cls []syntax.Class, width int) []mdRow {
	if width <= 0 || lipgloss.Width(string(text)) <= width {
		return []mdRow{{text: string(text), cls: cls}}
	}
	var out []mdRow
	emit := func(a, b int) {
		for b > a && text[b-1] == ' ' {
			b--
		}
		out = append(out, mdRow{text: string(text[a:b]), cls: cls[a:b:b]})
	}
	start, w, lastSpace := 0, 0, -1
	for i := 0; i < len(text); i++ {
		cw := lipgloss.Width(string(text[i]))
		if w+cw > width && i > start {
			if text[i] == ' ' {
				emit(start, i)
				for i < len(text) && text[i] == ' ' {
					i++
				}
				start = i
			} else if lastSpace > start {
				emit(start, lastSpace)
				start = lastSpace + 1
			} else {
				emit(start, i)
				start = i
			}
			w, lastSpace = 0, -1
			for k := start; k < i && k < len(text); k++ {
				w += lipgloss.Width(string(text[k]))
			}
			if i >= len(text) {
				break
			}
			cw = lipgloss.Width(string(text[i]))
		}
		if text[i] == ' ' {
			lastSpace = i
		}
		w += cw
	}
	if start < len(text) {
		emit(start, len(text))
	}
	return out
}
