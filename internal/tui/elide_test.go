package tui

import (
	"testing"

	"github.com/charmbracelet/lipgloss"
)

// elidePath keeps segments by priority — name, rightmost dir, first dir, then
// alternating right/left inward — and replaces the dropped contiguous middle
// with one "…".
func TestElidePath(t *testing.T) {
	const p = "/aa/bb/cc/dd/ee" // 15 cols, 5 segments
	cases := []struct {
		in   string
		n    int
		want string
	}{
		{p, 20, p},                // fits: unchanged
		{p, 14, "/aa/…/cc/dd/ee"}, // dropped: bb
		{p, 11, "/aa/…/dd/ee"},    // dropped: bb cc
		{p, 10, "…/cc/dd/ee"},     // first segment no longer fits either
		{p, 6, "…/ee"},            // only the name fits
		{p, 3, "ee"},              // not even "…/ee": the bare name still fits
		{p, 1, "…"},
		// The right side closing must not close the left: bbbb is too wide,
		// aa still joins.
		{"/aa/bbbb/cc", 10, "…/bbbb/cc"},
		{"/aa/bbbbbb/cc", 9, "/aa/…/cc"},
		// Windows separators survive as written.
		{`C:\repos\deep\thing`, 10, `C:\…\thing`},
		// A trailing separator (directory heading) is preserved.
		{"internal/tui/some/", 12, "…/tui/some/"},
		// Single segment: cut inside the name (beginning + ending survive).
		{"justonename", 6, "just…e"},
	}
	for _, c := range cases {
		if got := elidePath(c.in, c.n); got != c.want {
			t.Errorf("elidePath(%q, %d) = %q, want %q", c.in, c.n, got, c.want)
		}
		if w := lipgloss.Width(elidePath(c.in, c.n)); w > c.n && c.n > 0 {
			t.Errorf("elidePath(%q, %d) overflows: width %d", c.in, c.n, w)
		}
	}
}

// elideNameMiddle keeps the name's beginning and its extension (or ending).
func TestElideNameMiddle(t *testing.T) {
	cases := []struct {
		in   string
		n    int
		want string
	}{
		{"short.go", 20, "short.go"},
		{"averylongfilename.go", 10, "averyl….go"},
		// No extension: the ending gets a third of the budget.
		{"some-extremely-long-repo-directory-name", 10, "some-e…ame"},
		// An extension too wide to be useful degrades to the ending share.
		{"name.superlongextension", 8, "name.…on"},
		{"x", 1, "x"},
		{"xyz", 1, "…"},
	}
	for _, c := range cases {
		if got := elideNameMiddle(c.in, c.n); got != c.want {
			t.Errorf("elideNameMiddle(%q, %d) = %q, want %q", c.in, c.n, got, c.want)
		}
	}
}
