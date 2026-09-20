package markdown

import (
	"strings"
)

// Parse turns forge text into a block tree. It is total: any input yields a
// Doc, and nothing in the input can make it fail or panic.
func Parse(src string) Doc {
	src = normalise(src)
	if strings.TrimSpace(src) == "" {
		return Doc{Blocks: []Block{}}
	}
	if len(src) > MaxInput {
		return Doc{Blocks: plainParagraphs(src)}
	}
	return Doc{Blocks: parseBlocks(strings.Split(src, "\n"), 0)}
}

// normalise folds line endings to \n, drops NULs and expands each line's
// LEADING tabs to four spaces, so every indent test below counts spaces only.
func normalise(src string) string {
	src = strings.ReplaceAll(src, "\r\n", "\n")
	src = strings.ReplaceAll(src, "\r", "\n")
	src = strings.ReplaceAll(src, "\x00", "")
	src = strings.TrimRight(src, "\n")
	if !strings.Contains(src, "\t") {
		return src
	}
	lines := strings.Split(src, "\n")
	for i, ln := range lines {
		j := 0
		for j < len(ln) && (ln[j] == ' ' || ln[j] == '\t') {
			j++
		}
		if strings.Contains(ln[:j], "\t") {
			lines[i] = strings.ReplaceAll(ln[:j], "\t", "    ") + ln[j:]
		}
	}
	return strings.Join(lines, "\n")
}

// plainParagraphs is the oversized-input fallback: blank-line separated
// paragraphs of literal text, no block or inline parsing at all.
func plainParagraphs(src string) []Block {
	var out []Block
	for _, chunk := range strings.Split(src, "\n\n") {
		if strings.TrimSpace(chunk) == "" {
			continue
		}
		out = append(out, Block{Kind: KindPara, Inline: []Inline{{Kind: InText, Text: strings.Trim(chunk, "\n")}}})
	}
	return out
}

func isBlank(ln string) bool { return strings.TrimSpace(ln) == "" }

// indentOf is the number of leading spaces.
func indentOf(ln string) int {
	n := 0
	for n < len(ln) && ln[n] == ' ' {
		n++
	}
	return n
}

// parseBlocks parses one container's lines. depth counts the containers
// (quotes and list items) above it; at MaxDepth no further container opens
// and their markers stay literal text.
func parseBlocks(lines []string, depth int) []Block {
	var out []Block
	var para []string
	flush := func() {
		if len(para) > 0 {
			out = append(out, Block{Kind: KindPara, Inline: parseInline(strings.Join(para, "\n"))})
			para = nil
		}
	}
	for i := 0; i < len(lines); {
		ln := lines[i]
		if isBlank(ln) {
			flush()
			i++
			continue
		}
		if f, ok := fenceOpen(ln); ok {
			flush()
			var b Block
			b, i = parseFence(lines, i, f)
			out = append(out, b)
			continue
		}
		if level, text, ok := atxHeading(ln); ok {
			flush()
			out = append(out, Block{Kind: KindHeading, Level: level, Inline: parseInline(text)})
			i++
			continue
		}
		if isRule(ln) {
			flush()
			out = append(out, Block{Kind: KindRule})
			i++
			continue
		}
		if depth < MaxDepth {
			if isQuote(ln) {
				flush()
				var inner []string
				inner, i = gatherQuote(lines, i)
				out = append(out, Block{Kind: KindQuote, Blocks: parseBlocks(inner, depth+1)})
				continue
			}
			if m, ok := listMarker(ln); ok && (len(para) == 0 || m.interrupts()) {
				flush()
				var b Block
				b, i = parseList(lines, i, m, depth)
				out = append(out, b)
				continue
			}
		}
		if i+1 < len(lines) && strings.Contains(ln, "|") {
			if b, next, ok := parseTable(lines, i); ok {
				flush()
				out = append(out, b)
				i = next
				continue
			}
		}
		para = append(para, strings.TrimSpace(ln))
		i++
	}
	flush()
	return out
}

// ---- fenced code -----------------------------------------------------------

type fence struct {
	indent int
	ch     byte
	n      int
	info   string
}

