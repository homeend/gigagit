package web

import (
	"context"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/linknav"
	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/steer"
)

const sessOvDoc = "+++\nformat = 1\ntitle = \"Tour\"\nauthor = \"agent\"\ncreated = 2026-10-28T10:00:00Z\n+++\nSee [one](f.txt:1).\n"

// sessOvServer is a standalone server attached to its worktree's real
// session-docs directory (the one the link resolver reads), steering on.
// Not parallel: it pins XDG_STATE_HOME.
func sessOvServer(t *testing.T) (s *Server, top, docs string) {
	t.Helper()
	isolateState(t)
	s = New(domain.Open(newRepoDir(t, 1)))
	s.steerDir = t.TempDir()
	ctx := context.Background()
	cd, err := s.service().GitCommonDir(ctx)
	if err != nil {
		t.Fatal(err)
	}
	top, _ = s.service().TopLevel(ctx)
	docs = linknav.SessionDocsDir(cd, top)
	if err := os.MkdirAll(docs, 0o755); err != nil {
		t.Fatal(err)
	}
	s.attachDocs(ctx)
	t.Cleanup(s.docs.Close)
	return s, top, docs
}

func sessOvLink(t *testing.T, top, id string) string {
	t.Helper()
	link, err := domain.SessionOverviewLink(top, id)
	if err != nil {
		t.Fatal(err)
	}
	return link
}

type linkCmdBody struct {
	Steer    *steerWire `json:"steer"`
	Checkout string     `json:"checkout"`
}

// A session overview link pasted into # opens the overview: the server
// finds it (reading the directory again — it was written after the store
// attached) and answers the file_focus the page lands, never a navigate the
// page would look up as a permanent overview.
func TestLinkCommandOpensASessionOverview(t *testing.T) {
	s, top, docs := sessOvServer(t)
	ts := serve(t, s)
	writeDoc(t, docs, "ov1.md", sessOvDoc)
	var b linkCmdBody
	if code := getJSON(t, ts, "/api/link-command?link="+url.QueryEscape(sessOvLink(t, top, "ov1")), &b); code != http.StatusOK ||
		b.Steer == nil || b.Steer.Cmd != "file_focus" || b.Steer.FileID != "ov1" || b.Steer.HintKind != "" {
		t.Fatalf("code=%d body=%+v steer=%+v", code, b, b.Steer)
	}
}

// One deleted since the link was copied is said so, in the resolver's words.
func TestLinkCommandSessionOverviewGone(t *testing.T) {
	s, top, _ := sessOvServer(t)
	var e map[string]any
	if code := getAny(t, serve(t, s), "/api/link-command?link="+url.QueryEscape(sessOvLink(t, top, "ov9")), &e); code != http.StatusUnprocessableEntity ||
		!strings.Contains(e["error"].(string), "overview ov9 no longer exists") {
		t.Fatalf("code=%d body=%v", code, e)
	}
}

// A link to ANOTHER worktree's session overview keeps the # prompt's answer
// for every link kind: that checkout, for the page to name.
func TestLinkCommandSessionOverviewOtherWorktree(t *testing.T) {
	s, top, _ := sessOvServer(t)
	wt2 := filepath.Join(filepath.Dir(top), filepath.Base(top)+"-wt2")
	if out, err := exec.Command("git", "-C", top, "worktree", "add", "-q", wt2).CombinedOutput(); err != nil {
		t.Fatalf("worktree add: %v %s", err, out)
	}
	t.Cleanup(func() { os.RemoveAll(wt2) })
	cd, _ := s.service().GitCommonDir(context.Background())
	docs2 := linknav.SessionDocsDir(cd, wt2)
	if err := os.MkdirAll(docs2, 0o755); err != nil {
		t.Fatal(err)
	}
	writeDoc(t, docs2, "ov1.md", sessOvDoc)
	var b linkCmdBody
	if code := getJSON(t, serve(t, s), "/api/link-command?link="+url.QueryEscape(sessOvLink(t, wt2, "ov1")), &b); code != http.StatusOK ||
		b.Steer != nil || !domain.SamePath(b.Checkout, wt2) {
		t.Fatalf("code=%d body=%+v", code, b)
	}
}

