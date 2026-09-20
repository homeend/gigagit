package markdown

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// Scan bounds: how far a link's text and destination may run. They keep a
// line of stray brackets from costing a scan to the end of the text each.
const (
	maxLinkText = 1000
	maxLinkDest = 2048
)

// ParseInline parses one run of inline markdown (a paragraph, a heading, a
// table cell). Newlines become hard breaks, as they do in GitHub comments.
func ParseInline(s string) []Inline { return parseInline(s) }

func parseInline(s string) []Inline {
	if s == "" {
		return nil
	}
	return newInliner(s, 0).run()
}

type inliner struct {
	s     string
	depth int
	out   []Inline
	text  strings.Builder
	// Failed searches, remembered: whether a closer exists depends on the
	// closer alone, so one miss answers every later opener of that shape.
	noCloser map[[2]int]bool
	noTicks  map[int]bool
}

func newInliner(s string, depth int) *inliner {
	return &inliner{s: s, depth: depth, noCloser: map[[2]int]bool{}, noTicks: map[int]bool{}}
}

func (p *inliner) flush() {
	if p.text.Len() > 0 {
		p.push(Inline{Kind: InText, Text: p.text.String()})
		p.text.Reset()
	}
}

// push appends a node, merging adjacent text leaves.
func (p *inliner) push(n Inline) {
	if n.Kind == InText {
		if n.Text == "" {
			return
		}
		if k := len(p.out); k > 0 && p.out[k-1].Kind == InText {
			p.out[k-1].Text += n.Text
			return
		}
	}
	p.out = append(p.out, n)
}

func (p *inliner) add(n Inline) {
	p.flush()
	p.push(n)
}

// child parses nested content one level down; past MaxDepth it is literal.
func (p *inliner) child(s string) []Inline {
	if s == "" {
		return nil
	}
	if p.depth+1 > MaxDepth {
		return []Inline{{Kind: InText, Text: s}}
	}
	return newInliner(s, p.depth+1).run()
}

func (p *inliner) run() []Inline {
	s := p.s
	for i := 0; i < len(s); {
		c := s[i]
		switch {
		case c == '\\':
			i = p.escape(i)
		case c == '\n':
			p.lineBreak()
			i++
			for i < len(s) && s[i] == ' ' {
				i++
			}
		case c == '`':
			i = p.codeSpan(i)
		case c == '!' && i+1 < len(s) && s[i+1] == '[':
			i = p.link(i+1, true)
		case c == '[':
			i = p.link(i, false)
		case c == '<':
			i = p.angleAutolink(i)
		case c == '*' || c == '_' || c == '~':
			i = p.emphasis(i)
		case (c == 'h' || c == 'H') && p.boundaryBefore(i):
			i = p.bareURL(i)
		case c == '@' && p.boundaryBefore(i):
			i = p.mention(i)
		case c == '#':
			i = p.issueRef(i)
		default:
			p.text.WriteByte(c)
			i++
		}
	}
	p.flush()
	return p.out
}

func (p *inliner) lineBreak() {
	t := strings.TrimRight(p.text.String(), " ")
	p.text.Reset()
	p.text.WriteString(t)
	p.add(Inline{Kind: InBreak})
}

func (p *inliner) escape(i int) int {
	s := p.s
	if i+1 >= len(s) {
		p.text.WriteByte('\\')
		return i + 1
	}
	n := s[i+1]
	if n == '\n' {
		return i + 1 // the newline itself makes the break
	}
	if n < utf8.RuneSelf && isPunct(n) {
		p.text.WriteByte(n)
		return i + 2
	}
	p.text.WriteByte('\\')
	return i + 1
}

func isPunct(c byte) bool {
	return c >= '!' && c <= '/' || c >= ':' && c <= '@' || c >= '[' && c <= '`' || c >= '{' && c <= '~'
}

// ---- code spans ------------------------------------------------------------

func runLen(s string, i int, c byte) int {
	n := 0
	for i+n < len(s) && s[i+n] == c {
		n++
	}
	return n
}

