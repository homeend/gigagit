package mcp

import (
	"strings"
	"testing"
)

func TestTextPayloadPlain(t *testing.T) {
	p := textPayload([]byte("hello"), 0) // 0 → default cap
	if p.Text != "hello" || p.Binary || p.Truncated || p.Size != 5 {
		t.Fatalf("payload = %+v", p)
	}
}

func TestTextPayloadBinary(t *testing.T) {
	p := textPayload([]byte{0x00, 0x01, 'a'}, 0)
	if !p.Binary || p.Text != "" || p.Size != 3 {
		t.Fatalf("payload = %+v", p)
	}
	if !strings.Contains(p.Hint, "gg_export") {
		t.Fatalf("binary hint must point at gg_export: %+v", p)
	}
}

func TestTextPayloadInvalidUTF8IsBinary(t *testing.T) {
	p := textPayload([]byte{0xff, 0xfe, 0xfd}, 0)
	if !p.Binary {
		t.Fatalf("invalid UTF-8 must be binary: %+v", p)
	}
}

func TestTextPayloadTruncates(t *testing.T) {
	data := []byte(strings.Repeat("é", 100)) // 2 bytes per rune
	p := textPayload(data, 51)               // odd cap lands mid-rune
	if !p.Truncated || p.Size != 200 {
		t.Fatalf("payload = %+v", p)
	}
	if len(p.Text) != 50 { // backed off to the rune boundary
		t.Fatalf("truncation split a rune: len=%d", len(p.Text))
	}
}
