package overviewhtml

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/syntax"
	"github.com/homeend/gigagit/internal/textdiff"
)

func TestToksDropsUnstyledAndOutOfRange(t *testing.T) {
	t.Parallel()
	side := [][]syntax.Tok{{{Start: 0, End: 2, Class: syntax.Keyword}, {Start: 3, End: 4, Class: syntax.Name}}}
	got := Toks(side, 1)
	if len(got) != 1 || got[0][0] != 0 || got[0][1] != 2 || got[0][2] != "kw" {
		t.Fatalf("Toks = %v", got)
	}
	if Toks(side, 0) != nil || Toks(side, 2) != nil || Toks(nil, 1) != nil {
		t.Fatal("out of range must be nil")
	}
}

func TestRowsMatchTheDiffWire(t *testing.T) {
	t.Parallel()
	rows := []textdiff.Row{
		{Kind: textdiff.Same, Left: "a", Right: "a", LeftNo: 1, RightNo: 1},
		{Kind: textdiff.Changed, Left: "b", Right: "B", LeftNo: 2, RightNo: 2, LeftSpans: []textdiff.Span{{Start: 0, End: 1}}},
		{Kind: textdiff.Add, Right: "c", RightNo: 3},
	}
	b, _ := json.Marshal(Rows(rows, nil, nil))
	want := `[{"kind":"same","left":"a","right":"a","left_no":1,"right_no":1},` +
		`{"kind":"change","left":"b","right":"B","left_no":2,"right_no":2,"left_spans":[[0,1]]},` +
		`{"kind":"add","left":"","right":"c","left_no":0,"right_no":3}]`
	if string(b) != want {
		t.Fatalf("got  %s\nwant %s", b, want)
	}
}

func TestLinesCarryTheirRuns(t *testing.T) {
	t.Parallel()
	got := Lines([]string{"if x", "y"}, [][]syntax.Tok{{{Start: 0, End: 2, Class: syntax.Keyword}}})
	if len(got) != 2 || got[0].Text != "if x" || len(got[0].Tok) != 1 || got[1].Tok != nil {
		t.Fatalf("Lines = %+v", got)
	}
}

func TestSlug(t *testing.T) {
	t.Parallel()
	for in, want := range map[string]string{
		"Review overview: main 1a2b3c4": "review-overview-main-1a2b3c4",
		"  ":                             "overview",
		"Ünïcode / why?":                 "n-code-why",
	} {
		if got := Slug(in); got != want {
			t.Errorf("Slug(%q) = %q, want %q", in, got, want)
		}
	}
	if got := Slug(strings.Repeat("ab ", 100)); len(got) > 60 || strings.HasSuffix(got, "-") {
		t.Errorf("too long or dangling dash: %q", got)
	}
}
