package domain

import (
	"context"
	"errors"
	"testing"

	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/notemeta"
)

const metaDoc = `{"version":1,"summary":"x",
 "display":{"kind":{"show":["badge"],"values":{"issue":{"color":"red","audience":"author"}}}},
 "files":[{"path":"a.txt","annotations":[
   {"newRange":[1,1],"summary":"first","meta":{"kind":"issue","raised_by":"opus"}},
   {"newRange":[2,2],"summary":"second","meta":{"kind":"good"},"display":{"kind":{"show":["line"]}}}]}]}`

// metaReview stores doc on a commit adding a.txt (3 lines); it returns the
// service, the review id, its target and the commit.
func metaReview(t *testing.T, doc string) (*Service, string, ReviewTarget, string) {
	t.Helper()
	dir, svc, _ := reviewRepo(t)
	commitFile(t, dir, "a.txt", "one\ntwo\nthree\n", "add a")
	tip := revParse(t, dir, "HEAD")
	tg := ReviewTarget{Kind: ReviewRange, Range: tip + "^.." + tip, Label: "a"}
	id, _, err := svc.SaveReview(context.Background(), SaveReview{Target: tg, Agent: "A", Text: doc})
	if err != nil {
		t.Fatal(err)
	}
	return svc, id, tg, tip
}

func TestRemarksCarryMetaAndEffectiveDisplay(t *testing.T) {
	t.Parallel()
	svc, rid, _, _ := metaReview(t, metaDoc)
	ctx := context.Background()
	root, _, _, err := svc.NoteThread(ctx, remarkID(rid, 0))
	if err != nil {
		t.Fatal(err)
	}
	r := NoteMeta(root, nil)
	if len(r.Badges) != 1 || r.Badges[0].Color != "red" || r.Audience != "author" {
		t.Fatalf("remark 0 = %+v", r)
	}
	if len(r.Stored) != 1 || r.Stored[0].Key != "raised_by" {
		t.Fatalf("stored = %+v", r.Stored)
	}
	root1, _, _, err := svc.NoteThread(ctx, remarkID(rid, 1))
	if err != nil {
		t.Fatal(err)
	}
	if r1 := NoteMeta(root1, nil); len(r1.Badges) != 0 || len(r1.Lines) != 1 {
		t.Fatalf("remark 1 must use its own declaration: %+v", r1)
	}
}

func TestReviewNotesForCarryRemarkMeta(t *testing.T) {
	t.Parallel()
	svc, rid, _, _ := metaReview(t, metaDoc)
	r := mustReview(t, svc, rid)
	rns := reviewDocNotes(r, "a.txt", model.FileAddress{State: model.StateCommitted, Commit: r.Commit, Path: "a.txt"},
		nil, []string{"one", "two", "three"}, true)
	if len(rns) != 2 || model.MetaValue(rns[0].Note, "kind") != "issue" || len(rns[0].Note.Display) == 0 {
		t.Fatalf("notes = %+v", rns)
	}
}

func TestReplyInheritsRootDisplay(t *testing.T) {
	t.Parallel()
	root := model.Note{Meta: []notemeta.KV{{Key: "kind", Value: "issue"}},
		Display: notemeta.Display{"kind": {Show: []string{"badge"}}}}
	reply := model.Note{ParentID: "x", Meta: []notemeta.KV{{Key: "kind", Value: "good"}}}
	if r := NoteMeta(reply, &root); len(r.Badges) != 1 || r.Badges[0].Value != "good" {
		t.Fatalf("reply = %+v", r)
	}
	reply.Display = notemeta.Display{"kind": {Show: []string{"line"}}}
	if r := NoteMeta(reply, &root); len(r.Badges) != 0 {
		t.Fatalf("a reply's own declaration wins: %+v", r)
	}
}

func TestWireNoteCarriesMetaAndDisplay(t *testing.T) {
	t.Parallel()
	n := model.Note{ID: "a", Source: model.NoteSourceAgent, Meta: []notemeta.KV{{Key: "severity", Value: "high"}}}
	w := ToWireNote(ResolvedNote{Note: n})
	if len(w.Meta) != 1 || w.Display == nil || len(w.Display.Lines) != 1 || w.Severity != "high" {
		t.Fatalf("wire = %+v", w)
	}
	if w := ToWireNote(ResolvedNote{Note: model.Note{ID: "b"}}); w.Display != nil {
		t.Fatalf("a note with no meta must carry no display: %+v", w.Display)
	}
}

