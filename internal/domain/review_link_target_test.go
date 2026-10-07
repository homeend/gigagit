package domain

import (
	"context"
	"errors"
	"testing"

	"github.com/homeend/gigagit/internal/model"
)

func TestLinkReviewTargetKinds(t *testing.T) {
	t.Parallel()
	dir, svc, set := previewReviewRepo(t)
	ctx := context.Background()
	base := revParse(t, dir, "main")

	tg, err := svc.LinkReviewTarget(ctx, Resolved{Preview: &set, Commit: set.Tip})
	if err != nil || tg.Preview != "main...feat/x" {
		t.Fatalf("preview: %+v %v", tg, err)
	}
	tg, err = svc.LinkReviewTarget(ctx, Resolved{Pair: &model.LinkPair{A: base, B: set.Tip}, Commit: set.Tip})
	if err != nil || tg.Preview != base[:7]+".."+set.Tip[:7] || tg.Range != base+".."+set.Tip {
		t.Fatalf("pair: %+v %v", tg, err)
	}
	tg, err = svc.LinkReviewTarget(ctx, Resolved{Ref: "feat/x", Commit: set.Tip,
		Addr: model.FileAddress{State: model.StateCommitted, Commit: set.Tip}})
	if err != nil || tg.Kind != ReviewBranch || tg.Branch != "feat/x" || tg.Range != base+".."+set.Tip {
		t.Fatalf("ref: %+v %v", tg, err)
	}
	tg, err = svc.LinkReviewTarget(ctx, Resolved{Commit: set.Tip,
		Addr: model.FileAddress{State: model.StateCommitted, Commit: set.Tip, Path: "f.txt"}})
	if err != nil || tg.Range != set.Tip+"^.."+set.Tip || tg.Preview != "" || tg.Label != set.Tip[:8]+" feature commit" {
		t.Fatalf("commit (with a path): %+v %v", tg, err)
	}
	tg, err = svc.LinkReviewTarget(ctx, Resolved{Addr: model.FileAddress{State: model.StateUnstaged, Path: "f.txt"}})
	if err != nil || tg.Kind != ReviewWorking {
		t.Fatalf("working file: %+v %v", tg, err)
	}
	tg, err = svc.LinkReviewTarget(ctx, Resolved{Addr: model.FileAddress{State: model.StateStaged}})
	if err != nil || tg.Kind != ReviewWorking {
		t.Fatalf("@staged: %+v %v", tg, err)
	}
	if _, err = svc.LinkReviewTarget(ctx, Resolved{}); !errors.Is(err, ErrNoReviewChange) {
		t.Fatalf("a bare repo link: %v, want ErrNoReviewChange", err)
	}
	if _, err = svc.LinkReviewTarget(ctx, Resolved{Hint: model.LinkHint{Kind: "bookmark", ID: "x"}}); !errors.Is(err, ErrNoReviewChange) {
		t.Fatalf("a hint-only link: %v, want ErrNoReviewChange", err)
	}
}

func TestCommitReviewTargetRootCommit(t *testing.T) {
	t.Parallel()
	dir, svc := newRealRepo(t)
	root := revParse(t, dir, "HEAD") // BasicRepo has one commit
	tg, err := svc.CommitReviewTarget(context.Background(), root)
	if err != nil || tg.Range != root || tg.Commit != root {
		t.Fatalf("root: %+v %v", tg, err)
	}
	if _, err := svc.CommitReviewTarget(context.Background(), "main"); err == nil {
		t.Fatal("a ref name must be refused (hex only)")
	}
}
