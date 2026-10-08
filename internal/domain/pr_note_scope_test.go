package domain

import (
	"context"
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
