package domain

import (
	"context"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/syntax"
)

func blameLines(content ...string) []model.BlameLine {
	out := make([]model.BlameLine, len(content))
	for i, c := range content {
		out[i] = model.BlameLine{LineNo: i + 1, Content: c}
	}
	return out
}

// blameToks is the Highlighter's blame runs with highlighting on and no
// cache — what the blame surfaces show.
func blameToks(t *testing.T, path string, lines []model.BlameLine) [][]syntax.Tok {
	t.Helper()
	r, err := NewHighlighter(func() bool { return true }, nil).Blame(context.Background(), path, lines, TokInline)
	if err != nil {
		t.Fatal(err)
	}
	return r.Tok
}

func TestBlameRunsKnownLanguage(t *testing.T) {
	t.Parallel()
	tok := blameToks(t, "a.go", blameLines("package main", "", "func main() {}"))
	if len(tok) != 3 {
		t.Fatalf("want one token slice per line, got %d: %+v", len(tok), tok)
	}
	if len(tok[0]) == 0 || tok[0][0].Class != syntax.Keyword {
		t.Errorf("`package` should lex as a keyword, got %+v", tok[0])
	}
	// Line 3 follows a blank line: numbering must not drift.
	if len(tok[2]) == 0 {
		t.Errorf("line 3 (`func main() {}`) should carry runs: %+v", tok)
	}
}

func TestBlameRunsSkips(t *testing.T) {
	t.Parallel()
	if got := blameToks(t, "a.unknownext", blameLines("package main")); got != nil {
		t.Errorf("no lexer → nil, got %+v", got)
	}
	if got := blameToks(t, "a.go", nil); got != nil {
		t.Errorf("no lines → nil, got %+v", got)
	}
	if got := blameToks(t, "a.go", blameLines("package main\rvar x = 1")); got != nil {
		t.Errorf("a bare \\r → nil, got %+v", got)
	}
	// CRLF content stays coloured: the trailing \n keeps the last \r paired.
	if got := blameToks(t, "a.go", blameLines("package main\r", "var x = 1\r")); len(got) != 2 {
		t.Errorf("CRLF content should lex to 2 lines, got %+v", got)
	}
	big := blameLines(strings.Repeat("x", MaxSyntaxBytes))
	if got := blameToks(t, "a.go", big); got != nil {
		t.Errorf("content past MaxSyntaxBytes → nil")
	}
}

func TestHasBareCR(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		in   string
		want bool
	}{
		{"a\nb\n", false},
		{"a\r\nb\r\n", false},
		{"a\rb\n", true},
		{"a\r\n\rb", true},
	} {
		if got := HasBareCR([]byte(c.in)); got != c.want {
			t.Errorf("HasBareCR(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}
