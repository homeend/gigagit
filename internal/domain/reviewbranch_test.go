package domain

import (
	"context"
	"testing"

	"github.com/homeend/gigagit/internal/model"
)

// A review note remembers what later reads cannot work out: a commit pair's
// (a range review) the branch it was written on, a merge preview's where its
// range began — and no branch, a preview review being the preview's.
func TestNoteAddRecordsTheReviewedBranchAndBase(t *testing.T) {
	t.Parallel()
	svc, dir := newPreviewRepo(t) // main checked out; feat = 3 commits on top
	ctx := context.Background()
	base, tip := revParse(t, dir, "main"), revParse(t, dir, "feat")
	add := func(preview string) model.Note {
		t.Helper()
		n, err := svc.NoteAdd(ctx, model.Note{
			Source: model.NoteSourceAgent, Author: "ada", Preview: preview,
			Address: model.FileAddress{State: model.StateCommitted, Commit: tip, Path: "a.txt"},
			Side:    model.NoteSideNew, Range: [2]int{1, 1}, Summary: "s",
		})
		if err != nil {
			t.Fatal(err)
		}
		return n
	}
	if n := add("main...feat"); n.PreviewBranch != "" || n.PreviewBase != base {
		t.Fatalf("merge preview note: branch %q base %q, want no branch and %s", n.PreviewBranch, n.PreviewBase, base)
	}
	if n := add(base[:7] + ".." + tip[:7]); n.PreviewBranch != "main" || n.PreviewBase != "" {
		t.Fatalf("pair note: branch %q base %q, want the checked-out branch and no base", n.PreviewBranch, n.PreviewBase)
	}
	if n := add(""); n.PreviewBranch != "" || n.PreviewBase != "" {
		t.Fatalf("a plain note records neither: %+v", n)
	}
	c, err := svc.NoteCounts(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, sc := range c.ScopesByCommit[tip] {
		if sc.Scope == "main...feat" && (sc.Branch != "" || sc.Base != base) {
			t.Fatalf("the counts must carry the preview review's base and no branch: %+v", sc)
		}
		if !IsPreviewScope(sc.Scope) && sc.Branch != "main" {
			t.Fatalf("the counts must carry the range review's branch: %+v", sc)
		}
	}
}

// What was created on a branch is shown on that branch, and on no other. A
// range review records its branch; a preview review has none (it is its
// preview's); neither has a plain note. With no branch on either side nothing
// can be told apart, and it shows.
func TestReviewShownOnItsBranchOnly(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name    string
		note    model.Note
		viewing []string
		want    bool
	}{
		{"its own branch", model.Note{Preview: "aaaaaaa..bbbbbbb", PreviewBranch: "feat"}, []string{"feat"}, true},
		{"the target after the merge", model.Note{Preview: "aaaaaaa..bbbbbbb", PreviewBranch: "feat"}, []string{"main"}, false},
		{"a slash branch", model.Note{Preview: "aaaaaaa..bbbbbbb", PreviewBranch: "feat/x"}, []string{"feat/x"}, true},
		{"the remote-tracking spelling", model.Note{Preview: "aaaaaaa..bbbbbbb", PreviewBranch: "origin/feat"}, []string{"feat"}, true},
		{"a look-alike name", model.Note{Preview: "aaaaaaa..bbbbbbb", PreviewBranch: "feat"}, []string{"other-feat"}, false},
		{"an old pair: no branch", model.Note{Preview: "aaaaaaa..bbbbbbb"}, []string{"main"}, true},
		{"one of several viewed", model.Note{Preview: "aaaaaaa..bbbbbbb", PreviewBranch: "feat"}, []string{"main", "feat"}, true},
		{"no branch is viewed (detached)", model.Note{Preview: "aaaaaaa..bbbbbbb", PreviewBranch: "feat"}, nil, true},
		{"a preview's note is not a branch's", model.Note{Preview: "main...feat", PreviewBranch: "feat"}, []string{"main"}, true},
		{"a plain note", model.Note{}, []string{"main"}, true},
	} {
		if got := ReviewShownOn(NoteReviewBranch(tc.note), tc.viewing); got != tc.want {
			t.Errorf("%s: shown = %v, want %v", tc.name, got, tc.want)
		}
	}
}

