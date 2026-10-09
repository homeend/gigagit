package domain

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/homeend/gigagit/internal/agentdocs"
	"github.com/homeend/gigagit/internal/notebatch"
)

// The two 64 KiB caps are one number (notebatch cannot import agentdocs).
func TestOverviewLimitsAgree(t *testing.T) {
	if notebatch.MaxOverviewBytes != agentdocs.MaxOverviewBytes {
		t.Fatalf("notebatch %d != agentdocs %d", notebatch.MaxOverviewBytes, agentdocs.MaxOverviewBytes)
	}
}

const overviewDoc = `{"version":1,"summary":"looks fine",
"overview":"Lines 5 and 25 changed.\n\n[the first](big.go:5) and [the second](big.go:24-25); [not in the PR](other.go:1); [past the end](big.go:900); [a note](note:t3); [the file](big.go)",
"files":[{"path":"big.go","annotations":[{"newRange":[5,5],"summary":"check this"}]}]}`

func TestReviewOverviewResolvesAgainstTheTip(t *testing.T) {
	t.Parallel()
	svc, _, _ := sendRepo(t)
	rid := savePRReview(t, svc, overviewDoc)
	ov, err := svc.ReviewOverview(context.Background(), rid)
	if err != nil {
		t.Fatal(err)
	}
	var ok, bad []string
	for _, a := range ov.Anchors {
		if a.OK {
			ok = append(ok, a.Dest)
		} else {
			bad = append(bad, a.Dest)
		}
	}
	if !slices.Equal(ok, []string{"big.go:5", "big.go:24-25", "big.go"}) {
		t.Errorf("resolved = %v", ok)
	}
	if !slices.Equal(bad, []string{"other.go:1", "big.go:900", "note:t3"}) {
		t.Errorf("plain = %v", bad)
	}
	if !slices.Equal(ov.Unresolved(), bad) {
		t.Errorf("Unresolved() = %v", ov.Unresolved())
	}
	if ov.Text == "" || len(ov.Doc.Blocks) == 0 {
		t.Errorf("text/doc empty: %+v", ov)
	}
}

func TestReviewOverviewAbsent(t *testing.T) {
	t.Parallel()
	svc, _, _ := sendRepo(t)
	rid := savePRReview(t, svc, twoRemarks)
	if _, err := svc.ReviewOverview(context.Background(), rid); !errors.Is(err, ErrNoOverview) {
		t.Fatalf("err = %v, want ErrNoOverview", err)
	}
}

// Review Focus 4: an anchor names the NEW path of a renamed file; the old
// path is not a file of the review and draws plain.
func TestReviewOverviewRenamedFile(t *testing.T) {
	t.Parallel()
	dir, _ := newRealRepo(t)
	commitFile(t, dir, "old.go", "package a\nfunc A() {}\n", "add")
	runGitIn(t, dir, "mv", "old.go", "new.go")
	runGitIn(t, dir, "commit", "-q", "-m", "rename")
	tip := revParse(t, dir, "HEAD")
	_, svc := newRealRepoAt(t, dir)
	svc.UseNotesDir(t.TempDir())
	tg := ReviewTarget{Kind: ReviewRange, Range: tip + "^.." + tip, Label: tip[:7]}
	rid, _, err := svc.SaveReview(context.Background(), SaveReview{Target: tg, Agent: "c",
		Text: `{"version":1,"summary":"s","overview":"[new](new.go:2) [old](old.go:2)","files":[]}`})
	if err != nil {
		t.Fatal(err)
	}
	ov, err := svc.ReviewOverview(context.Background(), rid)
	if err != nil {
		t.Fatal(err)
	}
	if !ov.Anchors[0].OK || ov.Anchors[1].OK {
		t.Fatalf("anchors = %+v", ov.Anchors)
	}
}
