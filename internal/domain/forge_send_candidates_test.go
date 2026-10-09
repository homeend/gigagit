package domain

import (
	"context"
	"slices"
	"testing"

	"github.com/homeend/gigagit/internal/model"
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