// tickClose finds the closing run of exactly n backticks at or after from.
func (p *inliner) tickClose(from, n int) int {
	if p.noTicks[n] {
		return -1
	}
	s := p.s
	for j := from; j < len(s); {
		if s[j] != '`' {
			j++
			continue
		}
		k := runLen(s, j, '`')
		if k == n {
			return j
		}
		j += k
	}
	p.noTicks[n] = true
	return -1
}

func (p *inliner) codeSpan(i int) int {
	n := runLen(p.s, i, '`')
	end := p.tickClose(i+n, n)
	if end < 0 {
		p.text.WriteString(p.s[i : i+n])
		return i + n
	}
	body := strings.ReplaceAll(p.s[i+n:end], "\n", " ")
	if len(body) >= 2 && body[0] == ' ' && body[len(body)-1] == ' ' && strings.TrimSpace(body) != "" {
		body = body[1 : len(body)-1]
	}
	p.add(Inline{Kind: InCode, Text: body})
	return end + n
}

// ---- emphasis --------------------------------------------------------------

func (p *inliner) runeBefore(i int) rune {
	if i <= 0 {
		return ' '
	}
	r, _ := utf8.DecodeLastRuneInString(p.s[:i])
	return r
}

func (p *inliner) runeAt(i int) rune {
	if i >= len(p.s) {
		return ' '
	}
	r, _ := utf8.DecodeRuneInString(p.s[i:])
	return r
}

func alnum(r rune) bool { return unicode.IsLetter(r) || unicode.IsDigit(r) }

func (p *inliner) boundaryBefore(i int) bool {
	r := p.runeBefore(i)
	return !alnum(r) && r != '_'
}

func (p *inliner) emphasis(i int) int {
	s := p.s
	c := s[i]
	n := runLen(s, i, c)
	literal := func() int {
		p.text.WriteString(s[i : i+n])
		return i + n
	}
	if n > 3 || (c == '~' && n > 2) {
		return literal()
	}
	if unicode.IsSpace(p.runeAt(i+n)) || (c == '_' && alnum(p.runeBefore(i))) {
		return literal()
	}
	end := p.closer(i+n, c, n)
	if end < 0 {
		return literal()
	}
	inner := p.child(s[i+n : end])
	switch {
	case c == '~':
		p.add(Inline{Kind: InDel, In: inner})
	case n == 1:
		p.add(Inline{Kind: InEm, In: inner})
	case n == 2:
		p.add(Inline{Kind: InStrong, In: inner})
	default:
		p.add(Inline{Kind: InStrong, In: []Inline{{Kind: InEm, In: inner}}})
	}
	return end + n
}

// closer finds a closing run of exactly n c's after from: not preceded by
// whitespace, and for '_' not followed by a letter or digit (GitHub's
// intraword rule, which keeps snake_case literal). Code spans and escapes
// are stepped over.
func (p *inliner) closer(from int, c byte, n int) int {
	key := [2]int{int(c), n}
	if p.noCloser[key] {
		return -1
	}
	s := p.s
	for j := from; j < len(s); {
		switch {
		case s[j] == '\\' && j+1 < len(s):
			j += 2
		case s[j] == '`':
			k := runLen(s, j, '`')
			if e := p.tickClose(j+k, k); e >= 0 {
				j = e + k
			} else {
				j += k
			}
		case s[j] == c:
			k := runLen(s, j, c)
			if k == n && j > from && !unicode.IsSpace(p.runeBefore(j)) && (c != '_' || !alnum(p.runeAt(j+k))) {
				return j
			}
			j += k
		default:
			j++
		}
	}
	p.noCloser[key] = true
	return -1
}

// ---- links and images ------------------------------------------------------

// link handles "[text](dest)" with i at the '['; image says a '!' came first.
func (p *inliner) link(i int, image bool) int {
	s := p.s
	literal := func() int {
		if image {
			p.text.WriteByte('!')
		}
		p.text.WriteByte('[')
		return i + 1
	}
	closeAt := p.bracketClose(i + 1)
	if closeAt < 0 || closeAt+1 >= len(s) || s[closeAt+1] != '(' {
		return literal()
	}
	dest, end, ok := linkDest(s, closeAt+2)
	if !ok {
		return literal()
	}
	label := s[i+1 : closeAt]
	url, verdict := safeURL(dest)
	if image {
		alt := flatText(p.child(label))
		switch verdict {
		case urlOK:
			p.add(Inline{Kind: InImage, Text: alt, URL: url})
		case urlShow:
			p.text.WriteString(alt + " (" + dest + ")")
		default:
			p.text.WriteString(alt)
		}
		return end
	}
	inner := p.child(label)
	switch verdict {
	case urlOK:
		if len(inner) == 0 {
			inner = []Inline{{Kind: InText, Text: url}}
		}
		p.add(Inline{Kind: InLink, URL: url, In: inner})
	default:
		p.flush()
		for _, n := range inner {
			p.push(n)
		}
		if verdict == urlShow {
			p.text.WriteString(" (" + dest + ")")
		}
	}
	return end
}

