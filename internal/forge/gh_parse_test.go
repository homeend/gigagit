package forge

import (
	"errors"
	"os"
	"testing"

	"github.com/homeend/gigagit/internal/model"
)

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestParsePRList(t *testing.T) {
	t.Parallel()
	prs, err := parsePRList(fixture(t, "pr-list.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(prs) != 2 {
		t.Fatalf("len = %d, want 2", len(prs))
	}
	a, b := prs[0], prs[1]
	if a.Number != 7 || a.Author != "alice" || a.State != model.PRStateOpen ||
		a.ReviewState != "changes_requested" || a.Source != "feat/forge" || a.SourceRepo != "" ||
		a.Target != "main" || a.HeadSHA[:1] != "2" || a.BaseSHA[:1] != "1" || a.Updated.IsZero() {
		t.Errorf("pr 7 = %+v", a)
	}
	if !b.Draft || b.SourceRepo != "bob/gigagit" || b.ReviewState != "" {
		t.Errorf("pr 9 = %+v", b)
	}
}

func TestParsePRViewMergedAndBody(t *testing.T) {
	t.Parallel()
	p, err := parsePR(fixture(t, "pr-view-7.json"))
	if err != nil {
		t.Fatal(err)
	}
	if p.State != model.PRStateMerged {
		t.Errorf("state = %q", p.State)
	}
	if p.Body != "Adds the tab.\n\nSecond paragraph." { // CRLF normalised
		t.Errorf("body = %q", p.Body)
	}
}

func TestParseThreads(t *testing.T) {
	t.Parallel()
	cs, truncated, err := parseThreads(fixture(t, "threads-7.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !truncated {
		t.Error("truncated = false, want true (reviews.hasNextPage)")
	}
	by := map[string]model.ForgeComment{}
	for _, c := range cs {
		by[c.ID] = c
	}
	if c := by["C1"]; c.Kind != model.ForgeCommentInline || c.Path != "a.go" || c.Line != 12 ||
		c.StartLine != 10 || c.Side != model.NoteSideNew || c.ParentID != "" {
		t.Errorf("C1 = %+v", c)
	}
	if c := by["C2"]; c.ParentID != "C1" || c.Line != 12 {
		t.Errorf("C2 = %+v", c)
	}
	if c := by["C3"]; c.Kind != model.ForgeCommentFile || !c.Resolved || c.Line != 0 {
		t.Errorf("C3 = %+v", c)
	}
	// An outdated thread has no CURRENT line; it keeps the original one so a
	// reader can still tell where the remark was made.
	if c := by["C4"]; !c.Outdated || c.Side != model.NoteSideOld || c.Author != "ghost" || c.Hunk == "" ||
		c.Line != 6 || c.StartLine != 5 {
		t.Errorf("C4 = %+v", c)
	}
	if c := by["G1"]; c.Kind != model.ForgeCommentGeneral {
		t.Errorf("G1 = %+v", c)
	}
	if _, ok := by["R1"]; ok {
		t.Error("R1 (COMMENTED, empty body) must be dropped — it is only the envelope of inline comments")
	}
	if c := by["R2"]; c.Kind != model.ForgeCommentReview || c.Verdict != "changes_requested" {
		t.Errorf("R2 = %+v", c)
	}
	if c := by["R3"]; c.Verdict != "approved" { // empty body kept: a verdict is news
		t.Errorf("R3 = %+v", c)
	}
}

func TestParseThreadsNullPullRequestIsNotFound(t *testing.T) {
	t.Parallel()
	_, _, err := parseThreads([]byte(`{"data":{"repository":{"pullRequest":null}}}`))
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
}
