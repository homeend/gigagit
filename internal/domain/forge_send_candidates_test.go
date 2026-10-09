package domain

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/notebatch"
)

const sevRemarks = `{"version":1,"summary":"Looks fine\n\nmore","files":[
 {"path":"big.go","annotations":[{"newRange":[5,5],"summary":"check this","rationale":"why","meta":{"severity":"bug"}}]},
 {"path":"other.go","annotations":[{"newRange":[1,1],"summary":"a file the PR does not change"}]}]}`

func TestPRSendCandidatesGroupsAndRows(t *testing.T) {
	t.Parallel()
	svc, ff, head := sendRepo(t)
	ctx := context.Background()
	ff.mu.Lock()
	ff.comments = []model.ForgeComment{{ID: "C1", Kind: model.ForgeCommentInline, ThreadID: "T1", Path: "big.go", Side: model.NoteSideNew, Line: 5, StartLine: 5, Body: "please", Author: "carol"}}
	ff.mu.Unlock()
	if _, err := svc.PRCommentsRefresh(ctx, 7); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.PullRequest(ctx, 7); err != nil {
		t.Fatal(err)
	}
	rid := savePRReview(t, svc, sevRemarks)
	mine := addPRNote(t, svc, head, "big.go", 25, "mine")
	d, err := svc.NoteReply(ctx, "forge:C1", model.Note{Source: model.NoteSourceUser, Author: "me", Summary: "done"})
	if err != nil {
		t.Fatal(err)
	}
	c, err := svc.PRSendCandidates(ctx, 7)
	if err != nil {
		t.Fatal(err)
	}
	if c.PR != 7 || c.Head != head {
		t.Fatalf("header %+v", c)
	}
	var ids []string
	for _, g := range c.Groups {
		ids = append(ids, g.ID)
	}
	if !slices.Equal(ids, []string{"review:" + rid, GroupMine, GroupReplies}) {
		t.Fatalf("groups = %v", ids)
	}
	rev := c.Groups[0]
	if rev.Kind != "review" || rev.Agent != "claude" || rev.Title != "Looks fine" || rev.Slot == 0 || len(rev.Rows) != 2 {
		t.Fatalf("review group = %+v", rev)
	}
	first := rev.Rows[0]
	if first.ID != "review:"+rid+":0" || first.Kind != "remark" || first.Severity != "bug" || first.Path != "big.go" || first.Range != [2]int{5, 5} || first.Skip != "" {
		t.Fatalf("row 0 = %+v", first)
	}
	if !slices.Equal(first.Code, []string{"line 5 changed"}) {
		t.Fatalf("code = %v", first.Code)
	}
	if second := rev.Rows[1]; second.Path != "other.go" || second.Skip != SkipNotInPR {
		t.Fatalf("a remark on a file the PR does not change must be listed with SkipNotInPR, got %+v", second)
	}
	if m := c.Groups[1]; m.Kind != "mine" || len(m.Rows) != 1 || m.Rows[0].ID != mine || m.Rows[0].Kind != "note" {
		t.Fatalf("mine = %+v", m)
	}
	if r := c.Groups[2]; r.Kind != "replies" || len(r.Rows) != 1 || r.Rows[0].ID != d.ID || r.Rows[0].Kind != "reply" {
		t.Fatalf("replies = %+v", r)
	}
}

func TestPRSendCandidatesEmpty(t *testing.T) {
	t.Parallel()
	svc, _, _ := sendRepo(t)
	c, err := svc.PRSendCandidates(context.Background(), 7)
	if err != nil || len(c.Groups) != 0 {
		t.Fatalf("%+v %v", c, err)
	}
}

// A review group's title is its summary's first line; a review that is
// prose (no document) is titled by its text's first line, as the body it
// would send starts.
func TestCandidateGroupTitle(t *testing.T) {
	t.Parallel()
	if got := candidateGroupTitle(Review{Text: "  Looks good overall.\n\nOne nit below."}); got != "Looks good overall." {
		t.Fatalf("prose title = %q", got)
	}
	doc := Review{Text: "raw", Doc: &notebatch.ReviewDoc{Summary: "Fine\nmore"}}
	if got := candidateGroupTitle(doc); got != "Fine" {
		t.Fatalf("document title = %q", got)
	}
}

