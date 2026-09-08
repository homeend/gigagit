// Package syntax lexes source text into per-line token runs for the diff
// views. Pure: no git, TUI, or domain imports — only chroma. Offsets are rune
// indices within one line so the renderers can map them to display columns
// exactly like textdiff.Span.
package syntax

import (
	"path/filepath"
	"strings"

	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/lexers"
)

// Class is the coarse token kind the renderers colour. It deliberately
// collapses chroma's ~60 token types to a handful so both frontends carry one
// small palette.
type Class uint8

const (
	Plain Class = iota
	Keyword
	Type
	Func
	Name
	String
	Number
	Comment
	Operator
	Punct
	Attr
)

var classNames = [...]string{"", "kw", "ty", "fn", "nm", "str", "num", "cmt", "op", "pn", "at"}

// String is the short suffix used for CSS classes (web) and style lookup
// (TUI); empty for Plain.
func (c Class) String() string {
	if int(c) < len(classNames) {
		return classNames[c]
	}
	return ""
}

// Tok is one run of a single Class on one line: rune offsets [Start, End).
// Plain runs are never emitted.
type Tok struct {
	Start, End int
	Class      Class
}

// noLexerBasenames are file names chroma matches to the wrong lexer via a
// colliding glob rather than a dedicated grammar: go.mod/go.sum/go.work have
// no chroma lexer of their own, but ampl.xml and modula-2.xml both register
// "*.mod", and AMPL wins the match — lexing go.mod with AMPL's grammar
// produces nonsense runs. Treat these as unknown (plain) instead.
var noLexerBasenames = map[string]bool{
	"go.mod":  true,
	"go.sum":  true,
	"go.work": true,
}

// Detect returns the chroma lexer name for path, or "" when no lexer matches
// the file name. Content is never inspected (cheap, deterministic).
func Detect(path string) string {
	if path == "" {
		return ""
	}
	if noLexerBasenames[filepath.Base(path)] {
		return ""
	}
	l := lexers.Match(path)
	if l == nil {
		return ""
	}
	name := l.Config().Name
	// chroma ships a real "plaintext" lexer that matches *.txt (distinct
	// from its internal, name-"fallback" catch-all); highlighting it would
	// be a no-op, so callers should treat it the same as "no lexer found".
	if name == "plaintext" {
		return ""
	}
	return name
}

// MaxLineTokens bounds runs kept per line; a minified one-liner past it keeps
// its first MaxLineTokens runs and the rest render plain.
const MaxLineTokens = 2000

// Lex tokenises the whole of src with the named lexer and splits the result
// per line, numbering lines exactly as textdiff.splitLines does (a trailing
// \n adds no line; \r stays in the line and counts as a rune). nil when lang
// is unknown or src is empty.
func Lex(lang string, src []byte) [][]Tok {
	if lang == "" || len(src) == 0 {
		return nil
	}
	l := lexers.Get(lang)
	if l == nil {
		return nil
	}
	// Explicit options, NOT nil: chroma's defaults set EnsureLF, which
	// rewrites a bare \r to \n before lexing. textdiff.splitLines splits on
	// \n only, so chroma would see one MORE line than the renderers do and
	// every line after the first lone \r would receive the previous line's
	// runs. State "root" is the entry state every lexer defines.
	it, err := chroma.Coalesce(l).Tokenise(&chroma.TokeniseOptions{State: "root"}, string(src))
	if err != nil {
		return nil
	}
	// Number of lines textdiff will produce.
	nLines := strings.Count(string(src), "\n")
	if src[len(src)-1] != '\n' {
		nLines++
	}
	out := make([][]Tok, nLines)
	line, col := 0, 0
	for _, t := range it.Tokens() {
		cls := classOf(t.Type)
		val := t.Value
		for len(val) > 0 {
			nl := strings.IndexByte(val, '\n')
			seg := val
			if nl >= 0 {
				seg = val[:nl]
			}
			n := len([]rune(seg))
			if n > 0 && cls != Plain && line < nLines && len(out[line]) < MaxLineTokens {
				out[line] = append(out[line], Tok{Start: col, End: col + n, Class: cls})
			}
			col += n
			if nl < 0 {
				break
			}
			val = val[nl+1:]
			line++
			col = 0
		}
	}
	return out
}

// classOf maps a chroma token type to the coarse Class.
func classOf(t chroma.TokenType) Class {
	switch {
	// Preproc lines (#include, #define) read as directives, not comments;
	// they sit inside the Comment category so they must be tested first.
	case t.InSubCategory(chroma.CommentPreproc):
		return Attr
	case t.InCategory(chroma.Comment):
		return Comment
	case t == chroma.KeywordType:
		return Type
	case t.InCategory(chroma.Keyword):
		return Keyword
	case t.InSubCategory(chroma.LiteralString):
		return String
	case t.InSubCategory(chroma.LiteralNumber):
		return Number
	case t.InCategory(chroma.Operator):
		return Operator
	case t.InCategory(chroma.Punctuation):
		return Punct
	case t == chroma.NameClass || t == chroma.NameBuiltin || t == chroma.NameException || t == chroma.NameNamespace:
		return Type
	case t == chroma.NameFunction || t == chroma.NameFunctionMagic:
		return Func
	case t == chroma.NameDecorator || t == chroma.NameAttribute || t == chroma.NameTag:
		return Attr
	case t.InCategory(chroma.Name):
		return Name
	}
	return Plain
}
