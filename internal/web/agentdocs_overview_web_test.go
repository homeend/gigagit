package web

import (
	"context"
	"encoding/json"
	"net/http"
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
	if m := next(); m.Reason != "agentdocs" || len(m.Files) != 0 || len(m.Closed) != 1 || m.Closed[0] != o.ID {
		t.Fatalf("remove event = %+v", m)
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

func TestSteerOverviewVerbsOnTheWeb(t *testing.T) {
	t.Parallel()
	s, _ := noteSrv(t)
	code, rep := steerAsk(t, s, `{"id":"1","cmd":"overview_add","title":"T","text":"[a](f.txt) [b](nope.txt)","background":true}`)
	if code != http.StatusOK || !rep.OK || len(rep.Overviews) != 1 || rep.Detail != "added "+rep.Overviews[0].ID+" in the background" {
		t.Fatalf("add: %d %+v", code, rep)
	}
	if o := rep.Overviews[0]; o.State != "background" || o.Anchors != 2 || len(o.Unresolved) != 1 || o.Unresolved[0] != "nope.txt" {
		t.Fatalf("add wire: %+v", o)
	}
	id := rep.Overviews[0].ID
	if _, ok := s.ofs.resolve(s.service().Root(), id, ""); !ok {
		t.Fatal("the added overview is not in the page's list")
	}
	if _, rep = steerAsk(t, s, `{"id":"2","cmd":"overview_set","file_id":"`+id+`","text":"[b](f.txt:1)"}`); !rep.OK || rep.Detail != "set "+id {
		t.Fatalf("set: %+v", rep)
	}
	if _, rep = steerAsk(t, s, `{"id":"3","cmd":"overview_show","file_id":"`+id+`"}`); !rep.OK || rep.Detail != id || rep.Overviews[0].Text != "[b](f.txt:1)" {
		t.Fatalf("show: %+v", rep)
	}
	if _, rep = steerAsk(t, s, `{"id":"4","cmd":"overview_list"}`); !rep.OK || rep.Detail != "1 overviews" || rep.Overviews[0].Text != "" {
		t.Fatalf("list: %+v", rep)
	}
	if _, rep = steerAsk(t, s, `{"id":"5","cmd":"overview_rm","file_id":"`+id+`"}`); !rep.OK || rep.Detail != "closed "+id {
		t.Fatalf("rm: %+v", rep)
	}
	if _, ok := s.ofs.resolve(s.service().Root(), id, ""); ok {
		t.Fatal("rm left the entry listed")
	}
	if _, rep = steerAsk(t, s, `{"id":"5b","cmd":"overview_rm","file_id":"`+id+`"}`); rep.OK || rep.Error != "no overview "+id {
		t.Fatalf("rm again: %+v", rep)
	}
	if _, rep = steerAsk(t, s, `{"id":"5c","cmd":"overview_add","title":"","text":"x"}`); rep.OK || rep.Error != "an overview needs a title" {
		t.Fatalf("add no title: %+v", rep)
	}
	if _, rep = steerAsk(t, s, `{"id":"6","cmd":"overview_add","title":"T","text":"x"}`); !rep.OK || rep.Overviews[0].State != "background" ||
		rep.Detail != "showing "+rep.Overviews[0].ID+"; no gg web tab is open to show it" {
		t.Fatalf("add shown: %+v", rep)
	}
	s.cur = &opRun{} // an op in flight: a foreground add falls back to the background
	if _, rep = steerAsk(t, s, `{"id":"7","cmd":"overview_add","title":"T","text":"x"}`); !rep.OK || !strings.Contains(rep.Detail, "in the background (operation in flight)") {
		t.Fatalf("add while busy: %+v", rep)
	}
	s.steerMu.Lock()
	s.steerWorktree = s.service().Root() // what initSteerPresence records
	s.steerMu.Unlock()
	if _, rep = steerAsk(t, s, `{"id":"8","cmd":"overview_add","title":"T","text":"x","worktree":"/somewhere/else"}`); rep.OK || !strings.HasPrefix(rep.Error, "gg web is showing worktree ") {
		t.Fatalf("other worktree: %+v", rep)
	}
}

func TestXOnAnOverviewEntryRemovesItFromTheStore(t *testing.T) {
	t.Parallel()
	s, root := noteSrv(t)
	o, _ := s.docs.AddOverview(root, s.service().Root(), "T", "x")
	s.followDocs()
	ts := serve(t, s)
	postJSON(t, ts, "/api/open-files", `{"op":"close","id":"`+o.ID+`","tab":"t1"}`, "application/json", "", nil)
	if _, ok := s.docs.Overview(o.ID); !ok {
		t.Fatal("esc removed the overview")
	}
	if _, ok := s.ofs.resolve(s.service().Root(), o.ID, ""); !ok {
		t.Fatal("esc closed the overview entry")
	}
	postJSON(t, ts, "/api/open-files", `{"op":"close","id":"`+o.ID+`","tab":"t1","everywhere":true}`, "application/json", "", nil)
	if _, ok := s.docs.Overview(o.ID); ok {
		t.Fatal("x left the overview in the store")
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

// set, show and rm from an agent in another worktree are refused, as add is:
// the overview ids it names are this worktree's.
func TestSteerOverviewVerbsRefuseAnotherWorktree(t *testing.T) {
	t.Parallel()
	s, _ := noteSrv(t)
	_, rep := steerAsk(t, s, `{"id":"1","cmd":"overview_add","title":"T","text":"x","background":true}`)
	id := rep.Overviews[0].ID
	s.steerMu.Lock()
	s.steerWorktree = s.service().Root()
	s.steerMu.Unlock()
	for _, c := range []string{
		`{"id":"2","cmd":"overview_set","file_id":"` + id + `","text":"y","worktree":"/somewhere/else"}`,
		`{"id":"3","cmd":"overview_show","file_id":"` + id + `","worktree":"/somewhere/else"}`,
		`{"id":"4","cmd":"overview_rm","file_id":"` + id + `","worktree":"/somewhere/else"}`,
	} {
		if _, rep := steerAsk(t, s, c); rep.OK || !strings.HasPrefix(rep.Error, "gg web is showing worktree ") {
			t.Errorf("%s: %+v", c, rep)
		}
	}
	if _, ok := s.docs.Overview(id); !ok {
		t.Fatal("an rm from another worktree removed the overview")
	}
}
