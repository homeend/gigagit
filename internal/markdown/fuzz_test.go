package markdown

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode"
)

// FuzzParse: Parse is total, its tree keeps every consumer invariant, and —
// for input without the constructs that legitimately drop characters (link
// destinations, fence info strings, task boxes, surplus table cells) — no letter is lost. (Digits
// are: an ordered list keeps its number in Start, not in text.)
func FuzzParse(f *testing.F) {
	files, _ := filepath.Glob(filepath.Join("testdata", "*.md"))
	for _, md := range files {
		if src, err := os.ReadFile(md); err == nil {
			f.Add(string(src))
		}
	}
	for _, s := range []string{"", "*", "**a", "[", "![](", "> > > - 1. x", "|\n-", "```\n", "- [x]", "\\", "<https://", "a\r\n\r\nb\x00"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, src string) {
		d := Parse(src)
		checkDoc(t, d)
		if _, err := json.Marshal(d); err != nil {
			t.Fatalf("marshal: %v", err)
		}
		Summary(src)
		if strings.ContainsAny(src, "[`~|") || !utf8Valid(src) {
			return
		}
		have := map[rune]int{}
		for _, r := range allText(d) {
			have[r]++
		}
		for _, r := range src {
			if unicode.IsLetter(r) {
				if have[r] == 0 {
					t.Fatalf("lost %q from %q", r, src)
				}
				have[r]--
			}
		}
	})
}

func utf8Valid(s string) bool { return strings.ToValidUTF8(s, "") == s }