// Two reviews saved in the same instant keep one order: by id.
func TestSortCandidateGroupsTiesByID(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 10, 10, 1, 0, 0, 0, time.UTC)
	gs := []*SendCandidateGroup{
		{ID: "review:b", Kind: "review", Created: now},
		{ID: "review:a", Kind: "review", Created: now},
		{ID: "review:c", Kind: "review", Created: now.Add(-time.Hour)},
		{ID: "review:d", Kind: "review", Created: now.Add(time.Hour)},
	}
	sortCandidateGroups(gs)
	var ids []string
	for _, g := range gs {
		ids = append(ids, g.ID)
	}
	if !slices.Equal(ids, []string{"review:d", "review:a", "review:b", "review:c"}) {
		t.Fatalf("order = %v", ids)
	}
}

// A remark on the old side lists its code from the PR's base, and a note
// already being sent (or on GitHub) is not a candidate at all.
func TestPRSendCandidatesOldSideAndSendingRows(t *testing.T) {
	t.Parallel()
	svc, _, head := sendRepo(t)
	ctx := context.Background()
	if _, err := svc.PullRequest(ctx, 7); err != nil {
		t.Fatal(err)
	}
	rid := savePRReview(t, svc, `{"version":1,"summary":"old side","files":[{"path":"big.go","annotations":[{"oldRange":[5,5],"summary":"was here"}]}]}`)
	sending := addPRNote(t, svc, head, "big.go", 25, "already going")
	// Stamped after the read starts: the settle pass never judges it, so it
	// stays "sending" through the listing.
	stampNote(t, svc, sending, model.NoteSend{PR: 7, Review: "PRR_x", At: time.Now().Add(time.Minute)})
	svc.invalidateNoteCounts()
	c, err := svc.PRSendCandidates(ctx, 7)
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Groups) != 1 || c.Groups[0].ID != "review:"+rid {
		t.Fatalf("a sending note must not be a candidate; groups = %+v", c.Groups)
	}
	row := c.Groups[0].Rows[0]
	if row.Side != "old" || row.Range != [2]int{5, 5} || !slices.Equal(row.Code, []string{"line 5"}) {
		t.Fatalf("old-side row = %+v, want the base's line 5", row)
	}
}

// A draft reply row carries the planner's skip reason, as note rows do: a
// thread the PR's comments no longer give a thread id for cannot take it.
func TestPRSendCandidatesReplyRowsCarryASkipReason(t *testing.T) {
	t.Parallel()
	svc, ff, _ := sendRepo(t)
	ctx := context.Background()
	ff.mu.Lock()
	ff.comments = []model.ForgeComment{
		{ID: "C1", Kind: model.ForgeCommentInline, ThreadID: "T1", Path: "big.go", Side: model.NoteSideNew, Line: 5, StartLine: 5, Body: "please", Author: "carol"},
		{ID: "C2", Kind: model.ForgeCommentInline, Path: "big.go", Side: model.NoteSideNew, Line: 25, StartLine: 25, Body: "no thread", Author: "carol"},
	}
	ff.mu.Unlock()
	if _, err := svc.PRCommentsRefresh(ctx, 7); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.PullRequest(ctx, 7); err != nil {
		t.Fatal(err)
	}
	ok, err := svc.NoteReply(ctx, "forge:C1", model.Note{Source: model.NoteSourceUser, Author: "me", Summary: "fine"})
	if err != nil {
		t.Fatal(err)
	}
	bad, err := svc.NoteReply(ctx, "forge:C2", model.Note{Source: model.NoteSourceUser, Author: "me", Summary: "lost"})
	if err != nil {
		t.Fatal(err)
	}
	c, err := svc.PRSendCandidates(ctx, 7)
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Groups) != 1 || c.Groups[0].Kind != "replies" || len(c.Groups[0].Rows) != 2 {
		t.Fatalf("groups = %+v", c.Groups)
	}
	for _, row := range c.Groups[0].Rows {
		switch row.ID {
		case ok.ID:
			if row.Skip != "" {
				t.Fatalf("a reply to a live thread must be sendable, got %q", row.Skip)
			}
		case bad.ID:
			if row.Skip != SkipThreadNotInPR {
				t.Fatalf("a reply to a thread-less comment: skip = %q, want %q", row.Skip, SkipThreadNotInPR)
			}
		}
	}
}
