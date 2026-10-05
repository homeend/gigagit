package domain

import (
	"context"
	"errors"
	"reflect"
	"slices"
	"testing"

	"github.com/homeend/gigagit/internal/model"
)

func TestNoteResolveOnEveryKindOfRoot(t *testing.T) {
	t.Parallel()
	svc, rid, _ := threadReview(t)
	ctx := context.Background()
	r := mustReview(t, svc, rid)
	addr := model.FileAddress{State: model.StateCommitted, Commit: r.Commit, Path: "g.txt"}
	line, err := svc.NoteAdd(ctx, model.Note{Address: addr, Side: model.NoteSideNew, Range: [2]int{2, 2}, Summary: "mine"})
	if err != nil {
		t.Fatal(err)
	}
	rep, err := svc.NoteReply(ctx, line.ID, model.Note{Summary: "r"})
	if err != nil {
		t.Fatal(err)
	}
	// A reply id resolves its thread.
	if _, err := svc.NoteResolve(ctx, rep.ID, true, "me"); err != nil {
		t.Fatal(err)
	}
	got, _ := svc.NotesAt(ctx, addr)
	if len(got) != 1 || got[0].Resolution == nil || got[0].Resolution.By != "me" {
		t.Fatalf("stored root = %+v", got)
	}
	remark := remarkID(rid, 1)
	if _, err := svc.NoteResolve(ctx, remark, true, "B"); err != nil {
		t.Fatal(err)
	}
	if th, _ := mustReview(t, svc, rid).RemarkThreads(); th[1].Resolution == nil {
		t.Fatalf("remark not resolved: %+v", th)
	}
	if _, err := svc.NoteResolve(ctx, remark, false, "B"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.NoteResolve(ctx, remark, false, "B"); !errors.Is(err, ErrNotResolved) {
		t.Fatalf("second unresolve: %v", err)
	}
	if _, err := svc.NoteResolve(ctx, "forge:5", true, "x"); !errors.Is(err, ErrForgeResolved) {
		t.Fatalf("forge: %v", err)
	}
	if _, err := svc.NoteResolve(ctx, "nope0000", true, "x"); !errors.Is(err, ErrNoteNotFound) {
		t.Fatalf("unknown: %v", err)
	}
	if _, err := svc.NoteResolve(ctx, remarkID(rid, 7), true, "x"); !errors.Is(err, ErrNoSuchRemark) {
		t.Fatalf("remark out of range: %v", err)
	}
	// Removing the stored root takes its resolution.
	if err := svc.NoteRemove(ctx, line.ID); err != nil {
		t.Fatal(err)
	}
	if rs, _ := svc.notesStore(ctx).LoadAllResolved(); slices.ContainsFunc(rs, func(x model.ThreadResolution) bool { return x.Root == line.ID }) {
		t.Fatalf("resolution outlived its root: %+v", rs)
	}
}

func TestRemarkRepliesAreNeverListedOnTheirOwn(t *testing.T) {
	t.Parallel()
	svc, rid, _ := threadReview(t)
	ctx := context.Background()
	before, err := svc.NoteCounts(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.NoteReply(ctx, remarkID(rid, 0), model.Note{Summary: "x"}); err != nil {
		t.Fatal(err)
	}
	after, _ := svc.NoteCounts(ctx) // NoteReply invalidated the cache
	if !reflect.DeepEqual(before, after) {
		t.Fatalf("counts changed: %+v → %+v", before, after)
	}
	ov, _ := svc.NotesOverview(ctx)
	var files []NoteFileNotes
	files = append(append(append(files, ov.Unstaged...), ov.Staged...), ov.Untracked...)
	for _, c := range ov.Commits {
		files = append(files, c.Files...)
	}
	for _, f := range files {
		for _, n := range f.Notes {
			for _, x := range append([]ResolvedNote{n}, n.Replies...) {
				if x.Note.IsRemarkReply() {
					t.Fatalf("overview lists a remark reply: %+v", x)
				}
			}
		}
	}
	addrs, _ := svc.NoteAddresses(ctx)
	for _, a := range addrs {
		ns, _ := svc.NotesAt(ctx, a)
		for _, n := range ns {
			if n.Note.IsRemarkReply() {
				t.Fatalf("NotesAt lists a remark reply: %+v", n)
			}
		}
	}
}

func TestRemarkRepliesFollowAMovedRemark(t *testing.T) {
	t.Parallel()
	svc, rid, tg := threadReview(t)
	ctx := context.Background()
	if _, err := svc.NoteReply(ctx, remarkID(rid, 2), model.Note{Author: "B", Summary: "on third"}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.NoteResolve(ctx, remarkID(rid, 2), true, "B"); err != nil {
		t.Fatal(err)
	}
	resave(t, svc, rid, tg, threadDocReversed)
	th, outdated := mustReview(t, svc, rid).RemarkThreads()
	if len(outdated) != 0 || len(th[0].Replies) != 1 || th[0].Replies[0].Summary != "on third" || th[0].Resolution == nil || th[2].Resolution != nil {
		t.Fatalf("threads=%+v outdated=%+v", th, outdated)
	}
}

// A forge thread GitHub marked resolved carries a Resolution like a stored
// one, so every frontend folds it by one rule.
func TestForgeResolvedThreadCarriesAResolution(t *testing.T) {
	t.Parallel()
	svc := newForgeSvc(t, &fakeForge{comments: reviewThreads()})
	ctx := context.Background()
	if _, err := svc.PRCommentsRefresh(ctx, 7); err != nil {
		t.Fatal(err)
	}
	got := svc.withResolutions(ctx, svc.forgeNotesFor(prSet("7"), "a.go"))
	if got[0].Note.ID != "forge:C1" || got[0].Resolution == nil {
		t.Fatalf("resolved forge thread = %+v", got[0])
	}
	if w := ToWireNote(got[0]); !w.Resolved || w.Replyable {
		t.Fatalf("wire = %+v", w)
	}
}
