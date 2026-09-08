package web

import (
	"os"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/syntax"
)

// TestTokTriplesDropsUnstyledRuns pins the wire trim: a run whose class the
// stylesheet does not paint costs bytes and splits a renderCell run for
// nothing, so it is left out — and a line of nothing but such runs sends no
// array at all.
func TestTokTriplesDropsUnstyledRuns(t *testing.T) {
	t.Parallel()
	side := [][]syntax.Tok{
		{{Start: 0, End: 3, Class: syntax.Name}},
		{{Start: 0, End: 3, Class: syntax.Keyword}, {Start: 4, End: 7, Class: syntax.Name}, {Start: 8, End: 9, Class: syntax.Number}},
	}
	if got := tokTriples(side, 1); got != nil {
		t.Errorf("a line of only unstyled runs must send nothing, got %v", got)
	}
	got := tokTriples(side, 2)
	want := []tokTriple{{0, 3, "kw"}, {8, 9, "num"}}
	if len(got) != len(want) {
		t.Fatalf("triples = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("triples = %v, want %v", got, want)
		}
	}
}

// TestEveryWiredClassHasCSS keeps unstyledOnWire honest: every class that is
// NOT trimmed must have a .tk-* rule in style.css, or the browser renders a
// span the wire paid for and nobody sees.
func TestEveryWiredClassHasCSS(t *testing.T) {
	t.Parallel()
	css, err := os.ReadFile("static/style.css")
	if err != nil {
		t.Fatal(err)
	}
	for c := syntax.Class(1); c <= syntax.Attr; c++ {
		if unstyledOnWire[c] {
			continue
		}
		if rule := ".tk-" + c.String(); !strings.Contains(string(css), rule) {
			t.Errorf("class %q is sent on the wire but style.css has no %q rule — add the rule or add the class to unstyledOnWire", c.String(), rule)
		}
	}
}

// TestDiffJSONCarriesSyntaxRuns checks that /api/diff's rows carry left_tok/
// right_tok arrays of [start, end, class] for a file with a known lexer, and
// that a side with no old line (an all-add row) omits left_tok entirely.
func TestDiffJSONCarriesSyntaxRuns(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	gitRun(t, dir, "init", "-b", "main")
	write(t, dir, "a.go", "package a\nvar x = 1\n")
	gitRun(t, dir, "add", "-A")
	gitRun(t, dir, "commit", "-m", "add a.go")
	sha := gitRun(t, dir, "rev-parse", "HEAD")
	ts := serve(t, New(domain.Open(dir)))

	var rr map[string]any
	if code := getJSON(t, ts, "/api/diff?sha="+sha+"&path=a.go&status=A", &rr); code != 200 {
		t.Fatalf("code = %d", code)
	}
	rows := rr["rows"].([]any)
	second := rows[1].(map[string]any)
	toks, ok := second["right_tok"].([]any)
	if !ok || len(toks) == 0 {
		t.Fatalf("row 2 should carry right_tok, got %v", second)
	}
	first := toks[0].([]any)
	if first[2] != "kw" || first[0].(float64) != 0 || first[1].(float64) != 3 {
		t.Errorf("first run should be [0,3,\"kw\"] for `var`, got %v", first)
	}
	if _, has := second["left_tok"]; has {
		t.Errorf("an all-add row has no old line, so no left_tok: %v", second)
	}
}

// TestFilesJSRoutesEveryDiffCellThroughRenderCell guards against a future
// diffHTML branch (e.g. a new layout, or the unified narrow-viewport "same"
// row that once slipped through as a bare esc(...) call) reverting to plain
// text instead of going through renderCell, which is what actually paints
// left_tok/right_tok's syntax runs.
func TestFilesJSRoutesEveryDiffCellThroughRenderCell(t *testing.T) {
	t.Parallel()
	src, err := os.ReadFile("static/files.js")
	if err != nil {
		t.Fatal(err)
	}
	s := string(src)
	if n := strings.Count(s, "renderCell("); n < 6 {
		t.Errorf("renderCell( call count = %d, want >= 6 (every diff cell in every layout)", n)
	}
	if strings.Contains(s, "markSpans(") {
		t.Error("markSpans( still present — a diff cell is bypassing renderCell's syntax-run rendering")
	}
}
