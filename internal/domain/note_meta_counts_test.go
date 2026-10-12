package domain

import (
	"context"
	"testing"

	"github.com/homeend/gigagit/internal/notemeta"
)

const countsDoc = `{"version":1,"summary":"x",
 "display":{"kind":{"show":["badge","count"],"values":{"issue":{"color":"red","audience":"author"},"good":{"color":"green","audience":"reviewer"}}}},
 "files":[{"path":"a.txt","annotations":[
   {"newRange":[1,1],"summary":"one","meta":{"kind":"issue"}},
   {"newRange":[2,2],"summary":"two","meta":{"kind":"good"}}]},
  {"path":"b.txt","annotations":[{"newRange":[1,1],"summary":"three","meta":{"kind":"issue"}}]}]}`

func TestReviewMetaCounts(t *testing.T) {
	t.Parallel()
	svc, rid, _, _ := metaReview(t, countsDoc)
	all, byFile := ReviewMetaCounts(mustReview(t, svc, rid))
	if all.Author != 2 || all.Reviewer != 1 || len(all.Groups) != 1 || all.Groups[0].Values[0].N != 2 || all.Groups[0].Values[0].Color != "red" {
		t.Fatalf("all = %+v", all)
	}
	if byFile["a.txt"].Author != 1 || byFile["a.txt"].Reviewer != 1 || byFile["b.txt"].Groups[0].Values[0].Value != "issue" {
		t.Fatalf("byFile = %+v", byFile)
	}
	// Added meta counts too: a mark changes what the counts say.
	if _, err := svc.SetNoteMeta(context.Background(), remarkID(rid, 1), "MR", []notemeta.KV{{Key: "kind", Value: "issue"}}); err != nil {
		t.Fatal(err)
	}
	all, _ = ReviewMetaCounts(mustReview(t, svc, rid))
	if all.Groups[0].Values[0].N != 3 || all.Author != 3 {
		t.Fatalf("after the mark = %+v", all)
	}
	if c, by := ReviewMetaCounts(Review{}); len(c.Groups) != 0 || by != nil {
		t.Fatal("a prose review has no counts")
	}
}

func TestReviewOtherNoteCarriesMeta(t *testing.T) {
	t.Parallel()
	svc, rid, _, _ := metaReview(t, countsDoc)
	other, err := svc.ReviewOtherNotes(context.Background(), rid)
	if err != nil {
		t.Fatal(err)
	}
	if len(other) != 1 || other[0].Path != "b.txt" || len(other[0].Meta.Badges) != 1 || other[0].Meta.Badges[0].Value != "issue" {
		t.Fatalf("other = %+v", other)
	}
}

func TestSendCandidateCarriesMeta(t *testing.T) {
	t.Parallel()
	svc, _, _ := sendRepo(t)
	ctx := context.Background()
	if _, err := svc.PullRequest(ctx, 7); err != nil {
		t.Fatal(err)
	}
	savePRReview(t, svc, `{"version":1,"summary":"s",
	 "display":{"kind":{"show":["badge"],"values":{"issue":{"color":"red","audience":"author"}}}},
	 "files":[{"path":"big.go","annotations":[{"newRange":[5,5],"summary":"check this","meta":{"kind":"issue","severity":"bug"}}]}]}`)
	c, err := svc.PRSendCandidates(ctx, 7)
	if err != nil {
		t.Fatal(err)
	}
	row := c.Groups[0].Rows[0]
	if len(row.Meta.Badges) != 1 || row.Meta.Badges[0].Value != "issue" || row.Meta.Audience != "author" || row.Severity != "bug" {
		t.Fatalf("row = %+v", row)
	}
}
