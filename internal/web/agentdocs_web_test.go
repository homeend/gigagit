package web

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/homeend/gigagit/internal/agentdocs"
	"github.com/homeend/gigagit/internal/domain"
)

// noteSrv is a steering server over a one-file repo (f.txt: "content 1")
// with a store of its own, and the root its notes are filed under.
func noteSrv(t *testing.T) (*Server, string) {
	t.Helper()
	s := newSteerServer(t)
	return s, s.docsRoot(context.Background())
}

func TestNewHasItsOwnStoreAndAHostedPageShares(t *testing.T) {
	t.Parallel()
	if New(domain.Open(newRepoDir(t, 1))).docs == agentdocs.Shared() {
		t.Fatal("a standalone server must not use the shared store")
	}
	h := NewHost(domain.Open(newRepoDir(t, 1)), nil, true)
	if h.srv.docs != agentdocs.Shared() {
		t.Fatal("a hosted page must share the TUI's store")
	}
}

func TestSteerNoteAddOpensTheFileAndAnswersLikeTheTUI(t *testing.T) {
	t.Parallel()
	s, root := noteSrv(t)
	code, rep := steerAsk(t, s, `{"id":"1","cmd":"note_add","file":"f.txt","start":1,"end":1,"summary":"look"}`)
	if code != http.StatusOK || !rep.OK || rep.Detail != "noted f.txt:1 as t1" || len(rep.Notes) != 1 || rep.Notes[0].FileID != "f1" {
		t.Fatalf("code=%d rep=%+v", code, rep)
	}
	if s.docs.NoteCount(root, "f.txt") != 1 {
		t.Fatal("the store has no note")
	}
	if _, ok := s.ofs.lookup(s.service().Root(), ofKey{Src: "worktree", Path: "f.txt"}); !ok {
		t.Fatal("the noted file is not in the page's list")
	}
	_, rep = steerAsk(t, s, `{"id":"2","cmd":"note_add","file":"nope.txt","start":1,"end":1,"summary":"x"}`)
	if rep.OK || rep.Error != "nope.txt is not in the working tree" {
		t.Fatalf("missing file: %+v", rep)
	}
	_, rep = steerAsk(t, s, `{"id":"3","cmd":"note_add","file":"f.txt","start":1,"end":9,"summary":"x"}`)
	if rep.OK || rep.Error != "line 9 is past the end of f.txt (1 lines)" {
		t.Fatalf("past the end: %+v", rep)
	}
	_, rep = steerAsk(t, s, `{"id":"4","cmd":"note_add","file_id":"f9","start":1,"end":1,"summary":"x"}`)
	if rep.OK || rep.Error != "no open file f9" {
		t.Fatalf("unknown id: %+v", rep)
	}
}

// Review Focus 4: the note verbs are answered before the hub's wire check
// (which would 400 an unknown verb) and past the op gate.
func TestSteerNoteVerbsAreDispatchedBeforeTheWireCheckAndTheGate(t *testing.T) {
	t.Parallel()
	s, _ := noteSrv(t)
	s.cur = &opRun{} // an op in flight
	if code, rep := steerAsk(t, s, `{"id":"1","cmd":"note_list"}`); code != http.StatusOK || !rep.OK || rep.Detail != "no notes" {
		t.Fatalf("code=%d rep=%+v", code, rep)
	}
}

func TestSteerNoteShowRmAndClear(t *testing.T) {
	t.Parallel()
	s, _ := noteSrv(t)
	steerAsk(t, s, `{"id":"1","cmd":"note_add","file":"f.txt","start":1,"end":1,"summary":"a"}`)
	steerAsk(t, s, `{"id":"2","cmd":"note_add","file":"f.txt","start":1,"end":1,"summary":"b"}`)
	if _, rep := steerAsk(t, s, `{"id":"3","cmd":"note_show","note_id":"t1"}`); !rep.OK || len(rep.Notes) != 1 || len(rep.Notes[0].Text) != 1 || rep.Notes[0].Text[0] != "content 1" {
		t.Fatalf("show: %+v", rep)
	}
	if _, rep := steerAsk(t, s, `{"id":"3b","cmd":"note_list","file":"f.txt"}`); !rep.OK || len(rep.Notes) != 2 || rep.Notes[0].FileID != "f1" {
		t.Fatalf("list one file: %+v", rep)
	}
	if _, rep := steerAsk(t, s, `{"id":"4","cmd":"note_rm","note_id":"t1"}`); !rep.OK || rep.Detail != "removed t1" {
		t.Fatalf("rm: %+v", rep)
	}
	if _, rep := steerAsk(t, s, `{"id":"5","cmd":"note_rm","note_id":"t1"}`); rep.OK || rep.Error != "no note t1" {
		t.Fatalf("rm again: %+v", rep)
	}
	if _, rep := steerAsk(t, s, `{"id":"6","cmd":"note_rm","file":"f.txt"}`); !rep.OK || rep.Detail != "removed 1 notes from f.txt" {
		t.Fatalf("clear: %+v", rep)
	}
	if _, rep := steerAsk(t, s, `{"id":"7","cmd":"note_rm","file":"zz.txt"}`); rep.OK || rep.Error != "no open file zz.txt" {
		t.Fatalf("clear unknown: %+v", rep)
	}
}

