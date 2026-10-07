package web

import (
	"context"
	"fmt"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/homeend/gigagit/internal/forge"
	"github.com/homeend/gigagit/internal/model"
)

// writerForge is fakeForge + a recording forge.Writer + Snapshot: the web's
// send tests never reach a real forge.
type writerForge struct {
	*fakeForge
	wmu    sync.Mutex
	writes []string
}

func (f *writerForge) Snapshot(ctx context.Context, n int) (forge.Snapshot, error) {
	return snapForge{f.fakeForge}.Snapshot(ctx, n)
}

func (f *writerForge) log(s string) {
	f.wmu.Lock()
	f.writes = append(f.writes, s)
	f.wmu.Unlock()
}

func (f *writerForge) writeLog() string {
	f.wmu.Lock()
	defer f.wmu.Unlock()
	return strings.Join(f.writes, "\n")
}

func (f *writerForge) StartReview(_ context.Context, prID, _ string) (string, error) {
	f.log("StartReview " + prID)
	return "PRR_1", nil
}

func (f *writerForge) AddThread(_ context.Context, review string, t forge.Thread) (forge.ThreadRef, error) {
	f.log(fmt.Sprintf("AddThread %s %s:%d", review, t.Path, t.Line))
	return forge.ThreadRef{ID: "PRRT_1", CommentID: "PRRC_1"}, nil
}

func (f *writerForge) Reply(_ context.Context, _, thread, _ string) (forge.CommentRef, error) {
	f.log("Reply " + thread)
	return forge.CommentRef{ID: "PRRC_2"}, nil
}

func (f *writerForge) SubmitReview(_ context.Context, _ string, ev forge.Event, body string) error {
	f.log(fmt.Sprintf("SubmitReview %s %q", ev, body))
	return nil
}

func (f *writerForge) DeletePendingReview(_ context.Context, review string) error {
	f.log("DeletePendingReview " + review)
	return nil
}

func (f *writerForge) Resolve(_ context.Context, thread string) error {
	f.log("Resolve " + thread)
	return nil
}

func (f *writerForge) Unresolve(_ context.Context, thread string) error {
	f.log("Unresolve " + thread)
	return nil
}

// sendServer is a fetched PR #7 (node id PR_7, pr7.txt added) on a writable
// fake forge, with notes on (TestMain turns them off package-wide).
func sendServer(t *testing.T) (*httptest.Server, *writerForge, string) {
	t.Helper()
	dir, bare, head := prFixture(t)
	pr := openPR(7, "Add a thing")
	pr.HeadSHA, pr.NodeID = head, "PR_7"
	wf := &writerForge{fakeForge: &fakeForge{open: []model.PullRequest{pr}, baseURL: bare, comments: prThreads()}}
	ts, srv := prServe(t, dir, wf)
	srv.service().UseNotesDir(t.TempDir())
	waitPRsLoaded(t, ts)
	if done := runPROp(t, ts, "pr-fetch", 7); done["ok"] != true {
		t.Fatalf("pr-fetch: %v", done)
	}
	return ts, wf, head
}

// addWebNote writes a note on the PR head's path:line the way the page does.
func addWebNote(t *testing.T, ts *httptest.Server, head, path string, line int, summary string) string {
	t.Helper()
	code, out := postJSONAny(t, ts, "/api/notes/add",
		fmt.Sprintf(`{"path":%q,"rev":%q,"state":"commit","side":"new","line":%d,"summary":%q}`, path, head, line, summary))
	id, _ := out["id"].(string)
	if code != 200 || id == "" {
		t.Fatalf("add note = %d %v", code, out)
	}
	return id
}

// Serial: sendServer → prFixture isolates XDG state with t.Setenv.
func TestPRNotesCountsCarryGroupSlots(t *testing.T) {
	ts, _, head := sendServer(t)
	addWebNote(t, ts, head, "pr7.txt", 1, "mine")
	var out struct {
		Groups map[string][]int `json:"groups"`
		Notes  []wireNote       `json:"notes"`
	}
	if code := getJSON(t, ts, "/api/pr/notes?n=7&path=pr7.txt", &out); code != 200 {
		t.Fatalf("code %d", code)
	}
	if got := out.Groups["pr7.txt"]; len(got) == 0 || got[0] != 5 {
		t.Fatalf("groups = %v (want my draft review's slot 5 first)", out.Groups)
	}
	for _, n := range out.Notes {
		if n.Summary == "mine" && n.GroupSlot != 5 {
			t.Fatalf("the note's group_slot = %d", n.GroupSlot)
		}
	}
}
