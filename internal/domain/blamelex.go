package domain

import (
	"bytes"
	"strings"

	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/syntax"
)

// LexBlameLines lexes a blamed file's content, reassembled from its lines,
// for the surfaces that colour blame (the TUI overlay and the web overlay).
// It returns one token slice per blame line (index = line − 1), or nil —
// plain rendering — when the path has no lexer, the file is past
// MaxSyntaxBytes, or a line holds a BARE \r (see HasBareCR). The caller
// applies the [ui] diff_syntax switch; this function is pure.
func LexBlameLines(path string, lines []model.BlameLine) [][]syntax.Tok {
	if len(lines) == 0 {
		return nil
	}
	lang := syntax.Detect(path)
	if lang == "" {
		return nil
	}
	parts := make([]string, len(lines))
	for i, ln := range lines {
		parts[i] = ln.Content
	}
	// Trailing "\n": blame hands back content lines without their terminator,
	// and a CRLF file's last line would otherwise end in a \r that reads as
	// bare. syntax.Lex adds no line for a trailing newline, so the token slice
	// still has exactly one entry per blame line.
	src := strings.Join(parts, "\n") + "\n"
	if len(src) > MaxSyntaxBytes || HasBareCR([]byte(src)) {
		return nil
	}
	return syntax.Lex(lang, []byte(src))
}

// HasBareCR reports whether data holds a \r that is NOT part of a CRLF. The
// surfaces that lex their own content (blame, the file preview, the hunk
// picker) treat such a file as unhighlightable: they turn a lone \r into a
// line break while syntax.Lex keeps it inside its line, so every line after
// it would receive the previous line's runs. A CRLF file is safe — both
// sides count one line.
func HasBareCR(data []byte) bool {
	return bytes.Contains(bytes.ReplaceAll(data, []byte("\r\n"), []byte("\n")), []byte("\r"))
}