// The range opens from the base the note recorded, so it still opens once the
// branch was merged into its target (when git can no longer tell where the
// commit left it).
func TestScopeAtCommitUsesTheRecordedBaseAfterAMerge(t *testing.T) {
	t.Parallel()
	svc, dir := newPreviewRepo(t)
	ctx := context.Background()
	base, tip := revParse(t, dir, "main"), revParse(t, dir, "feat")
	if _, err := svc.NoteAdd(ctx, model.Note{
		Source: model.NoteSourceAgent, Author: "ada", Preview: "main...feat",
		Address: model.FileAddress{State: model.StateCommitted, Commit: tip, Path: "a.txt"},
		Side:    model.NoteSideNew, Range: [2]int{1, 1}, Summary: "s",
	}); err != nil {
		t.Fatal(err)
	}
	gitRun(t, dir, "merge", "--ff-only", "feat") // main now holds the tip
	svc.InvalidateNoteCounts()
	a, b, err := svc.ScopeAtCommit(ctx, "main...feat", tip)
	if err != nil || a != base || b != tip {
		t.Fatalf("after the merge: %s..%s, %v; want %s..%s", a, b, err, base, tip)
	}
}

// View all notes follows the same rule: on another branch a range review's
// notes and a branch's AI review are left out — and a commit holding nothing
// else with them. A preview's notes stay: they open in their preview.
func TestNotesOverviewShownOn(t *testing.T) {
	t.Parallel()
	note := func(summary, preview, branch string) ResolvedNote {
		return ResolvedNote{Note: model.Note{Summary: summary, Preview: preview, PreviewBranch: branch}}
	}
	ov := NotesOverview{Commits: []NoteCommitNotes{
		{Files: []NoteFileNotes{{Notes: []ResolvedNote{note("range", "aaaaaaa..bbbbbbb", "feat"), note("plain", "", ""), note("preview", "main...feat", "")}}}},
		{Files: []NoteFileNotes{{Notes: []ResolvedNote{note("range only", "aaaaaaa..bbbbbbb", "feat")}}}},
		{Reviews: []Review{{ID: "r1", Branch: "feat"}, {ID: "r2"}}},
	}}
	onMain := ov.ShownOn([]string{"main"})
	if len(onMain.Commits) != 2 {
		t.Fatalf("on main: %+v", onMain.Commits)
	}
	if ns := onMain.Commits[0].Files[0].Notes; len(ns) != 2 || ns[0].Note.Summary != "plain" || ns[1].Note.Summary != "preview" {
		t.Fatalf("on main, the first commit's notes: %+v", ns)
	}
	if rs := onMain.Commits[1].Reviews; len(rs) != 1 || rs[0].ID != "r2" {
		t.Fatalf("on main, a branch's AI review is left out: %+v", rs)
	}
	if onFeat := ov.ShownOn([]string{"feat"}); onFeat.Count() != 6 {
		t.Fatalf("on feat everything shows: %d", onFeat.Count())
	}
	if ov.Count() != 6 {
		t.Fatal("ShownOn must not modify its receiver")
	}
}

// A commit's reviews, as the viewing branch sees them: its range reviews
// written on that branch — never a preview's.
func TestScopesShownOn(t *testing.T) {
	t.Parallel()
	c := NoteCounts{ScopesByCommit: map[string][]NoteScopeCount{"h": {
		{Scope: "aaaaaaa..bbbbbbb", N: 1},
		{Scope: "ccccccc..ddddddd", N: 2, Branch: "feat"},
		{Scope: "eeeeeee..fffffff", N: 3, Branch: "other"},
		{Scope: "main...feat", N: 4},
	}}}
	names := func(scs []NoteScopeCount) (out []string) {
		for _, sc := range scs {
			out = append(out, sc.Scope)
		}
		return out
	}
	if got := names(c.ScopesShownOn("h", []string{"feat"})); len(got) != 2 || got[0] != "aaaaaaa..bbbbbbb" || got[1] != "ccccccc..ddddddd" {
		t.Fatalf("on feat: %v", got)
	}
	if got := names(c.ScopesShownOn("h", []string{"main"})); len(got) != 1 || got[0] != "aaaaaaa..bbbbbbb" {
		t.Fatalf("on main: %v", got)
	}
	if got := names(c.ScopesShownOn("h", nil)); len(got) != 3 {
		t.Fatalf("no branch viewed: every range review, never the preview's: %v", got)
	}
	if len(c.ScopesByCommit["h"]) != 4 {
		t.Fatal("the cached slice must not be modified")
	}
}
