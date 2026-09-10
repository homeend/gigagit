// Syntax colouring of the hunk picker: assembling and lexing the two full-file
// texts its document describes, so every candidate line, context line and
// output line can be painted from its own side's runs (spec §4.2).

package tui

import (
	"strings"
	"sync"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/hunkpick"
	"github.com/homeend/gigagit/internal/syntax"
)

// lexPickerDoc assembles the two full-file texts doc describes — the literal
// context plus every block's Current lines (the current side), the same context
// plus every block's Incoming lines (the incoming side) — and lexes each once
// with path's lexer. The two sides are lexed CONCURRENTLY: each can cost ~1 s
// of chroma at domain.MaxSyntaxBytes and they are independent, so the worst
// case is one side's time rather than their sum.
//
// A side comes back nil (the plain path) when the switch is off, the path has
// no lexer, the side is past domain.MaxSyntaxBytes, or the side holds a BARE \r.
// The picker splits its lines on \n exactly as syntax.Lex numbers them, so a
// bare \r causes it no desync — but the two other self-lexing viewers
// (lexBlame, lexPreview) refuse such content and this one refuses it alike
// rather than growing a third rule for pathological input.
func lexPickerDoc(path string, doc *hunkpick.Doc, on bool) (cur, inc [][]syntax.Tok) {
	if !on || doc == nil {
		return nil, nil
	}
	lang := syntax.Detect(path)
	if lang == "" {
		return nil, nil
	}
	var curL, incL []string
	for _, it := range doc.Items {
		if it.Block == nil {
			curL = append(curL, it.Literal...)
			incL = append(incL, it.Literal...)
			continue
		}
		curL = append(curL, it.Block.Current...)
		incL = append(incL, it.Block.Incoming...)
	}
	// Trailing "\n": the doc's lines carry no terminator, and syntax.Lex adds
	// no line for a trailing newline, so each side still yields exactly one
	// token slice per line. A CRLF document keeps its \r at each line's end;
	// sanitizeCell trims that last rune and classMask clamps token ends to the
	// trimmed length, so the display masks stay aligned.
	lex := func(lines []string) [][]syntax.Tok {
		if len(lines) == 0 {
			return nil
		}
		src := strings.Join(lines, "\n") + "\n"
		if len(src) > domain.MaxSyntaxBytes || hasBareCR([]byte(src)) {
			return nil
		}
		return syntax.Lex(lang, []byte(src))
	}
	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); cur = lex(curL) }()
	go func() { defer wg.Done(); inc = lex(incL) }()
	wg.Wait()
	return cur, inc
}

// withSyntax lexes the picker's two sides and stores the runs, so every line
// it renders can be painted from the mask its own side's lexer produced. It is
// chained at the four open sites, where the [ui] diff_syntax switch is known;
// on=false is a documented no-op leaving the byte-identical plain path (every
// test constructor and any future caller takes it).
//
// The lex is SYNCHRONOUS: the loaders that fetch a picker's bytes off the UI
// thread hand back raw sides (or, for a conflict, marker text), and the
// hunkpick.Doc that determines the two full-file texts is only assembled once
// the message reaches Update — no loader goroutine holds it. Spec §4.2 rules
// this acceptable ("else synchronously"); domain.MaxSyntaxBytes bounds it.
//
// MUST be called before the first render: ensureSan builds the display masks
// once, lazily, from these runs.
func (e *hunkPicker) withSyntax(on bool) *hunkPicker {
	if !on {
		return e
	}
	e.curTok, e.incTok = lexPickerDoc(e.path, e.doc, true)
	return e
}
