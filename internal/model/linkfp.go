package model

import (
	"fmt"
	"hash/fnv"
	"strings"
)

// LineFingerprint is the 8-hex fingerprint an uncommitted line link carries
// (`:<line>~<fp>`): FNV-1a 32-bit over the line with its leading and trailing
// whitespace trimmed, so re-indenting (or a CRLF ending) is not a change. A
// line that is blank after trimming has none ("") — it would match every
// blank line. It is deliberately NOT NoteContextHash's sha256: the browser
// builds links synchronously, where sha256 is async-only, and 32 bits tell
// "this line" from "another line" inside one file. The JS twin is
// lineFingerprint in internal/web/static/links.js; a test pins the pair.
func LineFingerprint(line string) string {
	t := strings.TrimSpace(line)
	if t == "" {
		return ""
	}
	h := fnv.New32a()
	h.Write([]byte(t))
	return fmt.Sprintf("%08x", h.Sum32())
}

// LinkFingerprintOK reports whether fp is a well-formed fingerprint: exactly
// 8 lowercase hex characters.
func LinkFingerprintOK(fp string) bool {
	if len(fp) != 8 {
		return false
	}
	for i := 0; i < len(fp); i++ {
		if c := fp[i]; !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}
