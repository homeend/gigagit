package domain

import (
	"context"
	"strings"

	"github.com/homeend/gigagit/internal/cache"
	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/syntax"
)

// Runs is a shown text's syntax colouring: the per-source-line runs (index
// = line − 1), or none — plain for good, or Pending: owed, to be asked
// again with TokOnly. Sum names the bytes the runs belong to (contentSum;
// "" for no text), so a frontend applies late runs only to the text it
// still shows.
type Runs struct {
	Sum     string
	Tok     [][]syntax.Tok
	Pending bool
}

// Highlighter is the one door to syntax colouring for every surface that
// shows a file — diffs, blame, file content, previews. It lexes through
// the content-keyed token store: a lex is shared by everyone asking for the
// same bytes, stops when nobody waits, and is kept, so the same text is
// coloured once whichever view shows it.
type Highlighter struct {
	syntax func() bool // the [ui] diff_syntax switch, read per call
	store  *tokStore
}

// NewHighlighter is a Highlighter over cache c (nil = runs shared while a
// lex runs, not kept), consulting on per call (nil = never highlight).
func NewHighlighter(on func() bool, c cache.Cache) *Highlighter {
	return &Highlighter{syntax: on, store: newTokStore(c)}
}

// On reports whether highlighting is on ([ui] diff_syntax): a surface that
// would start a background colouring job asks first, so the plain path
// costs nothing.
func (h *Highlighter) On() bool { return h != nil && h.syntax != nil && h.syntax() }

// Runs colours src as path's language. Plain for good when highlighting is
// off, the path has no lexer, or src is past MaxSyntaxBytes. TokDefer
// answers stored runs, lexes a src of at most InlineSyntaxBytes, and
// otherwise answers Pending; TokInline and TokOnly lex and wait.
func (h *Highlighter) Runs(ctx context.Context, path string, src []byte, mode TokMode) (Runs, error) {
	out := Runs{Sum: contentSum(src)}
	if !h.On() || src == nil || len(src) > MaxSyntaxBytes {
		return out, nil
	}
	lang := syntax.Detect(path)
	if lang == "" {
		return out, nil
	}
	if mode == TokDefer {
		if toks, ok := h.store.Get(lang, out.Sum); ok {
			out.Tok = toks
			return out, nil
		}
		if len(src) > InlineSyntaxBytes {
			out.Pending = true
			return out, nil
		}
	}
	toks, err := h.store.Lex(ctx, lang, out.Sum, src)
	if err != nil {
		return Runs{}, err
	}
	out.Tok = toks
	return out, nil
}

// Content colours a file's bytes shown as LINE ROWS (a preview, a viewer):
// Runs, except that a bare \r keeps it plain — those surfaces break a line
// there while the lexer does not, so every later line would take the
// previous line's runs (see HasBareCR).
func (h *Highlighter) Content(ctx context.Context, path string, data []byte, mode TokMode) (Runs, error) {
	if HasBareCR(data) {
		return Runs{Sum: contentSum(data)}, nil
	}
	return h.Runs(ctx, path, data, mode)
}

// Blame colours a blamed file's lines: their content re-joined, a trailing
// "\n" added (blame hands lines back without terminators, and a CRLF file's
// last line would otherwise end in a \r that reads as bare). The lexer adds
// no line for that trailing newline, so there is one run slice per line.
func (h *Highlighter) Blame(ctx context.Context, path string, lines []model.BlameLine, mode TokMode) (Runs, error) {
	if len(lines) == 0 {
		return Runs{}, nil
	}
	parts := make([]string, len(lines))
	for i, ln := range lines {
		parts[i] = ln.Content
	}
	return h.Content(ctx, path, []byte(strings.Join(parts, "\n")+"\n"), mode)
}