// The lookup is by id only: an open working-tree file whose path reads like
// the id is not the overview (the TUI's fix bea3f794).
func TestSessionOverviewFocusNotAFileNamedLikeIt(t *testing.T) {
	s, _, _ := sessOvServer(t)
	s.ofs.open(s.service().Root(), ofKey{Src: "worktree", Path: "ov2"}, "", 0)
	w := s.sessionOverviewFocus(context.Background(), steerWire{Cmd: "navigate", HintKind: model.OverviewHintKind, HintID: "ov2"})
	if w.Cmd != "navigate" || w.HintID != "ov2" {
		t.Fatalf("landed %+v on a file named like the overview", w)
	}
}

// Only the address-less shape is a session overview: a permanent overview's
// navigate names its commit.
func TestIsSessionOverviewNav(t *testing.T) {
	t.Parallel()
	nav := steerWire{Cmd: "navigate", HintKind: model.OverviewHintKind, HintID: "ab12cd34"}
	if !isSessionOverviewNav(nav) {
		t.Error("address-less overview navigate not recognised")
	}
	for _, w := range []steerWire{
		{Cmd: "navigate", HintKind: model.OverviewHintKind, HintID: "ab12cd34", Commit: "abc"},
		{Cmd: "navigate", HintKind: model.OverviewHintKind, HintID: "ab12cd34", File: "f.txt"},
		{Cmd: "navigate", HintKind: model.OverviewHintKind, HintID: "ab12cd34", State: "ref", Ref: "main"},
		{Cmd: "navigate", HintKind: model.ReviewHintKind, HintID: "r1"},
		{Cmd: "file_focus", HintKind: model.OverviewHintKind, HintID: "ab12cd34"},
	} {
		if isSessionOverviewNav(w) {
			t.Errorf("%+v read as a session overview", w)
		}
	}
}

// An agent's navigate to a session overview focuses it as gg session files
// focus does: the reply names it, and the tabs get the file_focus by id.
func TestSteerSessionOverviewNavigateFocusesIt(t *testing.T) {
	s, top, docs := sessOvServer(t)
	s.startLive(context.Background())
	t.Cleanup(s.Close)
	ts := serve(t, s)
	next, stop := eventsFor(t, ts, "t1")
	defer stop()
	next() // hello
	writeDoc(t, docs, "ov1.md", sessOvDoc)
	var rep steer.Reply
	body := `{"id":"1-1","cmd":"navigate","hint_kind":"overview","hint_id":"ov1","worktree":"` + filepath.ToSlash(top) + `"}`
	if code := postJSON(t, ts, "/api/session/steer", body, "application/json", "", &rep); code != http.StatusOK || !rep.OK || !strings.HasPrefix(rep.Detail, "focused ") {
		t.Fatalf("code=%d rep=%+v", code, rep)
	}
	for i := 0; i < 4; i++ {
		if m := next(); m.Reason == "steer" {
			if m.Steer == nil || m.Steer.Cmd != "file_focus" || m.Steer.FileID != "ov1" {
				t.Fatalf("steer = %+v", m.Steer)
			}
			return
		}
	}
	t.Fatal("no file_focus reached the tabs")
}

