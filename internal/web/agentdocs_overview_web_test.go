package web

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Review Focus 1: an overview is listed under the store's id, which never
// names a plain file the page opened itself.
func TestFollowPassListsOverviewsUnderTheStoresIDAndDropsGoneOnes(t *testing.T) {
	t.Parallel()
	s, root := noteSrv(t)
	wt := s.service().Root()
	plain, _ := s.ofs.open(wt, ofKey{Src: "worktree", Path: "f.txt"}, "", 0)
	o, err := s.docs.AddOverview(root, wt, "Tour", "[a](f.txt)")
	if err != nil {
		t.Fatal(err)
	}
	if o.ID == plain.ID {
		t.Fatalf("the overview took the plain file's id %s", o.ID)
	}
	s.followDocs()
	f, ok := s.ofs.resolve(wt, o.ID, "")
	if !ok || f.ID != o.ID || f.Source != "overview" || f.Title != "Tour" {
		t.Fatalf("entry = %+v %v", f, ok)
	}
	if _, pinned, _ := s.ofs.pinnedEntry(wt, o.ID); !pinned {
		t.Fatal("an overview entry must be pinned")
	}
	s.docs.SetOverview(o.ID, "Tour 2", "x")
	s.followDocs()
	if f, _ = s.ofs.resolve(wt, o.ID, ""); f.Title != "Tour 2" {
		t.Fatalf("title = %q after set", f.Title)
	}
	s.docs.RemoveOverview(o.ID)
	s.followDocs()
	if _, ok := s.ofs.resolve(wt, o.ID, ""); ok {
		t.Fatal("an overview that left the store is still listed")
	}
}

// Review Focus 2: the tabs hear which overview left, so one showing it
// closes its viewer.
func TestFollowPassTellsTheTabsWhichOverviewClosed(t *testing.T) {
	isolateGlobal(t)
	s, root := noteSrv(t)
	s.startLive(context.Background())
	t.Cleanup(s.Close)
	ts := serve(t, s)
	next, stop := eventsFor(t, ts, "t1")
	defer stop()
	next() // hello
	o, _ := s.docs.AddOverview(root, s.service().Root(), "T", "x")
	s.followDocs()
	if m := next(); m.Reason != "agentdocs" || len(m.Files) != 1 || m.Files[0].ID != o.ID || len(m.Closed) != 0 ||
		m.Stamps[o.ID] == "" || m.Stamps[o.ID] != s.docs.OverviewStamp(o.ID) {
		t.Fatalf("add event = %+v", m)
	}
	s.docs.RemoveOverview(o.ID)
	s.followDocs()
	m := next()
	if m.Reason != "agentdocs" || len(m.Files) != 0 || len(m.Closed) != 1 || m.Closed[0] != o.ID {
		t.Fatalf("remove event = %+v", m)
	}
	// The stamps list every overview the store holds — none is a list too:
	// a file whose overview left must hear it even when nothing names it
	// closed (the switcher's x unlists it before the store drops it).
	if m.Stamps == nil {
		t.Fatal("an agentdocs event with no overviews left carries no stamps")
	}
}

func TestOverviewEndpointServesTheTreeAndTheAnchors(t *testing.T) {
	t.Parallel()
	s, root := noteSrv(t)
	n, err := s.docs.AddNote(root, "f.txt", []string{"content 1"}, 1, 1, "s", "", "")
	if err != nil {
		t.Fatal(err)
	}
	o, _ := s.docs.AddOverview(root, s.service().Root(), "Tour", "see [here](f.txt:1) and [gone](nope.txt) and [n](note:"+n.ID+")")
	var body struct {
		ID      string          `json:"id"`
		Title   string          `json:"title"`
		Text    string          `json:"text"`
		Stamp   string          `json:"stamp"`
		Blocks  json.RawMessage `json:"blocks"`
		Anchors []struct {
			Dest    string `json:"dest"`
			Path    string `json:"path"`
			Start   int    `json:"start"`
			End     int    `json:"end"`
			Note    string `json:"note"`
			Missing bool   `json:"missing"`
			Ref     string `json:"ref"`
			Label   string `json:"label"`
		} `json:"anchors"`
	}
	if code := getJSON(t, serve(t, s), "/api/overview?id="+o.ID, &body); code != http.StatusOK {
		t.Fatalf("code %d", code)
	}
	if body.ID != o.ID || body.Title != "Tour" || len(body.Anchors) != 3 || body.Anchors[0].Missing || !body.Anchors[1].Missing {
		t.Fatalf("body = %+v", body)
	}
	if body.Stamp == "" || body.Stamp != s.docs.OverviewStamp(o.ID) {
		t.Fatalf("stamp %q, the store's (after the check) %q", body.Stamp, s.docs.OverviewStamp(o.ID))
	}
	if a := body.Anchors[2]; a.Missing || a.Note != n.ID || a.Path != "f.txt" || a.Start != 1 || a.End != 1 {
		t.Fatalf("note anchor = %+v (want its note's file and lines)", a)
	}
	if body.Anchors[0].Ref != `gg overview `+o.ID+` "Tour" → f.txt:1` {
		t.Fatalf("ref = %q", body.Anchors[0].Ref)
	}
	if body.Anchors[0].Label == "" {
		t.Fatalf("anchor label missing: %+v", body.Anchors[0])
	}
	if !strings.Contains(string(body.Blocks), `"k":"anchor"`) {
		t.Fatalf("blocks carry no anchor inline: %s", body.Blocks)
	}
	for _, bad := range []string{"f999", "x", "f1;rm"} {
		if code := getJSON(t, serve(t, s), "/api/overview?id="+bad, nil); code != http.StatusNotFound && code != http.StatusBadRequest {
			t.Fatalf("id %q: %d", bad, code)
		}
	}
}

