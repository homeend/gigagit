package domain

import (
	"context"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/model"
)

const previewReviewDoc = `{"version":1,"summary":"## Summary\nok","files":[{"path":"f.txt","annotations":[{"newRange":[1,1],"summary":"one"}]}]}`

// previewReviewRepo: main + branch feat/x one commit ahead, its own note store.
func previewReviewRepo(t *testing.T) (string, *Service, PreviewNoteSet) {
	t.Helper()
	dir, svc := newRealRepo(t)
	svc.UseNotesDir(t.TempDir())
	runGitIn(t, dir, "branch", "-M", "main")
	runGitIn(t, dir, "checkout", "-b", "feat/x")
	commitFile(t, dir, "f.txt", "x\n", "feature commit")
	set, err := svc.PreviewNotes(context.Background(), "feat/x", "main")
	if err != nil || !set.OK() {
		t.Fatalf("PreviewNotes: %+v %v", set, err)
	}
	return dir, svc, set
}

func savePreviewReview(t *testing.T, svc *Service, set PreviewNoteSet) string {
	t.Helper()
	id, _, err := svc.SaveReview(context.Background(), SaveReview{Target: ScopeReviewTarget(set), Agent: "Claude Code", Text: previewReviewDoc})
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func TestScopeReviewTargetPreviewAndPair(t *testing.T) {
	t.Parallel()
	dir, svc, set := previewReviewRepo(t)
	tg := ScopeReviewTarget(set)
	if tg.Kind != ReviewRange || tg.Range != set.Base+".."+set.Tip || tg.Diff.Rev != tg.Range ||
		tg.Commit != set.Tip || tg.Preview != "main...feat/x" || tg.Label != "main ... feat/x" {
		t.Fatalf("preview target = %+v", tg)
	}
	base := revParse(t, dir, "main")
	pair, err := svc.PairNotes(context.Background(), base, set.Tip)
	if err != nil {
		t.Fatal(err)
	}
	pt := ScopeReviewTarget(pair)
	want := base[:7] + ".." + set.Tip[:7]
	if pt.Preview != want || pt.Label != want || pt.Commit != set.Tip || pt.Range != base+".."+set.Tip {
		t.Fatalf("pair target = %+v", pt)
	}
}

func TestPreviewReviewIsTaggedAndOffCommits(t *testing.T) {
	t.Parallel()
	_, svc, set := previewReviewRepo(t)
	ctx := context.Background()
	id := savePreviewReview(t, svc, set)
	r, err := svc.Review(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if r.Preview != "main...feat/x" || r.Commit != set.Tip || r.Doc == nil {
		t.Fatalf("review = %+v", r)
	}
	c, err := svc.NoteCounts(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, h := range c.Reviews {
		if h.ID == id {
			t.Fatalf("a preview review is in the commit-facing Reviews: %+v", h)
		}
	}
	heads := c.PreviewReviews["main...feat/x"]
	if len(heads) != 1 || heads[0].ID != id || heads[0].Scope != set.Base+".."+set.Tip || heads[0].Remarks != 1 {
		t.Fatalf("PreviewReviews = %+v", c.PreviewReviews)
	}
	if got, _ := svc.ReviewsForCommit(ctx, set.Tip); len(got) != 0 {
		t.Fatalf("ReviewsForCommit lists the preview review: %+v", got)
	}
	// View all notes still lists it (Reviews() is the overview's source).
	all, _ := svc.Reviews(ctx)
	if len(all) != 1 || all[0].ID != id {
		t.Fatalf("Reviews() = %+v", all)
	}
}

func TestPreviewReviewRemarkReplyAttaches(t *testing.T) {
	t.Parallel()
	_, svc, set := previewReviewRepo(t)
	ctx := context.Background()
	id := savePreviewReview(t, svc, set)
	if _, err := svc.NoteReply(ctx, model.ReviewNoteIDPrefix+id+":0", model.Note{Summary: "fixed"}); err != nil {
		t.Fatal(err)
	}
	r, err := svc.Review(ctx, id)
	if err != nil || len(r.Replies) != 1 || r.Replies[0].Summary != "fixed" {
		t.Fatalf("replies = %+v %v", r.Replies, err)
	}
}

func TestPreviewReviewNotCountedAsPreviewNote(t *testing.T) {
	t.Parallel()
	_, svc, set := previewReviewRepo(t)
	savePreviewReview(t, svc, set)
	_, total, err := svc.PreviewNoteCounts(context.Background(), set)
	if err != nil || total != 0 {
		t.Fatalf("preview note total = %d %v, want 0 (a review is not a line note)", total, err)
	}
}

func TestUntaggedReviewStaysOnItsCommit(t *testing.T) {
	t.Parallel()
	_, svc, set := previewReviewRepo(t)
	ctx := context.Background()
	tg := ScopeReviewTarget(set)
	tg.Preview = "" // what `gg review --preview` stored before this change
	id, _, err := svc.SaveReview(ctx, SaveReview{Target: tg, Agent: "a", Text: previewReviewDoc})
	if err != nil {
		t.Fatal(err)
	}
	c, _ := svc.NoteCounts(ctx)
	if len(c.Reviews) != 1 || c.Reviews[0].ID != id || len(c.PreviewReviews) != 0 {
		t.Fatalf("untagged review: Reviews=%+v PreviewReviews=%+v", c.Reviews, c.PreviewReviews)
	}
	if got, _ := svc.ReviewsForCommit(ctx, set.Tip); len(got) != 1 || !strings.HasPrefix(got[0].Summary, "Review:") {
		t.Fatalf("ReviewsForCommit = %+v", got)
	}
}
func TestPreviewReviewsCurrentThenOlder(t *testing.T) {
	t.Parallel()
	dir, svc, set := previewReviewRepo(t)
	ctx := context.Background()
	id := savePreviewReview(t, svc, set)
	got, err := svc.PreviewReviews(ctx, set)
	if err != nil || len(got) != 1 || got[0].ID != id || got[0].Older {
		t.Fatalf("current: %+v %v", got, err)
	}
	commitFile(t, dir, "g.txt", "y\n", "second") // the source moves on
	moved, err := svc.PreviewNotes(ctx, "feat/x", "main")
	if err != nil {
		t.Fatal(err)
	}
	got, err = svc.PreviewReviews(ctx, moved)
	if err != nil || len(got) != 1 || !got[0].Older {
		t.Fatalf("after a new commit: %+v %v, want one older review", got, err)
	}
}

func TestPreviewReviewsHidesAGoneTip(t *testing.T) {
	t.Parallel()
	dir, svc, set := previewReviewRepo(t)
	ctx := context.Background()
	savePreviewReview(t, svc, set)
	// Rewrite the branch: the reviewed tip becomes unreachable, then pruned.
	runGitIn(t, dir, "reset", "--hard", "main")
	commitFile(t, dir, "h.txt", "z\n", "rewritten")
	runGitIn(t, dir, "reflog", "expire", "--expire=now", "--all")
	runGitIn(t, dir, "gc", "--prune=now", "--quiet")
	moved, err := svc.PreviewNotes(ctx, "feat/x", "main")
	if err != nil || !moved.OK() {
		t.Fatalf("PreviewNotes: %+v %v", moved, err)
	}
	if got, err := svc.PreviewReviews(ctx, moved); err != nil || len(got) != 0 {
		t.Fatalf("a review of a pruned tip = %+v %v, want hidden", got, err)
	}
}

func TestPreviewReviewsOnlyItsOwnScope(t *testing.T) {
	t.Parallel()
	dir, svc, set := previewReviewRepo(t)
	ctx := context.Background()
	savePreviewReview(t, svc, set)
	runGitIn(t, dir, "branch", "other", "main")
	other, err := svc.PreviewNotes(ctx, "feat/x", "other")
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := svc.PreviewReviews(ctx, other); len(got) != 0 {
		t.Fatalf("another preview of the same source shows %+v", got)
	}
	if got, _ := svc.PreviewReviews(ctx, PreviewNoteSet{}); len(got) != 0 {
		t.Fatalf("the zero set shows %+v", got)
	}
}

func TestPreviewReviewsByScopeKeepsExistingCommits(t *testing.T) {
	t.Parallel()
	dir, svc, set := previewReviewRepo(t)
	ctx := context.Background()
	id := savePreviewReview(t, svc, set)
	// The preview stops being previewable: the source is merged into main.
	runGitIn(t, dir, "checkout", "-q", "main")
	runGitIn(t, dir, "merge", "-q", "--ff-only", "feat/x")
	got, err := svc.PreviewReviewsByScope(ctx, "main...feat/x")
	if err != nil || len(got) != 1 || got[0].ID != id || !got[0].Older {
		t.Fatalf("merged preview's review = %+v %v, want it listed as older", got, err)
	}
	if got, _ := svc.PreviewReviewsByScope(ctx, "main...other"); len(got) != 0 {
		t.Fatalf("another scope shows %+v", got)
	}
	if got, _ := svc.PreviewReviewsByScope(ctx, ""); len(got) != 0 {
		t.Fatalf("the empty scope shows %+v", got)
	}
}

func TestPreviewReviewsByScopeHidesAGoneTip(t *testing.T) {
	t.Parallel()
	dir, svc, set := previewReviewRepo(t)
	ctx := context.Background()
	savePreviewReview(t, svc, set)
	runGitIn(t, dir, "checkout", "-q", "main")
	runGitIn(t, dir, "branch", "-D", "feat/x")
	runGitIn(t, dir, "reflog", "expire", "--expire=now", "--all")
	runGitIn(t, dir, "gc", "--prune=now", "--quiet")
	if got, err := svc.PreviewReviewsByScope(ctx, "main...feat/x"); err != nil || len(got) != 0 {
		t.Fatalf("a deleted, pruned source's review = %+v %v, want hidden", got, err)
	}
}

func TestReviewOlder(t *testing.T) {
	t.Parallel()
	dir, svc, set := previewReviewRepo(t)
	ctx := context.Background()
	id := savePreviewReview(t, svc, set)
	r, err := svc.Review(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if svc.ReviewOlder(ctx, r) {
		t.Fatal("a review of the current tip reads as older")
	}
	commitFile(t, dir, "g.txt", "y\n", "second") // the source moves on
	if !svc.ReviewOlder(ctx, r) {
		t.Fatal("a review of a moved tip does not read as older")
	}
	runGitIn(t, dir, "checkout", "-q", "main")
	runGitIn(t, dir, "branch", "-D", "feat/x")
	if !svc.ReviewOlder(ctx, r) {
		t.Fatal("a review whose scope no longer resolves does not read as older")
	}
	if svc.ReviewOlder(ctx, Review{Commit: set.Tip}) {
		t.Fatal("a commit review reads as older")
	}
}
