package domain

import (
	"context"
	"testing"

	"github.com/homeend/gigagit/internal/model"
)

func TestMovedRemarkStaysHiddenAcrossAReSave(t *testing.T) {
	t.Parallel()
	svc, r := structuredReview(t) // remark 0 = g.txt:2 "line two"
	ctx := context.Background()
	fps := r.remarkFPs()
	st := svc.notesStore(ctx)
	if err := st.Edit(r.ID, func(n *model.Note) error {
		n.RemarkSends = []model.RemarkSend{{RemarkFP: fps[0], Moved: true,
			Send: model.NoteSend{PR: 7, Thread: "PRRT_x", URL: "https://x"}}}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	svc.invalidateNoteCounts()
	d := sideDiff("one", "two", "three")
	if got, _ := svc.ReviewNotesFor(ctx, r.ID, "g.txt", d); len(got) != 0 {
		t.Fatalf("a moved remark still shows: %+v", got)
	}
	// Re-save the same review in place (an import, a retry): still hidden.
	if _, _, err := svc.SaveReview(ctx, SaveReview{Target: ReviewTarget{Kind: ReviewRange, Range: r.Commit + "^.." + r.Commit, Label: "g"},
		Agent: "Claude", Text: r.Text, NoteID: r.ID}); err != nil {
		t.Fatal(err)
	}
	if got, _ := svc.ReviewNotesFor(ctx, r.ID, "g.txt", d); len(got) != 0 {
		t.Fatalf("the re-save brought the moved remark back: %+v", got)
	}
	r2, _ := svc.Review(ctx, r.ID)
	if len(r2.RemarkSends) != 1 || !r2.RemarkSends[0].Moved {
		t.Fatalf("RemarkSends after the re-save = %+v", r2.RemarkSends)
	}
}