// A navigate to an overview that is neither a session document here nor a
// permanent one is refused to the agent in the TUI's words, and still goes
// to the tabs, whose page says so too (spec 2026-10-29 §4).
func TestSteerSessionOverviewGoneIsRefused(t *testing.T) {
	s, _, _ := sessOvServer(t)
	s.service().UseNotesDir(t.TempDir()) // the permanent overviews' store: empty
	s.startLive(context.Background())
	t.Cleanup(s.Close)
	ts := serve(t, s)
	next, stop := eventsFor(t, ts, "t1")
	defer stop()
	next() // hello
	var rep steer.Reply
	body := `{"id":"1-1","cmd":"navigate","hint_kind":"overview","hint_id":"ab12cd34"}`
	if code := postJSON(t, ts, "/api/session/steer", body, "application/json", "", &rep); code != http.StatusOK || rep.OK ||
		rep.Error != "overview ab12cd34 no longer exists" {
		t.Fatalf("code=%d rep=%+v", code, rep)
	}
	for i := 0; i < 4; i++ {
		if m := next(); m.Reason == "steer" {
			if m.Steer == nil || m.Steer.Cmd != "navigate" || m.Steer.HintID != "ab12cd34" {
				t.Fatalf("steer = %+v", m.Steer)
			}
			return
		}
	}
	t.Fatal("the navigate never reached the tabs")
}

// A permanent overview of that id is the page's to open: sent as before.
func TestSteerPermanentOverviewNavigateIsSent(t *testing.T) {
	s, _, _ := sessOvServer(t)
	svc := s.service()
	svc.UseNotesDir(t.TempDir())
	ctx := context.Background()
	tg, err := svc.HeadOverviewTarget(ctx)
	if err != nil {
		t.Fatal(err)
	}
	o, _, err := svc.SaveOverview(ctx, domain.SaveOverview{Target: tg, Agent: "Claude Code", Text: []byte(
		`{"version":1,"title":"How f works","overview":"Start at [f](f.txt:1).","files":[{"path":"f.txt","annotations":[{"newRange":[1,1],"summary":"the content"}]}]}`)})
	if err != nil {
		t.Fatal(err)
	}
	if code := steerPost(t, s, `{"id":"1-1","cmd":"navigate","hint_kind":"overview","hint_id":"`+o.ID+`"}`, "application/json"); code != http.StatusAccepted {
		t.Fatalf("code = %d, want 202", code)
	}
}

// gg open --web starting a server (and a Switch's replay) hands the page
// the overview to focus, found when the page asks — not at server start.
func TestStartAtSessionOverviewHandsFileFocus(t *testing.T) {
	s, _, docs := sessOvServer(t)
	if err := s.setStartAt(steer.Command{Cmd: "navigate", HintKind: model.OverviewHintKind, HintID: "ov1"}); err != nil {
		t.Fatal(err)
	}
	writeDoc(t, docs, "ov1.md", sessOvDoc)
	var at startAtBody
	getJSON(t, serve(t, s), "/api/session/start-at", &at)
	if at.Steer == nil || at.Steer.Cmd != "file_focus" || at.Steer.FileID != "ov1" {
		t.Fatalf("start-at = %+v", at.Steer)
	}
}

// A session overview names ITS worktree: steered at a page showing another
// one, it is asked about first, and the notice names the overview.
func TestSteerAskSessionOverviewOtherWorktree(t *testing.T) {
	t.Parallel()
	s := newSteerServer(t)
	other := filepath.Join(t.TempDir(), "wt-b")
	code, rep := steerAsk(t, s, `{"id":"1-1","cmd":"navigate","hint_kind":"overview","hint_id":"ov1","worktree":"`+filepath.ToSlash(other)+`"}`)
	if code != http.StatusOK || rep.OK || !strings.Contains(rep.Error, "asked the user to switch") {
		t.Fatalf("code=%d rep=%+v, want the ask reply", code, rep)
	}
	n := steerAskNoticeOf(t, s)
	if n == nil || n.Title != "an agent wants to show overview ov1 in wt-b" {
		t.Fatalf("notice = %+v", n)
	}
}

