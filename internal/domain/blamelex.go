package domain

import "bytes"

// HasBareCR reports whether data holds a \r that is NOT part of a CRLF. The
// surfaces that lex their own content (blame, the file preview, the hunk
// picker) treat such a file as unhighlightable: they turn a lone \r into a
// line break while syntax.Lex keeps it inside its line, so every line after
// it would receive the previous line's runs. A CRLF file is safe — both
// sides count one line.
func HasBareCR(data []byte) bool {
	return bytes.Contains(bytes.ReplaceAll(data, []byte("\r\n"), []byte("\n")), []byte("\r"))
}