func TestFileContentCarriesTheNotesAndAligns(t *testing.T) {
	t.Parallel()
	s, root := noteSrv(t)
	// Placed on content with a line above that the disk does not have: the
	// read aligns the note to "content 1".
	if _, err := s.docs.AddNote(root, "f.txt", []string{"OLD", "content 1"}, 2, 2, "s", "", ""); err != nil {
		t.Fatal(err)
	}
	var body struct {
		Notes []struct {
			ID    string `json:"id"`
			Start int    `json:"start"`
			Ref   string `json:"ref"`
		} `json:"notes"`
	}
	ts := serve(t, s)
	if code := getJSON(t, ts, "/api/file-content?path=f.txt", &body); code != http.StatusOK {
		t.Fatalf("code=%d", code)
	}
	if len(body.Notes) != 1 || body.Notes[0].Start != 1 || body.Notes[0].Ref != "gg note "+body.Notes[0].ID+" f.txt:1" {
		t.Fatalf("notes = %+v", body.Notes)
	}
	// A commit version never carries notes.
	var commit struct {
		Notes []any `json:"notes"`
	}
	if getJSON(t, ts, "/api/file-content?src=commit&rev=HEAD&path=f.txt", &commit); len(commit.Notes) != 0 {
		t.Fatalf("a commit version carries notes: %+v", commit.Notes)
	}
}

func TestDismissEndpoint(t *testing.T) {
	t.Parallel()
	s, root := noteSrv(t)
	n, err := s.docs.AddNote(root, "f.txt", []string{"content 1"}, 1, 1, "s", "", "")
	if err != nil {
		t.Fatal(err)
	}
	ts := serve(t, s)
	if code := postJSON(t, ts, "/api/file-notes", `{"op":"dismiss","id":"`+n.ID+`"}`, "application/json", "", nil); code != http.StatusOK {
		t.Fatalf("dismiss: %d", code)
	}
	if s.docs.NoteCount(root, "f.txt") != 0 {
		t.Fatal("the note is still in the store")
	}
	if code := postJSON(t, ts, "/api/file-notes", `{"op":"dismiss","id":"`+n.ID+`"}`, "application/json", "", nil); code != http.StatusNotFound {
		t.Fatalf("dismiss again: %d, want 404", code)
	}
	if code := postJSON(t, ts, "/api/file-notes", `{"op":"drop","id":"t1"}`, "application/json", "", nil); code != http.StatusBadRequest {
		t.Fatalf("unknown op: %d, want 400", code)
	}
}

// Review Focus 3: esc (a plain close) on a noted entry backgrounds it; x
// (close everywhere) removes it and clears the notes.
func TestCloseOfAPinnedEntryBackgroundsAndEverywhereClears(t *testing.T) {
	t.Parallel()
	s, root := noteSrv(t)
	steerAsk(t, s, `{"id":"1","cmd":"note_add","file":"f.txt","start":1,"end":1,"summary":"a"}`)
	s.followDocs()
	ts := serve(t, s)
	wt := s.service().Root()
	f, _ := s.ofs.lookup(wt, ofKey{Src: "worktree", Path: "f.txt"})
	postJSON(t, ts, "/api/open-files", `{"op":"close","id":"`+f.ID+`","tab":"t1"}`, "application/json", "", nil)
	if _, ok := s.ofs.lookup(wt, ofKey{Src: "worktree", Path: "f.txt"}); !ok {
		t.Fatal("esc closed a noted file")
	}
	postJSON(t, ts, "/api/open-files", `{"op":"close","id":"`+f.ID+`","tab":"t1","everywhere":true}`, "application/json", "", nil)
	if _, ok := s.ofs.lookup(wt, ofKey{Src: "worktree", Path: "f.txt"}); ok || s.docs.NoteCount(root, "f.txt") != 0 {
		t.Fatal("x must remove the file and clear its notes")
	}
}

// Review Focus 2 (the hosted case, its store side): a note filed in the
// server's store opens in the page's list, pinned, and the tabs hear it.
func TestFollowPassOpensANotedFileAndTellsTheTabs(t *testing.T) {
	isolateGlobal(t)
	s, root := noteSrv(t)
	s.startLive(context.Background())
	t.Cleanup(s.Close)
	ts := serve(t, s)
	next, stop := eventsFor(t, ts, "t1")
	defer stop()
	next() // hello
	if _, err := s.docs.AddNote(root, "f.txt", []string{"content 1"}, 1, 1, "s", "", ""); err != nil {
		t.Fatal(err)
	}
	s.followDocs()
	m := next()
	if m.Reason != "agentdocs" || len(m.Files) != 1 || m.Files[0].Path != "f.txt" || m.Files[0].Notes != 1 {
		t.Fatalf("event = %+v", m)
	}
	if _, pinned, _ := s.ofs.pinnedEntry(s.service().Root(), m.Files[0].ID); !pinned {
		t.Fatal("the noted file is not pinned")
	}
	s.docs.ClearPath(root, "f.txt")
	s.followDocs()
	if _, pinned, _ := s.ofs.pinnedEntry(s.service().Root(), m.Files[0].ID); pinned {
		t.Fatal("a file whose notes are gone stays pinned")
	}
}

