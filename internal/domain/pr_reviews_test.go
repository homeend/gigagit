package domain

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/model"
)

// prNoteSetOf opens PR #7 of sendRepo and returns its note set.
func prNoteSetOf(t *testing.T, svc *Service) PreviewNoteSet {
	t.Helper()
	pr, err := svc.PullRequest(context.Background(), 7)
	if err != nil {
		t.Fatal(err)
	}
	prev, err := svc.PRPreview(context.Background(), pr)
	if err != nil || !prev.Set.OK() {
		t.Fatalf("PR preview: %+v %v", prev.Endpoints, err)
	}
	return prev.Set
}

const twoRemarks = `{"version":1,"summary":"looks fine","files":[{"path":"big.go","annotations":[
 {"newRange":[5,5],"summary":"check this"},{"newRange":[25,25],"summary":"and this"}]}]}`

// repoDir is sendRepo's checkout (revParse needs it).
func repoDir(t *testing.T, svc *Service) string {
	t.Helper()
	dir, err := svc.TopLevel(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return dir
}

func saveHeadReview(t *testing.T, svc *Service, head, doc string) string {
	t.Helper()
	rid, _, err := svc.SaveReview(context.Background(), SaveReview{
		Target: ReviewTarget{Kind: ReviewRange, Range: head + "^.." + head, Label: "feat"}, Agent: "claude", Text: doc})
	if err != nil {
		t.Fatal(err)
	}
	return rid
}

// savePRReview is a review run on PR #7 (ScopeReviewTarget, the TUI/web path).
func savePRReview(t *testing.T, svc *Service, doc string) string {
	t.Helper()
	rid, _, err := svc.SaveReview(context.Background(), SaveReview{
		Target: ScopeReviewTarget(prNoteSetOf(t, svc)), Agent: "claude", Text: doc})
	if err != nil {
		t.Fatal(err)
	}
	return rid
}

// T1: a review run on the PR draws its remarks in the PR diff,
// as review:<id>:<n> roots of the review's group.
func TestPRDiffShowsItsReviewsRemarks(t *testing.T) {
	t.Parallel()
	svc, _, _ := sendRepo(t)
	rid := savePRReview(t, svc, twoRemarks)
	got, err := svc.PreviewNotesAt(context.Background(), prNoteSetOf(t, svc), "big.go")
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, r := range got {
		if r.Group != "review:"+rid {
			continue
		}
		ids = append(ids, r.Note.ID)
		if r.Sync != model.SyncLocal || r.Note.Source != model.NoteSourceAgent || r.Note.Author != "claude" {
			t.Errorf("remark root %+v", r)
		}
	}
	want := []string{model.ReviewNoteIDPrefix + rid + ":0", model.ReviewNoteIDPrefix + rid + ":1"}
	if !reflect.DeepEqual(ids, want) {
		t.Fatalf("remark ids = %v, want %v", ids, want)
	}
}

// A review of a commit that is not the PR's (main's own history) stays out.
func TestPRDiffLeavesOutAReviewOfAnotherCommit(t *testing.T) {
	t.Parallel()
	svc, _, head := sendRepo(t)
	base := revParse(t, repoDir(t, svc), head+"~1")
	saveHeadReview(t, svc, base, `{"version":1,"summary":"old","files":[{"path":"big.go","annotations":[{"newRange":[10,10],"summary":"x"}]}]}`)
	got, _ := svc.PreviewNotesAt(context.Background(), prNoteSetOf(t, svc), "big.go")
	for _, r := range got {
		if strings.HasPrefix(r.Group, "review:") {
			t.Fatalf("a review of another commit drew in the PR: %+v", r)
		}
	}
}

// T2: the Files badges count drawn remarks too.
func TestPRCountsIncludeItsNotesAndRemarks(t *testing.T) {
	t.Parallel()
	svc, _, head := sendRepo(t)
	ctx := context.Background()
	addPRNote(t, svc, head, "big.go", 5, "on the PR")
	savePRReview(t, svc, twoRemarks)
	counts, _, err := svc.PreviewNoteCounts(ctx, prNoteSetOf(t, svc))
	if err != nil {
		t.Fatal(err)
	}
	if counts["big.go"] != 3 {
		t.Fatalf("big.go counts %d, want 3 (1 note + 2 remarks)", counts["big.go"])
	}
}

// Groups per path, in line order, each once: the Files badge's colour bars.
func TestPreviewNoteGroupsByPath(t *testing.T) {
	t.Parallel()
	svc, _, head := sendRepo(t)
	addPRNote(t, svc, head, "big.go", 5, "mine")
	rid := savePRReview(t, svc, twoRemarks)
	groups, err := svc.PreviewNoteGroups(context.Background(), prNoteSetOf(t, svc))
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{GroupMine, "review:" + rid}; !reflect.DeepEqual(groups["big.go"], want) {
		t.Fatalf("groups = %v, want %v", groups["big.go"], want)
	}
}

// The remark slice is a CACHED instance: the per-path answer is built in a
// fresh slice, so two reads of one path agree and the second carries no
// element the first appended.
func TestPreviewNotesAtLeavesTheCachesAlone(t *testing.T) {
	t.Parallel()
	svc, _, head := sendRepo(t)
	addPRNote(t, svc, head, "big.go", 10, "mine")
	savePRReview(t, svc, twoRemarks)
	set := prNoteSetOf(t, svc)
	ctx := context.Background()
	before := len(svc.prReviewNotes(ctx, set)["big.go"])
	a, _ := svc.PreviewNotesAt(ctx, set, "big.go")
	b, _ := svc.PreviewNotesAt(ctx, set, "big.go")
	after := len(svc.prReviewNotes(ctx, set)["big.go"])
	if before != 2 || after != before || len(a) != len(b) || len(a) != 3 {
		t.Fatalf("caches %d → %d, reads %d / %d", before, after, len(a), len(b))
	}
}
