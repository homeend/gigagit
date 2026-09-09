package notebatch

import (
	"strings"
	"testing"
)

func TestParseAgentContextV1(t *testing.T) {
	t.Parallel()
	in := `{"version":1,"summary":"overall: solid",
	  "files":[{"path":"src/search.ts","summary":"scoring changed",
	    "annotations":[
	      {"newRange":[15,23],"summary":"prefix beats substring","rationale":"why","author":"sonnet","tags":["perf"],"confidence":"high","markup":"<b>ignored</b>"},
	      {"oldRange":[40,40],"summary":"dead branch"}
	    ]}]}`
	b, err := Parse([]byte(in))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(b.Items) != 2 {
		t.Fatalf("items = %+v, want 2", b.Items)
	}
	i0 := b.Items[0]
	if i0.Path != "src/search.ts" || i0.Target.NewLine != [2]int{15, 23} || i0.Summary != "prefix beats substring" ||
		i0.Rationale != "why" || i0.Author != "sonnet" || len(i0.Tags) != 1 || i0.Confidence != 0.9 {
		t.Fatalf("item 0 = %+v", i0)
	}
	if b.Items[1].Target.OldLine != [2]int{40, 40} || b.Items[1].Target.NewLine != [2]int{0, 0} {
		t.Fatalf("item 1 = %+v, want an old-side anchor only", b.Items[1])
	}
	// Top-level and per-file summaries have no anchor: they are CONTEXT, echoed
	// to stderr by the caller, never stored as notes.
	if len(b.Contexts) != 2 || !strings.Contains(b.Contexts[0], "overall: solid") ||
		!strings.Contains(b.Contexts[1], "scoring changed") {
		t.Fatalf("contexts = %q, want the top-level and file summaries", b.Contexts)
	}
}

func TestParseAgentContextNewRangeWinsOverOld(t *testing.T) {
	t.Parallel()
	b, err := Parse([]byte(`{"files":[{"path":"a.go","annotations":[{"oldRange":[1,2],"newRange":[5,6],"summary":"s"}]}]}`))
	if err != nil {
		t.Fatal(err)
	}
	got := b.Items[0].Target
	if got.NewLine != [2]int{5, 6} || got.OldLine != [2]int{0, 0} {
		t.Fatalf("target = %+v, want the new range to win", got)
	}
}

func TestParseCommentApplyShape(t *testing.T) {
	t.Parallel()
	in := `{"comments":[
	  {"filePath":"a.go","newLine":12,"summary":"one"},
	  {"filePath":"b.go","hunk":3,"summary":"two","rationale":"r","author":"gpt"},
	  {"filePath":"c.go","hunkNumber":2,"summary":"three"},
	  {"filePath":"d.go","oldLine":7,"summary":"four"},
	  {"replyTo":"a1b2c3d4","summary":"addressed"}
	]}`
	b, err := Parse([]byte(in))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(b.Items) != 5 {
		t.Fatalf("items = %d, want 5", len(b.Items))
	}
	if b.Items[0].Target.NewLine != [2]int{12, 12} {
		t.Fatalf("newLine → single-line range: %+v", b.Items[0].Target)
	}
	if b.Items[1].Target.Hunk != 3 || b.Items[2].Target.Hunk != 2 {
		t.Fatalf("hunk/hunkNumber must both set Target.Hunk: %+v %+v", b.Items[1].Target, b.Items[2].Target)
	}
	if b.Items[3].Target.OldLine != [2]int{7, 7} {
		t.Fatalf("oldLine → single-line range: %+v", b.Items[3].Target)
	}
	if b.Items[4].ReplyTo != "a1b2c3d4" || b.Items[4].Target.IsSet() || b.Items[4].Path != "" {
		t.Fatalf("a reply names no file and no target: %+v", b.Items[4])
	}
}

func TestParseRejectsBadBatches(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"neither key", `{"notes":[]}`, `"files"`},
		{"both keys", `{"files":[],"comments":[]}`, "exactly one"},
		{"not json", `nope`, "invalid JSON"},
		{"file without path", `{"files":[{"annotations":[{"newRange":[1,1],"summary":"s"}]}]}`, "non-empty path"},
		{"annotation without summary", `{"files":[{"path":"a.go","annotations":[{"newRange":[1,1]}]}]}`, "summary"},
		{"annotation without range", `{"files":[{"path":"a.go","annotations":[{"summary":"s"}]}]}`, "oldRange or newRange"},
		{"range not ordered", `{"files":[{"path":"a.go","annotations":[{"newRange":[9,2],"summary":"s"}]}]}`, "ordered"},
		{"range not 1-based", `{"files":[{"path":"a.go","annotations":[{"newRange":[0,3],"summary":"s"}]}]}`, "1-based"},
		{"comment with two targets", `{"comments":[{"filePath":"a.go","newLine":1,"oldLine":2,"summary":"s"}]}`, "exactly one of"},
		{"comment with no target", `{"comments":[{"filePath":"a.go","summary":"s"}]}`, "exactly one of"},
		{"comment with no summary", `{"comments":[{"filePath":"a.go","newLine":1}]}`, "summary"},
		{"unsupported version", `{"version":2,"files":[]}`, "version"},
	}
	for _, c := range cases {
		_, err := Parse([]byte(c.in))
		if err == nil {
			t.Errorf("%s: want an error", c.name)
			continue
		}
		if !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: err = %q, want it to contain %q", c.name, err, c.want)
		}
	}
}

// One bad item rejects the WHOLE batch: nothing may be stored from a batch that
// does not fully validate.
func TestParseOneBadItemRejectsEverything(t *testing.T) {
	t.Parallel()
	in := `{"files":[{"path":"a.go","annotations":[
	  {"newRange":[1,1],"summary":"fine"},
	  {"newRange":[2,2]}
	]}]}`
	if _, err := Parse([]byte(in)); err == nil || !strings.Contains(err.Error(), "files[0].annotations[1]") {
		t.Fatalf("err = %v, want a rejection naming the bad item's index", err)
	}
}

func TestParseConfidenceWords(t *testing.T) {
	t.Parallel()
	for word, want := range map[string]float64{"low": 0.3, "medium": 0.6, "high": 0.9, "bogus": 0} {
		in := `{"files":[{"path":"a.go","annotations":[{"newRange":[1,1],"summary":"s","confidence":"` + word + `"}]}]}`
		b, err := Parse([]byte(in))
		if err != nil {
			t.Fatalf("%s: %v", word, err)
		}
		if b.Items[0].Confidence != want {
			t.Errorf("confidence %q = %v, want %v", word, b.Items[0].Confidence, want)
		}
	}
}
