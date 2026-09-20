package markdown

import (
	"strings"

	"github.com/homeend/gigagit/internal/syntax"
)

// LangSuggestion is the fence language of a GitHub suggestion block. It is an
// ordinary code block to the parser; frontends caption it. gg never applies
// one.
const LangSuggestion = "suggestion"

// colourCode attaches token runs to a fenced block whose language chroma
// knows (its aliases included: golang, sh, js, py, yml, …). An unknown
// language, a suggestion, or a block past MaxLexLines stays plain.
func colourCode(b *Block) {
	if b.Lang == "" || strings.EqualFold(b.Lang, LangSuggestion) || len(b.Lines) == 0 || len(b.Lines) > MaxLexLines {
		return
	}
	var src strings.Builder
	for i, l := range b.Lines {
		if i > 0 {
			src.WriteByte('\n')
		}
		src.WriteString(l.Text)
	}
	toks := syntax.Lex(strings.ToLower(b.Lang), []byte(src.String()))
	for i := range b.Lines {
		if i >= len(toks) {
			break
		}
		for _, t := range toks[i] {
			if cls := t.Class.String(); cls != "" {
				b.Lines[i].Toks = append(b.Lines[i].Toks, WireTok{Start: t.Start, End: t.End, Class: cls})
			}
		}
	}
}