// The open overview's L (and ctrl+\'s y) copy its session overview link,
// built from the worktree its store is saved to.
func TestSessionOverviewLinkEndpoint(t *testing.T) {
	s, top, docs := sessOvServer(t)
	writeDoc(t, docs, "ov1.md", sessOvDoc)
	s.docs.Reload(s.docsRoot(context.Background()))
	ts := serve(t, s)
	var b struct {
		Link string `json:"link"`
	}
	if code := getJSON(t, ts, "/api/session-overview/link?id=ov1", &b); code != http.StatusOK || b.Link != sessOvLink(t, top, "ov1") {
		t.Fatalf("code=%d link=%q", code, b.Link)
	}
	var e map[string]any
	if code := getAny(t, ts, "/api/session-overview/link?id=..%2Fx", &e); code != http.StatusBadRequest {
		t.Errorf("bad id: code %d, want 400", code)
	}
	if code := getAny(t, ts, "/api/session-overview/link?id=ov9", &e); code != http.StatusNotFound || e["error"] != "overview ov9 no longer exists" {
		t.Errorf("gone: code %d body %v", code, e)
	}
}

// A store saved to no worktree (memory only) has no link to give: the TUI's
// words, never a gg:// with an empty path.
func TestSessionOverviewLinkMemoryOnly(t *testing.T) {
	t.Parallel()
	s := newSteerServer(t)
	s.docsDirOf = func(_, _ string) string { return "" }
	s.attachDocs(context.Background())
	var e map[string]any
	if code := getAny(t, serve(t, s), "/api/session-overview/link?id=ov1", &e); code != http.StatusUnprocessableEntity ||
		e["error"] != "this overview is not saved to a worktree — no link" {
		t.Fatalf("code=%d body=%v", code, e)
	}
}

func ofIDs(s *Server) []string {
	var ids []string
	for _, f := range s.ofs.list(s.service().Root()) {
		ids = append(ids, f.ID)
	}
	return ids
}

// A GET moves nothing: the # prompt's lookup hands the page the overview to
// focus, and the page's own open focuses it (spec 2026-10-29 §5).
func TestLinkCommandSessionOverviewLeavesTheListOrder(t *testing.T) {
	s, top, docs := sessOvServer(t)
	writeDoc(t, docs, "ov1.md", sessOvDoc)
	s.docs.Reload(s.docsRoot(context.Background()))
	s.followDocs()
	s.ofs.open(s.service().Root(), ofKey{Src: "worktree", Path: "f.txt"}, "", 0)
	before := strings.Join(ofIDs(s), ",")
	var b linkCmdBody
	if code := getJSON(t, serve(t, s), "/api/link-command?link="+url.QueryEscape(sessOvLink(t, top, "ov1")), &b); code != http.StatusOK ||
		b.Steer == nil || b.Steer.Cmd != "file_focus" || b.Steer.FileID != "ov1" {
		t.Fatalf("code=%d steer=%+v", code, b.Steer)
	}
	if after := strings.Join(ofIDs(s), ","); after != before {
		t.Fatalf("open files %s → %s: a GET reordered them", before, after)
	}
}

// The steer path lands the overview it found by id: gone since the lookup,
// it is refused — never the open file whose path reads like the id
// (spec 2026-10-29 §3).
func TestLandOpenFileGoneSinceTheLookup(t *testing.T) {
	s, _, _ := sessOvServer(t)
	wt := s.service().Root()
	bait, _ := s.ofs.open(wt, ofKey{Src: "worktree", Path: "ov1"}, "", 0)
	s.ofs.open(wt, ofKey{Src: "worktree", Path: "f.txt"}, "", 0)
	before := strings.Join(ofIDs(s), ",")
	c := steer.Command{ID: "1-1", Cmd: "file_focus", FileID: "ov1"}
	rep := s.landOpenFile(context.Background(), c, steerWire{Cmd: "file_focus", FileID: "ov1"}, steer.OpenFile{ID: "ov1", Source: "overview"})
	if rep.OK || rep.Error != "overview ov1 no longer exists" {
		t.Fatalf("rep = %+v", rep)
	}
	if after := strings.Join(ofIDs(s), ","); after != before {
		t.Fatalf("open files %s → %s (bait %s focused?)", before, after, bait.ID)
	}
}