// An overview of another root (a TUI's other worktree) is not this page's.
func TestOverviewEndpointRefusesAnotherRoot(t *testing.T) {
	t.Parallel()
	s, _ := noteSrv(t)
	o, _ := s.docs.AddOverview("/elsewhere", "/elsewhere", "T", "x")
	if code := getJSON(t, serve(t, s), "/api/overview?id="+o.ID, nil); code != http.StatusNotFound {
		t.Fatalf("code %d", code)
	}
}

// esc and x on an overview entry both background it: a document is held
// by the store, never closed by the list (spec §4.4; close is local).
func TestXOnAnOverviewEntryBackgroundsIt(t *testing.T) {
	t.Parallel()
	s, root := noteSrv(t)
	o, _ := s.docs.AddOverview(root, s.service().Root(), "T", "x")
	s.followDocs()
	ts := serve(t, s)
	for _, body := range []string{`{"op":"close","id":"` + o.ID + `","tab":"t1"}`, `{"op":"close","id":"` + o.ID + `","tab":"t1","everywhere":true}`} {
		postJSON(t, ts, "/api/open-files", body, "application/json", "", nil)
		if _, ok := s.docs.Overview(o.ID); !ok {
			t.Fatalf("%s removed the overview", body)
		}
		if _, ok := s.ofs.resolve(s.service().Root(), o.ID, ""); !ok {
			t.Fatalf("%s closed the overview entry", body)
		}
	}
}

func TestAnOverviewDoesNotOpenByPath(t *testing.T) {
	t.Parallel()
	if _, err := ofKeyOf(ofReq{Src: "overview", Path: "overview-1.md"}); err == nil || err.Error() != "an overview opens by id" {
		t.Fatalf("err = %v", err)
	}
}

// files focus on an overview names it by id and title, as the TUI does.
func TestFileFocusOnAnOverviewNamesItByIDAndTitle(t *testing.T) {
	t.Parallel()
	s, root := noteSrv(t)
	o, _ := s.docs.AddOverview(root, s.service().Root(), "The tour", "x")
	s.followDocs()
	_, rep := steerAsk(t, s, `{"id":"1","cmd":"file_focus","file_id":"`+o.ID+`"}`)
	if want := "focused overview " + o.ID + ` "The tour"`; !rep.OK || !strings.HasPrefix(rep.Detail, want) {
		t.Fatalf("rep = %+v, want detail starting %q", rep, want)
	}
}

// Interim (until plan 3 gives the page its own close/delete split): with
// the worktree attached to its session-docs directory, x on an overview
// entry must not unlink the agent's file — the page has no confirm.
func TestXOnAnAttachedOverviewKeepsTheDocument(t *testing.T) {
	t.Parallel()
	s, root := noteSrv(t)
	dir := filepath.Join(t.TempDir(), "docs")
	if err := s.docs.Attach(root, s.service().Root(), dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.docs.Close)
	o, err := s.docs.AddOverview(root, s.service().Root(), "T", "x")
	if err != nil {
		t.Fatal(err)
	}
	s.followDocs()
	ts := serve(t, s)
	postJSON(t, ts, "/api/open-files", `{"op":"close","id":"`+o.ID+`","tab":"t1","everywhere":true}`, "application/json", "", nil)
	if _, ok := s.docs.OverviewIn(root, o.ID); !ok {
		t.Fatal("x removed the attached overview")
	}
	if _, err := os.Stat(filepath.Join(dir, o.ID+".md")); err != nil {
		t.Fatal("x unlinked the agent's file")
	}
}
