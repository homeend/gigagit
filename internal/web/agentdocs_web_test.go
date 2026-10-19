package web

import (
	"context"
	"fmt"
	"net/http"
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

// A retired verb (session docs part 4) is answered before the hub's wire
// check (which would 400 an unknown verb) and past the op gate (which
// would 409), as a reply: an old CLI prints the fix.
func TestRetiredVerbsAreAnsweredBeforeTheWireCheckAndTheGate(t *testing.T) {
	t.Parallel()
	s, _ := noteSrv(t)
	s.cur = &opRun{} // an op in flight
	if code, rep := steerAsk(t, s, `{"id":"1","cmd":"note_list"}`); code != http.StatusOK || rep.OK || rep.Error != "note_list is retired: write the file (gg session note add)" {
		t.Fatalf("code=%d rep=%+v", code, rep)
	}
}

// esc and x on a noted entry both background it; the notes stay (close is
// local, spec 2026-10-18-gg-session-docs §4.4).
func TestCloseOfAPinnedEntryBackgroundsEscAndX(t *testing.T) {
	t.Parallel()
	s, root := noteSrv(t)
	if _, err := s.docs.AddNote(root, "f.txt", []string{"content 1"}, 1, 1, "a", "", ""); err != nil {
		t.Fatal(err)
	}
	s.followDocs()
	ts := serve(t, s)
	wt := s.service().Root()
	f, _ := s.ofs.lookup(wt, ofKey{Src: "worktree", Path: "f.txt"})
	for _, body := range []string{`{"op":"close","id":"` + f.ID + `","tab":"t1"}`, `{"op":"close","id":"` + f.ID + `","tab":"t1","everywhere":true}`} {
		postJSON(t, ts, "/api/open-files", body, "application/json", "", nil)
		if _, ok := s.ofs.lookup(wt, ofKey{Src: "worktree", Path: "f.txt"}); !ok || s.docs.NoteCount(root, "f.txt") != 1 {
			t.Fatalf("%s must keep the file and its note", body)
		}
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

// Passes run one at a time: each reads the store inside its turn, so the
// last one leaves the pins the store has now (two interleaved passes could
// leave a pin the store no longer holds). A pass started while another holds
// the turn has no effect until the turn is free.
func TestFollowPassesRunOneAtATime(t *testing.T) {
	t.Parallel()
	s, root := noteSrv(t)
	wt := s.service().Root()
	s.followMu.Lock()
	if _, err := s.docs.AddNote(root, "f.txt", []string{"content 1"}, 1, 1, "s", "", ""); err != nil {
		s.followMu.Unlock()
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() { s.followDocs(); close(done) }()
	time.Sleep(100 * time.Millisecond)
	_, listed := s.ofs.lookup(wt, ofKey{Src: "worktree", Path: "f.txt"})
	s.followMu.Unlock()
	if listed {
		t.Fatal("a pass listed the noted file while another held the turn")
	}
	<-done
	if _, ok := s.ofs.lookup(wt, ofKey{Src: "worktree", Path: "f.txt"}); !ok {
		t.Fatal("the pass did not list the noted file once the turn was free")
	}
}

// An eviction a steer recorded for one worktree is not announced by a pass
// over another (a repo switch between the steer's turn and the pass).
func TestAnEvictionDoesNotCrossAWorktreeSwitch(t *testing.T) {
	isolateGlobal(t)
	s, _ := noteSrv(t)
	s.startLive(context.Background())
	t.Cleanup(s.Close)
	ts := serve(t, s)
	next, stop := eventsFor(t, ts, "t1")
	defer stop()
	next() // hello
	s.followMu.Lock()
	s.evicted = append(s.evicted, ofEvicted{wt: "/a/worktree/no/longer/shown", path: "old.txt"})
	s.followMu.Unlock()
	s.followDocs()
	if m := next(); m.Reason != "agentdocs" || m.Evicted != "" {
		t.Fatalf("event = %+v, want no eviction from another worktree", m)
	}
}

// listDocs files an eviction under the worktree its caller listed in, not
// whatever the server shows by the time the closure returns (a reRoot in
// between would otherwise announce the old list's eviction on the new one).
func TestListDocsFilesTheEvictionUnderTheCallersWorktree(t *testing.T) {
	isolateGlobal(t)
	s, _ := noteSrv(t)
	s.startLive(context.Background())
	t.Cleanup(s.Close)
	ts := serve(t, s)
	next, stop := eventsFor(t, ts, "t1")
	defer stop()
	next() // hello
	s.listDocs("/the/worktree/the/caller/listed/in", func() string { return "old.txt" })
	if m := next(); m.Reason != "agentdocs" || m.Evicted != "" {
		t.Fatalf("event = %+v, want the other worktree's eviction dropped", m)
	}
}
