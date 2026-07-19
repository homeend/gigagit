package mcp

import (
	"bytes"
	"unicode/utf8"
)

const defaultMaxBytes = 262144

// filePayload is the shared text/binary/truncation reply contract for every
// content-reading tool (bookmark_read, shelf_read).
type filePayload struct {
	Text      string `json:"text,omitempty"`
	Binary    bool   `json:"binary,omitempty"`
	Truncated bool   `json:"truncated,omitempty"`
	Size      int    `json:"size"`
	Hint      string `json:"hint,omitempty"`
}

// textPayload classifies data: binary (NUL byte or invalid UTF-8) yields no
// text and a gg_export hint; text over maxBytes is truncated at a rune
// boundary. maxBytes <= 0 means the default cap.
func textPayload(data []byte, maxBytes int) filePayload {
	if maxBytes <= 0 {
		maxBytes = defaultMaxBytes
	}
	p := filePayload{Size: len(data)}
	if bytes.IndexByte(data, 0) >= 0 || !utf8.Valid(data) {
		p.Binary = true
		p.Hint = "binary content — use gg_export to copy it to a directory"
		return p
	}
	if len(data) > maxBytes {
		cut := maxBytes
		for cut > 0 && !utf8.RuneStart(data[cut]) {
			cut--
		}
		p.Text = string(data[:cut])
		p.Truncated = true
		return p
	}
	p.Text = string(data)
	return p
}
