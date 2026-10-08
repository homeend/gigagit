package domain

import (
	"context"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/git"
	"github.com/homeend/gigagit/internal/model"
)

// addScopedNote is a root note on commit:path:line written in scope ("" = plain).
func addScopedNote(t *testing.T, svc *Service, commit, path string, line int, scope, sum string) string {
	t.Helper()
	n, err := svc.NoteAdd(context.Background(), model.Note{Source: model.NoteSourceUser, Summary: sum, Preview: scope,
		Address: model.FileAddress{State: model.StateCommitted, Commit: commit, Path: path},
		Side:    model.NoteSideNew, Range: [2]int{line, line}})
	if err != nil {
		t.Fatal(err)
	}
	return n.ID
}

func idsAt(t *testing.T, svc *Service, set PreviewNoteSet, path string) map[string]bool {
	t.Helper()
	got, err := svc.PreviewNotesAt(context.Background(), set, path)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]bool{}
	for _, r := range got {
		out[r.Note.ID] = true
	}
	return out
}

// Spec §1: the PR shows the note written for it and nothing else stored on
// its commits — a plain note, another review's note.
func TestPRShowsOnlyItsOwnNotes(t *testing.T) {
	t.Parallel()
	svc, _, head := sendRepo(t)
	mine := addScopedNote(t, svc, head, "big.go", 5, "main..."+git.PRRef(7), "for the PR")
	plain := addScopedNote(t, svc, head, "big.go", 25, "", "a commit note")
	other := addScopedNote(t, svc, head, "big.go", 5, "main...feat", "a branch review's note")
	ids := idsAt(t, svc, prNoteSetOf(t, svc), "big.go")
	if !ids[mine] || ids[plain] || ids[other] {
		t.Fatalf("PR view ids %v: mine %s, plain %s, other %s", ids, mine, plain, other)
	}
	counts, total, err := svc.PreviewNoteCounts(context.Background(), prNoteSetOf(t, svc))
	if err != nil || counts["big.go"] != 1 || total != 1 {
		t.Fatalf("counts %v total %d err %v", counts, total, err)
	}
}

// Review Focus 1: the base is any spelling, and it moves.
func TestPRNoteMatchesAnyBaseSpelling(t *testing.T) {
	t.Parallel()
	svc, _, head := sendRepo(t)
	base := revParse(t, repoDir(t, svc), "main")
	bySha := addScopedNote(t, svc, head, "big.go", 5, base+"..."+git.PRRef(7), "written over the base sha")
	byName := addScopedNote(t, svc, head, "big.go", 25, "origin/main..."+git.PRRef(7), "written by an agent")
	ids := idsAt(t, svc, prNoteSetOf(t, svc), "big.go")
	if !ids[bySha] || !ids[byName] {
		t.Fatalf("PR view ids %v", ids)
	}
}

// Review Focus 2: stacked PRs share commits; a note keeps to its own PR.
func TestPRNoteOfAnotherPRStaysOut(t *testing.T) {
	t.Parallel()
	svc, _, head := sendRepo(t)
	addScopedNote(t, svc, head, "big.go", 5, "main..."+git.PRRef(8), "PR 8's note")
	if ids := idsAt(t, svc, prNoteSetOf(t, svc), "big.go"); len(ids) != 0 {
		t.Fatalf("PR 7 shows %v", ids)
	}
}

func TestPRScopeNumber(t *testing.T) {
	t.Parallel()
	for scope, want := range map[string]int{
		"main..." + git.PRRef(7): 7, "abc123..." + git.PRRef(12): 12,
		"main...feat": 0, "a1..b2": 0, "": 0, git.PRRef(7): 0,
	} {
		n, ok := PRScopeNumber(scope)
		if (want == 0) == ok || n != want {
			t.Errorf("PRScopeNumber(%q) = %d %v, want %d", scope, n, ok, want)
		}
	}
	if got := NoteScopeLabel("0123abc..." + git.PRRef(7)); got != "PR #7" {
		t.Fatalf("label %q", got)
	}
}

// Spec §1: a note from elsewhere whose lines reappear in the PR is not the
// PR's (the carried notes of the 2026-10-07 spec are gone).
func TestPRCarriesNoNoteFromElsewhere(t *testing.T) {
	t.Parallel()
	svc, _, _ := sendRepo(t)
	mainTip := revParse(t, repoDir(t, svc), "main")
	elsewhere := addScopedNote(t, svc, mainTip, "big.go", 10, "", "line 10 is the same in the PR")
	set := prNoteSetOf(t, svc)
	if ids := idsAt(t, svc, set, "big.go"); ids[elsewhere] {
		t.Fatal("a note from main's tip shows in the PR")
	}
	all, err := svc.PreviewNotesAll(context.Background(), set)
	if err != nil || len(all) != 0 {
		t.Fatalf("PreviewNotesAll = %v, %v", all, err)
	}
}