func fenceOpen(ln string) (fence, bool) {
	ind := indentOf(ln)
	if ind > 3 || ind >= len(ln) {
		return fence{}, false
	}
	ch := ln[ind]
	if ch != '`' && ch != '~' {
		return fence{}, false
	}
	n := 0
	for ind+n < len(ln) && ln[ind+n] == ch {
		n++
	}
	if n < 3 {
		return fence{}, false
	}
	info := strings.TrimSpace(ln[ind+n:])
	if ch == '`' && strings.Contains(info, "`") {
		return fence{}, false // an inline code span, not a fence
	}
	return fence{indent: ind, ch: ch, n: n, info: info}, true
}

func (f fence) closes(ln string) bool {
	ind := indentOf(ln)
	if ind > 3 {
		return false
	}
	n := 0
	for ind+n < len(ln) && ln[ind+n] == f.ch {
		n++
	}
	return n >= f.n && strings.TrimSpace(ln[ind+n:]) == ""
}

// parseFence consumes the block opened at lines[i]; an unclosed fence runs
// to the end of its container, as it does on GitHub.
func parseFence(lines []string, i int, f fence) (Block, int) {
	b := Block{Kind: KindCode, Lang: fenceLang(f.info), Lines: []CodeLine{}}
	i++
	for ; i < len(lines); i++ {
		if f.closes(lines[i]) {
			i++
			break
		}
		ln := lines[i]
		strip := min(indentOf(ln), f.indent)
		b.Lines = append(b.Lines, CodeLine{Text: ln[strip:]})
	}
	colourCode(&b)
	return b, i
}

// fenceLang is the info string's first word, kept only when it is a tame
// identifier: the value ends up in a data attribute and a lexer lookup, so
// anything else reads as "no language".
func fenceLang(info string) string {
	word, _, _ := strings.Cut(info, " ")
	if len(word) == 0 || len(word) > 32 {
		return ""
	}
	for i := 0; i < len(word); i++ {
		c := word[i]
		ok := c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' ||
			c == '_' || c == '+' || c == '#' || c == '.' || c == '-'
		if !ok {
			return ""
		}
	}
	return word
}

// ---- headings and rules ----------------------------------------------------

func atxHeading(ln string) (int, string, bool) {
	ind := indentOf(ln)
	if ind > 3 {
		return 0, "", false
	}
	s := ln[ind:]
	level := 0
	for level < len(s) && s[level] == '#' {
		level++
	}
	if level == 0 || level > 6 {
		return 0, "", false
	}
	if level < len(s) && s[level] != ' ' {
		return 0, "", false
	}
	text := strings.TrimSpace(s[level:])
	// A closing run of #s counts only when a space sets it off.
	if t := strings.TrimRight(text, "#"); t != text && (t == "" || strings.HasSuffix(t, " ")) {
		text = strings.TrimSpace(t)
	}
	return level, text, true
}

func isRule(ln string) bool {
	if indentOf(ln) > 3 {
		return false
	}
	s := strings.TrimSpace(ln)
	if s == "" {
		return false
	}
	ch := s[0]
	if ch != '-' && ch != '*' && ch != '_' {
		return false
	}
	n := 0
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case ch:
			n++
		case ' ':
		default:
			return false
		}
	}
	return n >= 3
}

// ---- quotes ----------------------------------------------------------------

func isQuote(ln string) bool {
	ind := indentOf(ln)
	return ind <= 3 && ind < len(ln) && ln[ind] == '>'
}

// startsBlock reports a line that can never be a lazy paragraph continuation.
func startsBlock(ln string) bool {
	if _, ok := fenceOpen(ln); ok {
		return true
	}
	if _, _, ok := atxHeading(ln); ok {
		return true
	}
	if isRule(ln) || isQuote(ln) {
		return true
	}
	_, ok := listMarker(ln)
	return ok
}

// gatherQuote collects the quote starting at lines[i] with its markers
// stripped. A plain line right after quoted text continues that text lazily.
func gatherQuote(lines []string, i int) ([]string, int) {
	var inner []string
	for ; i < len(lines); i++ {
		ln := lines[i]
		if isQuote(ln) {
			rest := ln[indentOf(ln)+1:]
			rest = strings.TrimPrefix(rest, " ")
			inner = append(inner, rest)
			continue
		}
		if isBlank(ln) || startsBlock(ln) || len(inner) == 0 || !lazyTarget(inner[len(inner)-1]) {
			break
		}
		inner = append(inner, ln)
	}
	return inner, i
}