func TestPinnedEntriesAreNeverEvicted(t *testing.T) {
	t.Parallel()
	n := int64(0)
	r := newOpenFiles(func() int64 { n++; return n })
	r.ensureOpen("/w", ofKey{Src: "worktree", Path: "noted.txt"}, true)
	for i := 0; i < maxOpenFiles+3; i++ {
		r.open("/w", ofKey{Src: "worktree", Path: fmt.Sprintf("p%d", i)}, "", 0)
	}
	if _, ok := r.lookup("/w", ofKey{Src: "worktree", Path: "noted.txt"}); !ok {
		t.Fatal("a pinned entry was evicted")
	}
	if got := len(r.list("/w")); got != maxOpenFiles {
		t.Fatalf("list = %d, want the cap held by evicting the others", got)
	}
}

// A failed TopLevel falls back to the opened directory for this call only:
// a cached fallback would file this page's notes under a key the TUI never
// uses, for good.
func TestDocsRootDoesNotCacheAFailedLookup(t *testing.T) {
	t.Parallel()
	dir := t.TempDir() // not a repository: TopLevel fails
	s := New(domain.Open(dir))
	if got := s.docsRoot(context.Background()); got != domain.CheckoutKey(dir) {
		t.Fatalf("root = %q, want the fallback %q", got, domain.CheckoutKey(dir))
	}
	if s.rootc.svc != nil {
		t.Fatal("a failed lookup was cached")
	}
}

// A note the store refuses lists nothing: the file is checked before it
// joins the page's list (no tab would hear of an entry added on a failure).
func TestARefusedNoteDoesNotListTheFile(t *testing.T) {
	t.Parallel()
	s, _ := noteSrv(t)
	if _, rep := steerAsk(t, s, `{"id":"1","cmd":"note_add","file":"f.txt","start":1,"end":9,"summary":"x"}`); rep.OK {
		t.Fatalf("past the end accepted: %+v", rep)
	}
	if l := s.ofs.list(s.service().Root()); len(l) != 0 {
		t.Fatalf("a refused note listed %+v", l)
	}
}

// The follow pass names a plain file it pushed out over the cap, as an open
// does — the tabs would otherwise see it leave the list unexplained.
func TestFollowPassNamesTheFileItPushedOut(t *testing.T) {
	isolateGlobal(t)
	s, root := noteSrv(t)
	s.startLive(context.Background())
	t.Cleanup(s.Close)
	ts := serve(t, s)
	next, stop := eventsFor(t, ts, "t1")
	defer stop()
	next() // hello
	wt := s.service().Root()
	for i := 0; i < maxOpenFiles; i++ {
		s.ofs.open(wt, ofKey{Src: "worktree", Path: fmt.Sprintf("p%d", i)}, "", 0)
	}
	if _, err := s.docs.AddOverview(root, wt, "T", "x"); err != nil {
		t.Fatal(err)
	}
	s.followDocs()
	if m := next(); m.Reason != "agentdocs" || m.Evicted != "p0" {
		t.Fatalf("event = %+v, want p0 named as pushed out", m)
	}
}

// Passes run one at a time: each reads the store inside the turn, so the
// last one leaves the pins the store has now (two interleaved passes could
// leave a pin the store no longer holds).
func TestFollowPassesRunOneAtATime(t *testing.T) {
	t.Parallel()
	s, _ := noteSrv(t)
	s.followMu.Lock()
	done := make(chan struct{})
	go func() { s.followDocs(); close(done) }()
	select {
	case <-done:
		s.followMu.Unlock()
		t.Fatal("a pass ran while another held the turn")
	case <-time.After(100 * time.Millisecond):
	}
	s.followMu.Unlock()
	<-done
}

// A note that lists its file over the cap names the file it pushed out both
// to the agent and to the tabs — whichever lists it, the steer or a pass.
func TestANoteOverTheCapTellsTheAgentAndTheTabs(t *testing.T) {
	isolateGlobal(t)
	s, _ := noteSrv(t)
	s.startLive(context.Background())
	t.Cleanup(s.Close)
	ts := serve(t, s)
	next, stop := eventsFor(t, ts, "t1")
	defer stop()
	next() // hello
	wt := s.service().Root()
	for i := 0; i < maxOpenFiles; i++ {
		s.ofs.open(wt, ofKey{Src: "worktree", Path: fmt.Sprintf("p%d", i)}, "", 0)
	}
	_, rep := steerAsk(t, s, `{"id":"1","cmd":"note_add","file":"f.txt","start":1,"end":1,"summary":"look"}`)
	if !rep.OK || !strings.HasSuffix(rep.Detail, "; closed p0 (20 files open)") {
		t.Fatalf("reply = %+v", rep)
	}
	for {
		m := next()
		if m.Reason == "agentdocs" {
			if m.Evicted != "p0" {
				t.Fatalf("event = %+v, want p0 named", m)
			}
			return
		}
	}
}
