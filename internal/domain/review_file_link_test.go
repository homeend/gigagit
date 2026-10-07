package domain

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/model"
)

func TestReviewFileLink(t *testing.T) {
	t.Parallel()
	svc, _, single, rng := reviewLinkFixture(t)
	ctx := context.Background()
	for _, id := range []string{single, rng} {
		whole, err := svc.ReviewLink(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		fl, err := svc.ReviewFileLink(ctx, id, "a.txt")
		if err != nil {
			t.Fatalf("ReviewFileLink: %v", err)
		}
		// The whole review's link with the path spliced in before its target.
		base, rest, _ := strings.Cut(whole, "@")
		if fl != base+"/a.txt@"+rest {
			t.Fatalf("file link %q, whole %q", fl, whole)
		}
		if l := mustParse(t, fl); l.Line != 0 || l.Hint.Kind != model.ReviewHintKind || l.Hint.ID != id {
			t.Fatalf("file link %q parses as %+v", fl, l)
		}
	}
	if _, err := svc.ReviewFileLink(ctx, "deadbeef", "a.txt"); !errors.Is(err, ErrReviewNotFound) {
		t.Fatalf("unknown review: %v", err)
	}
}

func TestReviewRemarkLink(t *testing.T) {
	t.Parallel()
	svc, _, _, rng := reviewLinkFixture(t)
	ctx := context.Background()
	rl, err := svc.ReviewRemarkLink(ctx, model.ReviewNoteIDPrefix+rng+":0")
	if err != nil {
		t.Fatal(err)
	}
	if l := mustParse(t, rl); !strings.Contains(rl, "/a.txt@") || l.Line != 1 || l.End != 2 || l.Side != model.NoteSideNew || l.Hint.ID != rng {
		t.Fatalf("remark 0 link %q = %+v", rl, l)
	}
	old, err := svc.ReviewRemarkLink(ctx, model.ReviewNoteIDPrefix+rng+":1")
	if err != nil {
		t.Fatal(err)
	}
	if l := mustParse(t, old); !strings.HasSuffix(old, ":old:1?review="+rng) || l.Side != model.NoteSideOld || l.Hint.ID != rng {
		t.Fatalf("old-side remark link %q = %+v", old, l)
	}
	show, err := svc.ReviewShow(ctx, rng)
	if err != nil {
		t.Fatal(err)
	}
	if show.Remarks[0].ReviewLink != rl || show.Remarks[0].Link == rl || show.Remarks[1].ReviewLink != old {
		t.Fatalf("ReviewShow remarks %+v", show.Remarks)
	}
	if _, err := svc.ReviewRemarkLink(ctx, model.ReviewNoteIDPrefix+rng+":99"); err == nil || !strings.Contains(err.Error(), "no remark 99") {
		t.Fatalf("unknown remark: %v", err)
	}
	if _, err := svc.ReviewRemarkLink(ctx, "x"); err == nil || !strings.Contains(err.Error(), "not a review remark id") {
		t.Fatalf("malformed id: %v", err)
	}
}

// A working-changes review has no commit; its file link still builds.
func TestReviewFileLinkOnAWorkingReview(t *testing.T) {
	t.Parallel()
	_, svc, _ := reviewRepo(t)
	ctx := context.Background()
	files := []model.NoteFile{{Path: "f.txt", Blob: strings.Repeat("a", 40)}}
	id, _, err := svc.SaveReview(ctx, SaveReview{Target: WorkingReviewTarget(), Agent: "Claude Code", Text: linkReviewDoc, Files: files})
	if err != nil {
		t.Fatal(err)
	}
	fl, err := svc.ReviewFileLink(ctx, id, "f.txt")
	if err != nil {
		t.Fatalf("working file link: %v", err)
	}
	if l := mustParse(t, fl); !strings.HasSuffix(fl, "/f.txt?review="+id) || l.Hint.ID != id {
		t.Fatalf("working file link %q = %+v", fl, l)
	}
}

// A file link names a file the review holds: one its change touched or one
// its document (or a working review's fingerprint) names. Any other path
// is refused — the link would open the review on nothing.
func TestReviewFileLinkRefusesAPathNotInTheReview(t *testing.T) {
	t.Parallel()
	svc, dir, single, rng := reviewLinkFixture(t)
	ctx := context.Background()
	for _, id := range []string{single, rng} {
		_, err := svc.ReviewFileLink(ctx, id, "nope.txt")
		if !errors.Is(err, ErrNotInReview) || !strings.Contains(err.Error(), "nope.txt is not in review "+id) {
			t.Fatalf("%s: %v", id, err)
		}
	}
	// b.txt changed with a.txt but has no remark; ghost.txt has a remark
	// but its change is elsewhere. Both are the review's.
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("z\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "b.txt"), []byte("b\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, dir, "add", ".")
	gitRun(t, dir, "commit", "-q", "-m", "two files")
	head := strings.TrimSpace(gitOut(t, dir, "rev-parse", "HEAD"))
	doc := `{"version":1,"summary":"ok","files":[{"path":"ghost.txt","annotations":[{"newRange":[1,1],"summary":"s"}]}]}`
	id, _, err := svc.SaveReview(ctx, SaveReview{Target: ReviewTarget{Kind: ReviewRange, Range: head + "^.." + head, Commit: head}, Agent: "Claude", Text: doc})
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{"a.txt", "b.txt", "ghost.txt"} {
		if _, err := svc.ReviewFileLink(ctx, id, p); err != nil {
			t.Errorf("%s: %v", p, err)
		}
	}
	if _, err := svc.ReviewFileLink(ctx, id, "c.txt"); !errors.Is(err, ErrNotInReview) {
		t.Errorf("c.txt: %v", err)
	}
}