// lazyTarget: the previous line is paragraph text a lazy line may join — not
// blank, and not a fence or heading line. Nested quote markers are peeled
// first, so a lazy line can continue a paragraph several quotes deep.
func lazyTarget(prev string) bool {
	for isQuote(prev) {
		prev = strings.TrimPrefix(prev[indentOf(prev)+1:], " ")
	}
	if isBlank(prev) {
		return false
	}
	if _, ok := fenceOpen(prev); ok {
		return false
	}
	if _, _, ok := atxHeading(prev); ok {
		return false
	}
	return !isRule(prev)
}

// ---- lists -----------------------------------------------------------------

type marker struct {
	indent  int  // leading spaces before the marker
	content int  // column the item's content starts at
	ordered bool //
	delim   byte // bullet char, or the ordered delimiter ('.' or ')')
	num     int  // ordered: the number
	empty   bool // nothing follows the marker on its line
}

// interrupts reports whether this marker may cut a paragraph short: a bullet
// with content, or an ordered item numbered 1 (CommonMark's rule, which keeps
// "in 2019. The" style prose from turning into lists).
func (m marker) interrupts() bool {
	if m.empty {
		return false
	}
	return !m.ordered || m.num == 1
}

func (m marker) sibling(o marker) bool {
	return m.ordered == o.ordered && m.delim == o.delim
}

func listMarker(ln string) (marker, bool) {
	ind := indentOf(ln)
	if ind > 3 || ind >= len(ln) {
		return marker{}, false
	}
	m := marker{indent: ind}
	j := ind
	switch c := ln[j]; {
	case c == '-' || c == '*' || c == '+':
		m.delim = c
		j++
	case c >= '0' && c <= '9':
		for j < len(ln) && j-ind < 9 && ln[j] >= '0' && ln[j] <= '9' {
			m.num = m.num*10 + int(ln[j]-'0')
			j++
		}
		if j >= len(ln) || (ln[j] != '.' && ln[j] != ')') {
			return marker{}, false
		}
		m.ordered, m.delim = true, ln[j]
		j++
	default:
		return marker{}, false
	}
	if j == len(ln) {
		m.empty, m.content = true, j+1
		return m, true
	}
	if ln[j] != ' ' {
		return marker{}, false
	}
	sp := 0
	for j+sp < len(ln) && ln[j+sp] == ' ' {
		sp++
	}
	switch {
	case j+sp == len(ln):
		m.empty, m.content = true, j+1
	case sp > 4:
		m.content = j + 1
	default:
		m.content = j + sp
	}
	return m, true
}

// parseList consumes the list whose first marker is at lines[i].
func parseList(lines []string, i int, first marker, depth int) (Block, int) {
	b := Block{Kind: KindList, Ordered: first.ordered}
	if first.ordered {
		b.Start = first.num
	}
	m := first
	for {
		var item []string
		item, i = gatherItem(lines, i, m)
		it := Item{}
		if len(item) > 0 {
			it.Task, item[0] = taskMarker(item[0])
		}
		it.Blocks = parseBlocks(item, depth+1)
		if it.Blocks == nil {
			it.Blocks = []Block{}
		}
		b.Items = append(b.Items, it)

		// Blank lines between items make a loose list, not two lists.
		j := i
		for j < len(lines) && isBlank(lines[j]) {
			j++
		}
		if j >= len(lines) {
			return b, i
		}
		next, ok := listMarker(lines[j])
		if !ok || !first.sibling(next) || isRule(lines[j]) {
			return b, i
		}
		m, i = next, j
	}
}

