package web

import (
	"testing"

	"github.com/homeend/gigagit/internal/domain"
)

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
