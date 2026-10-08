package domain

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/homeend/gigagit/internal/forge"
	"github.com/homeend/gigagit/internal/git"
	"github.com/homeend/gigagit/internal/model"
)

// prThreadSvc: the prPreviewRepo PR (#7, a.go changed) with one GitHub
// thread on a.go:3 (root PRRC_c1, reply PRRC_c2) cached, and a note store.
func prThreadSvc(t *testing.T) (*Service, PreviewNoteSet, string, *fakeForge) {
	t.Helper()
	dir, head := prPreviewRepo(t)
	_, svc := newRealRepoAt(t, dir)
	svc.UseNotesDir(t.TempDir())
	at := time.Date(2026, 10, 5, 10, 0, 0, 0, time.UTC)
	ff := &fakeForge{
		byNum: map[int]model.PullRequest{7: {Number: 7, State: "open", Target: "main", HeadSHA: head}},
		comments: []model.ForgeComment{
			{ID: "PRRC_c1", Kind: model.ForgeCommentInline, Author: "bob", Body: "why?\n\n" + forge.SendMarker("zz"), Path: "a.go",
				Side: model.NoteSideNew, Line: 3, StartLine: 3, ThreadID: "PRRT_t1", ReviewID: "PRR_r1", Created: at},
			{ID: "PRRC_c2", ParentID: "PRRC_c1", Kind: model.ForgeCommentInline, Author: "ann", Body: "because", Path: "a.go",
				Side: model.NoteSideNew, Line: 3, StartLine: 3, ThreadID: "PRRT_t1", ReviewID: "PRR_r2", Created: at},
		},
	}
	svc.SetForgeProviders([]forge.Provider{ff})
	ctx := context.Background()
	if _, err := svc.PRCommentsRefresh(ctx, 7); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.PullRequest(ctx, 7); err != nil {
		t.Fatal(err)
	}
	set, err := svc.PreviewNotes(ctx, git.PRRef(7), "main")
	if err != nil || !set.OK() {
		t.Fatalf("set = %+v, %v", set, err)
	}
	return svc, set, head, ff
}

func TestReplyToAGitHubThreadIsALocalDraftUnderIt(t *testing.T) {
	t.Parallel()
	svc, set, head, _ := prThreadSvc(t)
	ctx := context.Background()
	// Answering the REPLY still hangs the draft off the thread's root.
	d, err := svc.NoteReply(ctx, model.ForgeNoteIDPrefix+"PRRC_c2", model.Note{Source: model.NoteSourceUser, Summary: "on it"})
	if err != nil {
		t.Fatal(err)
	}
	if d.ParentID != model.ForgeNoteIDPrefix+"PRRC_c1" || d.Address.Commit != head || d.Address.Path != "a.go" || d.Range != [2]int{3, 3} {
		t.Fatalf("draft = %+v", d)
	}
	got, err := svc.PreviewNotesAt(ctx, set, "a.go")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("roots = %d, want the one GitHub thread (the draft is not a root)", len(got))
	}
	root := got[0]
	if root.Sync != model.SyncForge || root.Group != "github:PRR_r1" {
		t.Errorf("root sync/group = %q/%q", root.Sync, root.Group)
	}
	if strings.Contains(root.Note.Summary+root.Note.Rationale, "gg:") {
		t.Errorf("the send marker shows: %q / %q", root.Note.Summary, root.Note.Rationale)
	}
	last := root.Replies[len(root.Replies)-1]
	if last.Note.ID != d.ID || last.Sync != model.SyncLocal || last.Range != [2]int{3, 3} {
		t.Fatalf("draft reply = %+v (want local, last, at the thread's line)", last)
	}
	// It is a stored note: removable like any other.
	if err := svc.NoteRemove(ctx, d.ID); err != nil {
		t.Fatal(err)
	}
}

func TestReplyToAnUnknownForgeCommentFails(t *testing.T) {
	t.Parallel()
	svc, _, _, _ := prThreadSvc(t)
	if _, err := svc.NoteReply(context.Background(), model.ForgeNoteIDPrefix+"PRRC_nope",
		model.Note{Summary: "x"}); err == nil {
		t.Fatal("a reply to a comment gg has not read must fail")
	}
}

func TestStoredNoteCarriesItsSyncState(t *testing.T) {
	t.Parallel()
	svc, set, head, _ := prThreadSvc(t)
	ctx := context.Background()
	n, err := svc.NoteAdd(ctx, model.Note{Source: model.NoteSourceUser, Summary: "local", Preview: set.Pair(),
		Address: model.FileAddress{State: model.StateCommitted, Commit: head, Path: "a.go"},
		Side:    model.NoteSideNew, Range: [2]int{1, 1}})
	if err != nil {
		t.Fatal(err)
	}
	st := svc.notesStore(ctx)
	if err := st.Edit(n.ID, func(x *model.Note) error {
		x.Send = &model.NoteSend{PR: 7, Err: "HTTP 403"}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	svc.invalidateNoteCounts()
	got, _ := svc.PreviewNotesAt(ctx, set, "a.go")
	for _, r := range got {
		if r.Note.ID == n.ID {
			if r.Sync != model.SyncFailed || r.SendErr != "HTTP 403" || r.Group != GroupMine {
				t.Fatalf("stored note = sync %q err %q group %q", r.Sync, r.SendErr, r.Group)
			}
			w := ToWireNote(r)
			if w.Sync != "failed" || w.SendErr != "HTTP 403" || w.Group != GroupMine {
				t.Fatalf("wire = %+v", w)
			}
			return
		}
	}
	t.Fatal("the stored note is not in the PR's notes")
}