// Review Focus 3: the send refuses a note the PR does not show.
func TestPlanSendRefusesANoteNotWrittenForThePR(t *testing.T) {
	t.Parallel()
	svc, _, head := sendRepo(t)
	plain := addScopedNote(t, svc, head, "big.go", 5, "", "a commit note")
	_, err := svc.planSend(context.Background(), PRSendRequest{PR: 7, Notes: []string{plain}})
	if err == nil || !strings.Contains(err.Error(), "not in this PR") {
		t.Fatalf("planSend of a plain note: %v", err)
	}
}

// Review Focus 5: a review run on the PR (the TUI/web/CLI path:
// ScopeReviewTarget) is stored with the PR's stamp and shows in the PR.
func TestPRReviewIsStampedAndShows(t *testing.T) {
	t.Parallel()
	svc, _, _ := sendRepo(t)
	set := prNoteSetOf(t, svc)
	tgt := ScopeReviewTarget(set)
	if n, ok := PRScopeNumber(tgt.Preview); !ok || n != 7 {
		t.Fatalf("review target preview %q", tgt.Preview)
	}
	rid, _, err := svc.SaveReview(context.Background(), SaveReview{Target: tgt, Agent: "claude", Text: twoRemarks})
	if err != nil {
		t.Fatal(err)
	}
	if got := svc.prReviews(context.Background(), set); len(got) != 1 || got[0].ID != rid {
		t.Fatalf("PR reviews = %+v", got)
	}
}

// Spec §1: a review of a commit that is in the PR is the commit's, not the PR's.
func TestPRLeavesOutAReviewOfItsCommit(t *testing.T) {
	t.Parallel()
	svc, _, head := sendRepo(t)
	saveHeadReview(t, svc, head, twoRemarks) // a commit review of the PR's head
	set := prNoteSetOf(t, svc)
	if got := svc.prReviews(context.Background(), set); len(got) != 0 {
		t.Fatalf("PR reviews = %+v", got)
	}
	for _, r := range mustNotesAt(t, svc, set, "big.go") {
		if strings.HasPrefix(r.Group, "review:") {
			t.Fatalf("a commit review's remark drew in the PR: %+v", r)
		}
	}
}

func mustNotesAt(t *testing.T, svc *Service, set PreviewNoteSet, path string) []ResolvedNote {
	t.Helper()
	got, err := svc.PreviewNotesAt(context.Background(), set, path)
	if err != nil {
		t.Fatal(err)
	}
	return got
}

// Final review I1: a PR's reviews show as remarks in its diff and in its send
// groups (prReviewHeads); the preview Reviews block stays off a PR, as it was
// — it matched one base spelling only and appeared only after a reload.
func TestPreviewReviewsStayOffAPR(t *testing.T) {
	t.Parallel()
	svc, _, _ := sendRepo(t)
	savePRReview(t, svc, twoRemarks)
	got, err := svc.PreviewReviews(context.Background(), prNoteSetOf(t, svc))
	if err != nil || len(got) != 0 {
		t.Fatalf("PreviewReviews on a PR = %+v, %v", got, err)
	}
}

// Final review I2: two PRs over the same head and base (a PR reopened as a
// new one) keep their own reviews' remarks — the remark cache is per PR.
func TestPRReviewRemarksAreCachedPerPR(t *testing.T) {
	t.Parallel()
	svc, _, head := sendRepo(t)
	runGitIn(t, repoDir(t, svc), "update-ref", git.PRRef(9), head)
	savePRReview(t, svc, twoRemarks) // run on #7
	ctx := context.Background()
	seven := prNoteSetOf(t, svc)
	nine, err := svc.PreviewNotes(ctx, git.PRRef(9), seven.Target)
	if err != nil || !nine.OK() || nine.Tip != seven.Tip || nine.Base != seven.Base {
		t.Fatalf("#9 set %+v err %v", nine, err)
	}
	if n := len(svc.prReviewNotes(ctx, seven)["big.go"]); n != 2 {
		t.Fatalf("#7 remarks = %d", n)
	}
	if got := svc.prReviewNotes(ctx, nine); len(got) != 0 {
		t.Fatalf("#9 shows #7's remarks: %v", got)
	}
}
