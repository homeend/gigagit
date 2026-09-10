// Syntax colouring of the hunk picker: assembling and lexing the two full-file
// texts its document describes, so every candidate line, context line and
// output line can be painted from its own side's runs (spec §4.2).

package tui

import (
	"strings"
	"sync"

	tea "github.com/charmbracelet/bubbletea"

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
		// The cap is checked BEFORE the join: summing len(l)+1 is exactly the
		// joined length, so an oversized side is refused without copying the
		// whole document twice to find that out.
		n := 0
		for _, l := range lines {
			n += len(l) + 1
		}
		if n > domain.MaxSyntaxBytes {
			return nil
		}
		src := strings.Join(lines, "\n") + "\n"
		if hasBareCR([]byte(src)) {
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

// pickerLexedMsg carries one picker's finished syntax runs back to the UI
// thread. picker identifies which picker asked: a lex started for a surface
// the user has since closed must not paint anything, so the handler applies it
// only while that very picker is still live.
type pickerLexedMsg struct {
	picker   *hunkPicker
	cur, inc [][]syntax.Tok
}

// lexCmd is the ASYNCHRONOUS form: it hands the lex to the Bubble Tea runtime
// so the UI thread never waits for chroma (measured: 1.8 s on a 1 MB Go file,
// 390 ms on 200 KB — a hard freeze if done inline). The four open sites push
// the picker unlexed and return this Cmd; the runs arrive later as a
// pickerLexedMsg and the next repaint carries the colour.
//
// It returns nil — no goroutine at all — when the switch is off or the path
// has no lexer, so the plain path costs nothing.
func (e *hunkPicker) lexCmd(on bool) tea.Cmd {
	if !on || syntax.Detect(e.path) == "" {
		return nil
	}
	// The closure captures the immutable inputs rather than reading e from the
	// worker: the document's text never changes while the picker is open (only
	// its per-block Mode/Picks do, on the UI thread).
	path, doc := e.path, e.doc
	return func() tea.Msg {
		cur, inc := lexPickerDoc(path, doc, true)
		return pickerLexedMsg{picker: e, cur: cur, inc: inc}
	}
}

// setSyntax stores finished runs on a picker that may already have rendered
// plain, dropping the sanitized caches so the next render rebuilds them with
// masks. Painting is layout-stable — a mask never changes a line's text or
// width — so no scroll offset or cursor position needs adjusting.
func (e *hunkPicker) setSyntax(cur, inc [][]syntax.Tok) {
	e.curTok, e.incTok = cur, inc
	e.sanBuilt, e.outBuilt = false, false
}

// withSyntax is the SYNCHRONOUS form of the same wiring: it lexes the picker's
// two sides inline and stores the runs, so every line it renders can be
// painted from the mask its own side's lexer produced. The four open sites use
// lexCmd instead (chroma is far too slow to run on the UI thread); this stays
// for tests and for any caller that already holds a worker goroutine.
// on=false is a documented no-op leaving the byte-identical plain path.
//
// MUST be called before the first render — or, like setSyntax, it invalidates
// the sanitized caches so a later call still takes effect: ensureSan builds
// the display masks once, lazily, from these runs.
func (e *hunkPicker) withSyntax(on bool) *hunkPicker {
	if !on {
		return e
	}
	cur, inc := lexPickerDoc(e.path, e.doc, true)
	e.setSyntax(cur, inc)
	return e
}

// sanLine is one sanitized display line of the picker's document plus its
// paint mask. The mask is empty when the picker was opened without syntax runs
// (or the line's own runs are empty), which is the byte-identical plain path.
type sanLine struct {
	text string
	mask runMask
}

// sanPickLine sanitizes one document line for display and builds its paint
// mask from that line's syntax runs. sanitizeCell does the mapping so a tab's
// 4-column expansion carries the classes with it; with no runs the line takes
// sanitizeLine and an empty mask.
func sanPickLine(l string, toks []syntax.Tok) sanLine {
	if len(toks) == 0 {
		return sanLine{text: sanitizeLine(l)}
	}
	disp, emph, cls := sanitizeCell(l, nil, toks)
	return sanLine{text: string(disp), mask: runMask{cls: cls, emph: emph}}
}