// gatherItem collects one item's lines with the content indent stripped.
func gatherItem(lines []string, i int, m marker) ([]string, int) {
	first := ""
	if !m.empty && m.content <= len(lines[i]) {
		first = lines[i][m.content:]
	}
	item := []string{first}
	for i++; i < len(lines); i++ {
		ln := lines[i]
		if isBlank(ln) {
			j := i
			for j < len(lines) && isBlank(lines[j]) {
				j++
			}
			if j >= len(lines) || indentOf(lines[j]) < m.content {
				break
			}
			item = append(item, "")
			continue
		}
		if indentOf(ln) >= m.content {
			item = append(item, ln[m.content:])
			continue
		}
		if startsBlock(ln) || !lazyTarget(item[len(item)-1]) {
			break
		}
		if _, ok := listMarker(item[len(item)-1]); ok && len(item) > 1 {
			break // the previous line opened a nested item; do not glue onto it
		}
		item = append(item, strings.TrimSpace(ln))
	}
	for len(item) > 0 && item[len(item)-1] == "" {
		item = item[:len(item)-1]
	}
	return item, i
}

// taskMarker peels a GitHub task box off an item's first line.
func taskMarker(s string) (string, string) {
	if len(s) < 5 || s[0] != '[' || s[2] != ']' || s[3] != ' ' {
		return "", s
	}
	switch s[1] {
	case ' ':
		return TaskOpen, s[4:]
	case 'x', 'X':
		return TaskDone, s[4:]
	}
	return "", s
}

// ---- tables ----------------------------------------------------------------

// parseTable tries a pipe table whose header is lines[i]. The header and its
// delimiter row must agree on the column count; body rows are padded or cut to
// it. The table ends at a blank line or a line without a pipe.
func parseTable(lines []string, i int) (Block, int, bool) {
	head := splitCells(lines[i])
	if len(head) == 0 || len(head) > MaxTableCols {
		return Block{}, i, false
	}
	align, ok := delimiterRow(lines[i+1])
	if !ok || len(align) != len(head) {
		return Block{}, i, false
	}
	b := Block{Kind: KindTable, Align: align, Rows: [][][]Inline{}}
	for _, c := range head {
		b.Head = append(b.Head, cellInline(c))
	}
	i += 2
	for ; i < len(lines); i++ {
		ln := lines[i]
		if isBlank(ln) || !strings.Contains(ln, "|") {
			break
		}
		cells := splitCells(ln)
		row := make([][]Inline, len(head))
		for c := range row {
			if c < len(cells) {
				row[c] = cellInline(cells[c])
			} else {
				row[c] = []Inline{}
			}
		}
		b.Rows = append(b.Rows, row)
	}
	return b, i, true
}

func cellInline(s string) []Inline {
	in := parseInline(s)
	if in == nil {
		return []Inline{}
	}
	return in
}

// splitCells splits a table row on its unescaped pipes. As on GitHub, \|
// is a literal pipe everywhere in a cell — inside a code span too.
func splitCells(ln string) []string {
	s := strings.TrimSpace(ln)
	s = strings.TrimPrefix(s, "|")
	if strings.HasSuffix(s, "|") && !strings.HasSuffix(s, `\|`) {
		s = s[:len(s)-1]
	}
	var cells []string
	var cur strings.Builder
	for i := 0; i < len(s); i++ {
		switch {
		case s[i] == '\\' && i+1 < len(s) && s[i+1] == '|':
			cur.WriteByte('|')
			i++
		case s[i] == '|':
			cells = append(cells, strings.TrimSpace(cur.String()))
			cur.Reset()
			if len(cells) > MaxTableCols {
				return cells
			}
		default:
			cur.WriteByte(s[i])
		}
	}
	return append(cells, strings.TrimSpace(cur.String()))
}

func delimiterRow(ln string) ([]string, bool) {
	if !strings.Contains(ln, "-") {
		return nil, false
	}
	cells := splitCells(ln)
	align := make([]string, 0, len(cells))
	for _, c := range cells {
		left, right := strings.HasPrefix(c, ":"), strings.HasSuffix(c, ":")
		dashes := strings.TrimSuffix(strings.TrimPrefix(c, ":"), ":")
		if dashes == "" || strings.Trim(dashes, "-") != "" {
			return nil, false
		}
		switch {
		case left && right:
			align = append(align, "center")
		case right:
			align = append(align, "right")
		case left:
			align = append(align, "left")
		default:
			align = append(align, "")
		}
	}
	return align, true
}
