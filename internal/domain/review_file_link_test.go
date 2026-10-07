package domain

import (
	"context"
	"errors"
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