// bracketClose finds the ']' matching an already-open '[', within bounds.
func (p *inliner) bracketClose(from int) int {
	s := p.s
	depth := 1
	limit := min(len(s), from+maxLinkText)
	for j := from; j < limit; {
		switch s[j] {
		case '\\':
			j += 2
			continue
		case '`':
			k := runLen(s, j, '`')
			if e := p.tickClose(j+k, k); e >= 0 {
				j = e + k
			} else {
				j += k
			}
			continue
		case '[':
			depth++
		case ']':
			depth--
			if depth == 0 {
				return j
			}
		}
		j++
	}
	return -1
}

// linkDest parses `dest "title")` with i just past the '('; it returns the
// destination and the index just past the ')'.
func linkDest(s string, i int) (string, int, bool) {
	limit := min(len(s), i+maxLinkDest)
	for i < limit && s[i] == ' ' {
		i++
	}
	var dest string
	if i < limit && s[i] == '<' {
		j := strings.IndexAny(s[i:limit], ">\n")
		if j < 0 || s[i+j] != '>' {
			return "", 0, false
		}
		dest, i = s[i+1:i+j], i+j+1
	} else {
		depth, j := 0, i
	scan:
		for ; j < limit; j++ {
			switch s[j] {
			case '\\':
				j++
			case '(':
				depth++
			case ')':
				if depth == 0 {
					break scan
				}
				depth--
			case ' ', '\n':
				break scan
			}
		}
		if j > limit {
			j = limit
		}
		dest, i = s[i:j], j
	}
	for i < limit && s[i] == ' ' {
		i++
	}
	if i < limit && (s[i] == '"' || s[i] == '\'') {
		q := s[i]
		j := i + 1
		for j < limit && s[j] != q && s[j] != '\n' {
			if s[j] == '\\' {
				j++
			}
			j++
		}
		if j >= limit || s[j] != q {
			return "", 0, false
		}
		i = j + 1
		for i < limit && s[i] == ' ' {
			i++
		}
	}
	if i >= limit || s[i] != ')' {
		return "", 0, false
	}
	return dest, i + 1, true
}

type urlVerdict int

const (
	urlDrop urlVerdict = iota // never shown: an active-content scheme, or nothing
	urlShow                   // not a link, but worth reading: shown as text
	urlOK                     // http(s): the only destinations a consumer sees
)

// safeURL is the one gate every link and image destination passes. Only a
// clean http(s) URL is ever returned as a URL.
func safeURL(dest string) (string, urlVerdict) {
	dest = strings.TrimSpace(dest)
	if dest == "" {
		return "", urlDrop
	}
	// Scheme sniffing ignores the whitespace and control bytes browsers ignore
	// ("java\tscript:").
	var sq strings.Builder
	for _, r := range dest {
		if r > ' ' && r != 0x7f {
			sq.WriteRune(unicode.ToLower(r))
		}
	}
	squeezed := sq.String()
	for _, bad := range []string{"javascript:", "data:", "vbscript:"} {
		if strings.HasPrefix(squeezed, bad) {
			return "", urlDrop
		}
	}
	clean := len(dest) <= maxLinkDest
	for _, r := range dest {
		if r <= ' ' || r == 0x7f {
			clean = false
		}
	}
	lower := strings.ToLower(dest)
	rest, isHTTP := strings.CutPrefix(lower, "https://")
	if !isHTTP {
		rest, isHTTP = strings.CutPrefix(lower, "http://")
	}
	if clean && isHTTP && rest != "" {
		return dest, urlOK
	}
	return "", urlShow
}

