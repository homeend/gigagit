package engine

import "testing"

func partsText(r Result) string {
	var s string
	for _, p := range r.SummaryParts {
		s += p.text()
	}
	return s
}

func TestWithSummaryLockstep(t *testing.T) {
	t.Parallel()
	r := Result{Changed: true}.WithSummary("created branch %s", "feat/x")
	if r.Summary != "created branch feat/x" {
		t.Fatalf("Summary = %q", r.Summary)
	}
	if !r.Changed {
		t.Fatal("WithSummary must preserve other fields")
	}
	if len(r.SummaryParts) != 1 || r.SummaryParts[0].Format != "created branch %s" {
		t.Fatalf("SummaryParts = %#v", r.SummaryParts)
	}
	if partsText(r) != r.Summary {
		t.Fatalf("invariant broken: parts %q vs summary %q", partsText(r), r.Summary)
	}
}

func TestAppendSummaryLockstep(t *testing.T) {
	t.Parallel()
	r := Result{}.WithSummary("rebased %s onto %s", "a", "b").
		AppendSummary(" (your changes remain stashed)")
	want := "rebased a onto b (your changes remain stashed)"
	if r.Summary != want {
		t.Fatalf("Summary = %q, want %q", r.Summary, want)
	}
	if len(r.SummaryParts) != 2 || partsText(r) != r.Summary {
		t.Fatalf("parts = %#v", r.SummaryParts)
	}
}

func TestAppendSummaryOnFreshResult(t *testing.T) {
	t.Parallel()
	r := Result{}.AppendSummary("cancelled")
	if r.Summary != "cancelled" || len(r.SummaryParts) != 1 {
		t.Fatalf("Summary=%q parts=%#v", r.Summary, r.SummaryParts)
	}
}

func TestAppendSummaryLegacyEnglishFallback(t *testing.T) {
	t.Parallel()
	// A hand-built (no-parts) summary must NOT gain a partial parts slice:
	// the whole summary falls back to English at render.
	r := Result{Summary: "hand-built"}.AppendSummary("; suffix")
	if r.Summary != "hand-built; suffix" {
		t.Fatalf("Summary = %q", r.Summary)
	}
	if len(r.SummaryParts) != 0 {
		t.Fatalf("legacy append must keep parts empty, got %#v", r.SummaryParts)
	}
}

func TestPercentInArgsIsSafe(t *testing.T) {
	t.Parallel()
	r := Result{}.WithSummary("committed %s %s", "abc1234", "raise coverage to 100%")
	if r.Summary != "committed abc1234 raise coverage to 100%" {
		t.Fatalf("Summary = %q", r.Summary)
	}
}

func TestNoArgsFormatIsVerbatim(t *testing.T) {
	t.Parallel()
	// Zero args = no Sprintf pass, so a literal % survives (mirrors i18n.T).
	r := Result{}.WithSummary("100% done")
	if r.Summary != "100% done" {
		t.Fatalf("Summary = %q", r.Summary)
	}
}

func TestProgressfLockstep(t *testing.T) {
	t.Parallel()
	p := Progressf("rebasing", "%s onto %s", "feat/x", "main")
	if p.Step != "rebasing" || p.Detail != "feat/x onto main" {
		t.Fatalf("p = %#v", p)
	}
	if p.DetailMsg.Format != "%s onto %s" || len(p.DetailMsg.Args) != 2 {
		t.Fatalf("DetailMsg = %#v", p.DetailMsg)
	}
}

func TestPromptReqLockstep(t *testing.T) {
	t.Parallel()
	req := PromptReq("branch.delete", "Delete branch %s?", []string{"delete", "cancel"}, "feat/x")
	if req.ID != "branch.delete" || req.Prompt != "Delete branch feat/x?" {
		t.Fatalf("req = %#v", req)
	}
	if req.PromptMsg.Format != "Delete branch %s?" || len(req.Options) != 2 {
		t.Fatalf("req = %#v", req)
	}
}
