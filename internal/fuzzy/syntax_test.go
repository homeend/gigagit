package fuzzy

import (
	"fmt"
	"testing"
)

func names(ms []Match) []string {
	out := make([]string, len(ms))
	for i, m := range ms {
		out[i] = m.S
	}
	return out
}

func TestParseTerms(t *testing.T) {
	got := Parse("'darwin ^docs .md$ !test ^exact$ plain")
	want := "[exact:darwin prefix:docs suffix:.md !exact:test equal:exact fuzzy:plain]"
	if fmt.Sprint(got.Terms) != want {
		t.Fatalf("terms = %v\nwant %s", got.Terms, want)
	}
}

func TestParseLoneMarkersAreIgnored(t *testing.T) {
	// A marker with nothing behind it (mid-typing) must not turn into an
	// empty term that matches or rejects everything.
	for _, q := range []string{"'", "^", "$", "!", "' ^ !"} {
		if got := Parse(q).Terms; len(got) != 0 {
			t.Fatalf("Parse(%q) = %v, want no terms", q, got)
		}
	}
}

func TestExactTermRejectsAScatteredSubsequence(t *testing.T) {
	cands := []string{
		"docs/superpowers/specs/2026-06-13-arrow-focus-design.md", // d…a…r…w…i…n
		"internal/clipboard/darwin.go",
	}
	if got := names(Rank("darwin", cands, 10)); len(got) != 2 {
		t.Fatalf("fuzzy darwin should still match both (subsequence), got %v", got)
	}
	if got := fmt.Sprint(names(Rank("'darwin", cands, 10))); got != "[internal/clipboard/darwin.go]" {
		t.Fatalf("'darwin must keep only the literal substring, got %s", got)
	}
}

func TestPrefixSuffixAndNegation(t *testing.T) {
	cands := []string{"internal/tui/view.go", "internal/tui/view_test.go", "docs/view.md"}
	cases := map[string]string{
		"^internal":            "[internal/tui/view.go internal/tui/view_test.go]",
		".go$":                 "[internal/tui/view.go internal/tui/view_test.go]",
		"view !_test":          "[docs/view.md internal/tui/view.go]",
		"^internal .go$ !test": "[internal/tui/view.go]",
		"^docs/view.md$":       "[docs/view.md]",
	}
	for q, want := range cases {
		if got := fmt.Sprint(names(Rank(q, cands, 10))); got != want {
			t.Fatalf("Rank(%q) = %s, want %s", q, got, want)
		}
	}
}

func TestTermsAreAnded(t *testing.T) {
	cands := []string{"internal/tui/files_view.go", "internal/web/view.js", "internal/tui/model.go"}
	if got := fmt.Sprint(names(Rank("tui view", cands, 10))); got != "[internal/tui/files_view.go]" {
		t.Fatalf("every term must match, got %s", got)
	}
}

func TestSmartCase(t *testing.T) {
	cands := []string{"readme.md", "README.md"}
	if got := len(Rank("readme", cands, 10)); got != 2 {
		t.Fatalf("a lower-case query is case-insensitive, got %d matches", got)
	}
	if got := fmt.Sprint(names(Rank("README", cands, 10))); got != "[README.md]" {
		t.Fatalf("an upper-case letter makes the query case-sensitive, got %s", got)
	}
}

func TestLiteralMatchOutranksAScatteredOne(t *testing.T) {
	cands := []string{
		"docs/superpowers/specs/2026-06-13-arrow-focus-design.md",
		"internal/clipboard/darwin.go",
	}
	if got := names(Rank("darwin", cands, 10)); got[0] != "internal/clipboard/darwin.go" {
		t.Fatalf("the literal hit must rank first, got %v", got)
	}
}