func flatText(in []Inline) string {
	var sb strings.Builder
	for _, n := range in {
		switch n.Kind {
		case InBreak:
			sb.WriteByte(' ')
		case InText, InCode, InRef, InImage:
			sb.WriteString(n.Text)
		default:
			sb.WriteString(flatText(n.In))
		}
	}
	return sb.String()
}

// ---- autolinks and references ----------------------------------------------

func hasHTTPPrefix(s string) int {
	for _, pre := range []string{"https://", "http://"} {
		if len(s) > len(pre) && strings.EqualFold(s[:len(pre)], pre) {
			return len(pre)
		}
	}
	return 0
}

func (p *inliner) angleAutolink(i int) int {
	s := p.s
	if hasHTTPPrefix(s[i+1:]) > 0 {
		if j := strings.IndexAny(s[i+1:min(len(s), i+1+maxLinkDest)], "> \n<"); j > 0 && s[i+1+j] == '>' {
			if url, v := safeURL(s[i+1 : i+1+j]); v == urlOK {
				p.add(Inline{Kind: InLink, URL: url, In: []Inline{{Kind: InText, Text: url}}})
				return i + j + 2
			}
		}
	}
	p.text.WriteByte('<')
	return i + 1
}

func (p *inliner) bareURL(i int) int {
	s := p.s
	if hasHTTPPrefix(s[i:]) == 0 {
		p.text.WriteByte(s[i])
		return i + 1
	}
	j := i
	for j < len(s) && j-i < maxLinkDest && s[j] > ' ' && s[j] != '<' && s[j] != 0x7f {
		j++
	}
	// Trailing punctuation belongs to the sentence, not the URL.
	for j > i {
		last := s[j-1]
		if strings.IndexByte(".,;:!?'\"*_~", last) >= 0 ||
			(last == ')' && strings.Count(s[i:j], ")") > strings.Count(s[i:j], "(")) {
			j--
			continue
		}
		break
	}
	url, v := safeURL(s[i:j])
	if v != urlOK {
		p.text.WriteByte(s[i])
		return i + 1
	}
	p.add(Inline{Kind: InLink, URL: url, In: []Inline{{Kind: InText, Text: url}}})
	return j
}

func isNameByte(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9'
}

// mention: "@name" or "@org/team" at a word boundary.
func (p *inliner) mention(i int) int {
	s := p.s
	j := i + 1
	for j < len(s) && j-i <= 80 && (isNameByte(s[j]) || ((s[j] == '-' || s[j] == '/') && j+1 < len(s) && isNameByte(s[j+1]) && j > i+1)) {
		j++
	}
	if j == i+1 {
		p.text.WriteByte('@')
		return i + 1
	}
	p.add(Inline{Kind: InRef, Text: s[i:j]})
	return j
}

// issueRef: "#123", optionally owned by an "org/repo" written right before it.
func (p *inliner) issueRef(i int) int {
	s := p.s
	j := i + 1
	for j < len(s) && j-i <= 10 && s[j] >= '0' && s[j] <= '9' {
		j++
	}
	if j == i+1 || alnum(p.runeAt(j)) {
		p.text.WriteByte('#')
		return i + 1
	}
	pending := p.text.String()
	owner := trailingRepo(pending)
	if owner == "" && !p.boundaryBefore(i) {
		p.text.WriteByte('#')
		return i + 1
	}
	if owner != "" {
		p.text.Reset()
		p.text.WriteString(pending[:len(pending)-len(owner)])
	}
	p.add(Inline{Kind: InRef, Text: owner + s[i:j]})
	return j
}

// trailingRepo is the "owner/repo" the pending text ends with, or "".
func trailingRepo(t string) string {
	k := len(t)
	for k > 0 && (isNameByte(t[k-1]) || strings.IndexByte("-._/", t[k-1]) >= 0) {
		k--
	}
	word := t[k:]
	owner, repo, ok := strings.Cut(word, "/")
	if !ok || owner == "" || repo == "" || strings.Contains(repo, "/") {
		return ""
	}
	if k > 0 {
		if r, _ := utf8.DecodeLastRuneInString(t[:k]); alnum(r) {
			return ""
		}
	}
	return word
}
