package template

import (
	"fmt"
	"strings"
)

// CQuotePath renders p the way git prints a path containing control
// characters: double-quoted, with \n \r \t \" \\ as their usual C escapes and
// every other control byte (< 0x20) as a \NNN octal escape. A path with no
// control bytes is returned byte-exact and unquoted. Byte-wise, not rune-wise:
// UTF-8 continuation/lead bytes are >= 0x80 and can never look like controls.
// (Moved from internal/tui so the engine's CompleteConflict op and the TUI's
// context-file writer share one implementation.)
func CQuotePath(p string) string {
	needsQuote := false
	for i := 0; i < len(p); i++ {
		if p[i] < 0x20 {
			needsQuote = true
			break
		}
	}
	if !needsQuote {
		return p
	}
	var b strings.Builder
	b.WriteByte('"')
	for i := 0; i < len(p); i++ {
		c := p[i]
		switch c {
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		default:
			if c < 0x20 {
				fmt.Fprintf(&b, `\%03o`, c)
			} else {
				b.WriteByte(c)
			}
		}
	}
	b.WriteByte('"')
	return b.String()
}

// ConflictContextDoc renders the per-run context file body handed to a
// conflict agent: op/source/target header lines then the conflicted paths one
// per line, each value C-quoted only when it carries a control byte, so no
// value can forge an extra line. Both the TUI's tool runs and the engine's
// CompleteConflict op write exactly these bytes.
func ConflictContextDoc(op, source, target string, files []string) string {
	var b strings.Builder
	b.WriteString("op: " + CQuotePath(op) + "\n")
	b.WriteString("source: " + CQuotePath(source) + "\n")
	b.WriteString("target: " + CQuotePath(target) + "\n")
	b.WriteString("conflicted:\n")
	for _, f := range files {
		b.WriteString(CQuotePath(f) + "\n")
	}
	return b.String()
}
