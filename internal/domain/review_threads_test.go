package domain

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/homeend/gigagit/internal/model"
)

// threadDoc has three remarks, all on lines of g.txt (3 lines).
const threadDoc = `{"version":1,"summary":"ok","files":[{"path":"g.txt","annotations":[
 {"newRange":[1,1],"summary":"first"},
 {"newRange":[2,2],"summary":"second"},
 {"newRange":[3,3],"summary":"third"}]}]}`

const threadDocReversed = `{"version":1,"summary":"ok","files":[{"path":"g.txt","annotations":[
 {"newRange":[3,3],"summary":"third"},
 {"newRange":[2,2],"summary":"second"},
 {"newRange":[1,1],"summary":"first"}]}]}`

const threadDocWithoutThird = `{"version":1,"summary":"ok","files":[{"path":"g.txt","annotations":[
 {"newRange":[1,1],"summary":"first"},
 {"newRange":[2,2],"summary":"second"}]}]}`

// threadReview stores threadDoc on a commit adding g.txt; it returns the
// service, the review id and its target (to re-save under the same id).
func threadReview(t *testing.T) (*Service, string, ReviewTarget) {
	t.Helper()
	dir, svc, _ := reviewRepo(t)
	commitFile(t, dir, "g.txt", "one\ntwo\nthree\n", "add g")
	tip := revParse(t, dir, "HEAD")
	tg := ReviewTarget{Kind: ReviewRange, Range: tip + "^.." + tip, Label: "g"}
	id, _, err := svc.SaveReview(context.Background(), SaveReview{Target: tg, Agent: "A", Text: threadDoc})
	if err != nil {
		t.Fatal(err)
	}
	return svc, id, tg
}

func resave(t *testing.T, svc *Service, id string, tg ReviewTarget, doc string) {
	t.Helper()
	if _, _, err := svc.SaveReview(context.Background(), SaveReview{Target: tg, Agent: "A", Text: doc, NoteID: id}); err != nil {
		t.Fatal(err)
	}
}

func mustReview(t *testing.T, svc *Service, id string) Review {
	t.Helper()
	r, err := svc.Review(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func remarkID(rid string, n int) string { return model.ReviewNoteIDPrefix + rid + ":" + itoa(n) }

func itoa(n int) string { return string(rune('0' + n)) } // indices < 10 here

func TestReplyToARemarkIsStoredWithItsReview(t *testing.T) {
	t.Parallel()
	svc, rid, _ := threadReview(t)
	ctx := context.Background()
	rep, err := svc.NoteReply(ctx, remarkID(rid, 1), model.Note{Source: model.NoteSourceAgent, Author: "B", Summary: "agreed"})
	if err != nil {
		t.Fatal(err)
	}
	r := mustReview(t, svc, rid)
	if rep.Address.Path != "" || rep.Address.Commit != r.Commit || rep.RemarkFP == "" || rep.RemarkSummary != "second" {
		t.Fatalf("reply = %+v", rep)
	}
	if _, err := svc.NoteReply(ctx, remarkID(rid, 9), model.Note{Summary: "x"}); !errors.Is(err, ErrNoSuchRemark) {
		t.Fatalf("out of range: %v", err)
	}
	if _, err := svc.NoteReply(ctx, model.ReviewNoteIDPrefix+"deadbeef:0", model.Note{Summary: "x"}); !errors.Is(err, ErrReviewNotFound) {
		t.Fatalf("gone review: %v", err)
	}
	// A forge parent is a GitHub thread now (a local draft reply): one gg has
	// not read is refused.
	if _, err := svc.NoteReply(ctx, "forge:12", model.Note{Summary: "x"}); !errors.Is(err, ErrUnknownForgeComment) {
		t.Fatalf("forge: %v", err)
	}
	// A reply to B's reply stays flat under the remark.
	if _, err := svc.NoteReply(ctx, rep.ID, model.Note{Author: "A", Summary: "thanks"}); err != nil {
		t.Fatal(err)
	}
	th, _ := mustReview(t, svc, rid).RemarkThreads()
	if len(th) != 3 || len(th[1].Replies) != 2 || len(th[0].Replies) != 0 {
		t.Fatalf("threads = %+v", th)
	}
}

func TestRemarkRepliesOfAVanishedRemarkAreOutdated(t *testing.T) {
	t.Parallel()
	svc, rid, tg := threadReview(t)
	ctx := context.Background()
	if _, err := svc.NoteReply(ctx, remarkID(rid, 2), model.Note{Author: "B", Summary: "on third"}); err != nil {
		t.Fatal(err)
	}
	resave(t, svc, rid, tg, threadDocWithoutThird)
	th, outdated := mustReview(t, svc, rid).RemarkThreads()
	if len(th) != 2 || len(outdated) != 1 || outdated[0].Summary != "third" || outdated[0].Replies[0].Summary != "on third" {
		t.Fatalf("threads=%+v outdated=%+v", th, outdated)
	}
	other, err := svc.ReviewOtherNotes(ctx, rid)
	if err != nil || !slices.ContainsFunc(other, func(o ReviewOtherNote) bool { return o.Outdated && o.Summary == "third" }) {
		t.Fatalf("ReviewOtherNotes = %+v, %v", other, err)
	}
}

func TestReviewNotesForCarriesRepliesAndResolution(t *testing.T) {
	t.Parallel()
	svc, rid, _ := threadReview(t)
	ctx := context.Background()
	id := remarkID(rid, 0)
	if _, err := svc.NoteReply(ctx, id, model.Note{Author: "B", Summary: "ok"}); err != nil {
		t.Fatal(err)
	}
	st := svc.notesStore(ctx)
	fp := mustReview(t, svc, rid).remarkFPs()[0]
	if err := st.Resolve(model.ThreadResolution{Root: id, By: "B", At: time.Unix(9, 0).UTC(), RemarkFP: fp}); err != nil {
		t.Fatal(err)
	}
	r := mustReview(t, svc, rid)
	got, err := svc.ReviewNotesFor(ctx, rid, "g.txt", sideDiff("one", "two", "three")) // review_doc_test.go helper
	if err != nil {
		t.Fatal(err)
	}
	i := slices.IndexFunc(got, func(n ResolvedNote) bool { return n.Note.ID == id })
	if i < 0 || len(got[i].Replies) != 1 || got[i].Resolution == nil || got[i].Resolution.By != "B" {
		t.Fatalf("remark 0 = %+v", got)
	}
	if rem, res := r.Tally(); rem != 3 || res != 1 {
		t.Fatalf("Tally = %d %d", rem, res)
	}
}

func TestNoteCountsCarryTheReviewTally(t *testing.T) {
	t.Parallel()
	svc, rid, _ := threadReview(t)
	ctx := context.Background()
	if _, err := svc.NoteResolve(ctx, remarkID(rid, 2), true, "B"); err != nil {
		t.Fatal(err)
	}
	c, err := svc.NoteCounts(ctx)
	if err != nil {
		t.Fatal(err)
	}
	i := slices.IndexFunc(c.Reviews, func(h ReviewHead) bool { return h.ID == rid })
	if i < 0 || c.Reviews[i].Remarks != 3 || c.Reviews[i].Resolved != 1 {
		t.Fatalf("heads = %+v", c.Reviews)
	}
}
