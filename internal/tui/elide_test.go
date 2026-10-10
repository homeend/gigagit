package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
)

// elidePath keeps segments by priority — name, rightmost dir, first dir, then
// alternating right/left inward — and replaces the dropped contiguous middle
// with one "…".
func TestElidePath(t *testing.T) {
	t.Parallel()
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
		// The directory holding the name, too wide to keep whole, is cut in
		// its middle rather than dropped, so sibling files stay apart; the
		// first segment joins it while the cut keeps parentMin columns.
		{"/aa/abcdefghijklmnopqrst/cc.go", 24, "/aa/abcdefghi…qrst/cc.go"},
		{"/aa/xx/abcdefghijklmnopqrst/cc.go", 26, "/aa/…/abcdefghi…qrst/cc.go"},
		{"/aaaaaaaa/abcdefghijklmnopqrst/cc.go", 24, "…/abcdefghij…pqrst/cc.go"},
		{"abcdefghijklmnopqrst/cc.go", 20, "abcdefghi…qrst/cc.go"},
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
	t.Parallel()
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

// Three byte-identical bench.js files in sibling benchmark directories: a
// frame title too narrow for the directory whole keeps a middle cut of it,
// so the titles still tell the files apart (a review overview's anchors
// stepping between them looked like nothing moved).
func TestElidePathKeepsSiblingDirsApart(t *testing.T) {
	t.Parallel()
	dirs := []string{
		"load-config-target-defaults-preset-env-typescript",
		"load-config-assumptions-target-defaults-preset-env-plugin-typescript-decorators",
	}
	seen := map[string]bool{}
	for _, d := range dirs {
		title := "View benchmark/babel-core/" + d + "/bench.js @ 774ace5"
		got := elidePath(title, 60)
		if lipgloss.Width(got) > 60 || !strings.HasSuffix(got, "/bench.js @ 774ace5") {
			t.Fatalf("elidePath(%q, 60) = %q: want the name whole within 60 columns", title, got)
		}
		if seen[got] {
			t.Fatalf("two sibling directories elide to the same title %q", got)
		}
		seen[got] = true
	}
}