// commitNote is a note by author on line 1 of a.txt at the commit tip.
func commitNote(t *testing.T, svc *Service, tip, author string) model.Note {
	t.Helper()
	addr, err := svc.NoteTarget(context.Background(), "a.txt", false, tip)
	if err != nil {
		t.Fatal(err)
	}
	n, err := svc.NoteAdd(context.Background(), model.Note{Source: model.NoteSourceAgent, Author: author,
		Address: addr, Side: model.NoteSideNew, Range: [2]int{1, 1}, Summary: "s"})
	if err != nil {
		t.Fatal(err)
	}
	return n
}

func TestSetNoteMetaByAuthorRewritesInPlace(t *testing.T) {
	t.Parallel()
	svc, _, _, tip := metaReview(t, metaDoc)
	ctx := context.Background()
	n := commitNote(t, svc, tip, "Claude Code")
	if _, err := svc.SetNoteMeta(ctx, n.ID, "Claude Code", []notemeta.KV{{Key: "kind", Value: "issue"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SetNoteMeta(ctx, n.ID, "MR", []notemeta.KV{{Key: "status", Value: "agreed"}}); err != nil {
		t.Fatal(err)
	}
	got, err := svc.NoteGet(ctx, n.ID)
	if err != nil {
		t.Fatal(err)
	}
	if model.MetaValue(got, "kind") != "issue" || len(got.Meta) != 1 {
		t.Fatalf("author meta = %v", got.Meta)
	}
	if len(got.Added) != 1 || got.Added[0].By != "MR" || got.Added[0].Value != "agreed" {
		t.Fatalf("added = %+v", got.Added)
	}
	if err := svc.UnsetNoteMeta(ctx, n.ID, "MR", []string{"status"}); err != nil {
		t.Fatal(err)
	}
	if got, _ := svc.NoteGet(ctx, n.ID); len(got.Added) != 0 {
		t.Fatalf("unset left %+v", got.Added)
	}
}

const metaDocSwapped = `{"version":1,"summary":"x","files":[{"path":"a.txt","annotations":[
  {"newRange":[2,2],"summary":"second","meta":{"kind":"good"}},
  {"newRange":[1,1],"summary":"first","meta":{"kind":"issue"}}]}]}`

func TestRemarkAddedMetaFollowsFingerprint(t *testing.T) {
	t.Parallel()
	svc, rid, tg, _ := metaReview(t, metaDoc)
	ctx := context.Background()
	if _, err := svc.SetNoteMeta(ctx, remarkID(rid, 1), "MR", []notemeta.KV{{Key: "status", Value: "disagreed"}}); err != nil {
		t.Fatal(err)
	}
	resave(t, svc, rid, tg, metaDocSwapped)
	r0, _, _, err := svc.NoteThread(ctx, remarkID(rid, 0))
	if err != nil {
		t.Fatal(err)
	}
	if model.MetaValue(r0, "status") != "disagreed" {
		t.Fatalf("the mark must follow 'second' to index 0: %+v", r0.Added)
	}
	r1, _, _, _ := svc.NoteThread(ctx, remarkID(rid, 1))
	if model.MetaValue(r1, "status") != "" {
		t.Fatalf("'first' must carry no mark: %+v", r1.Added)
	}
}

func TestSetNoteDisplayRefusals(t *testing.T) {
	t.Parallel()
	svc, rid, _, _ := metaReview(t, metaDoc)
	ctx := context.Background()
	if err := svc.SetNoteDisplay(ctx, remarkID(rid, 0), notemeta.Display{}); !errors.Is(err, ErrRemarkDisplay) {
		t.Fatalf("remark display = %v", err)
	}
	if err := svc.SetNoteDisplay(ctx, "forge:1", notemeta.Display{}); !errors.Is(err, ErrReadOnlyNote) {
		t.Fatalf("forge display = %v", err)
	}
	if _, err := svc.SetNoteMeta(ctx, "nosuchid", "MR", []notemeta.KV{{Key: "a", Value: "b"}}); !errors.Is(err, ErrNoteNotFound) {
		t.Fatalf("missing note = %v", err)
	}
}

func TestSetNoteMetaReportsDroppedEntries(t *testing.T) {
	t.Parallel()
	svc, _, _, tip := metaReview(t, metaDoc)
	ctx := context.Background()
	n := commitNote(t, svc, tip, "A")
	probs, err := svc.SetNoteMeta(ctx, n.ID, "A", []notemeta.KV{{Key: "ok", Value: "1"}, {Key: "bad", Value: "a\nb"}})
	if err != nil || len(probs) != 1 {
		t.Fatalf("probs %v err %v", probs, err)
	}
}
