package tui

import (
	"strconv"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/agentdocs"
	"github.com/homeend/gigagit/internal/steer"
)

func overviewAddCmd(id, title, text string) steer.Command {
	return steer.Command{ID: id, Cmd: "overview_add", Title: title, Text: text, Wait: true}
}

func applyOverview(t *testing.T, m Model, c steer.Command) (Model, steer.Reply) {
	t.Helper()
	nm, cmd := m.applySteer(c)
	nm = pumpAll(t, nm, cmd)
	return nm, awaitNote(t, nm, c.ID)
}

func TestOverviewAddShowsItAndAnswers(t *testing.T) {
	t.Parallel()
	m, r := applyOverview(t, loadedNavModel(t), overviewAddCmd("o-1", "Tour", tourText))
	d := topDoc(m)
	if d == nil || d.ov == nil || d.title != "Tour" {
		t.Fatalf("top = %+v, want the overview shown", d)
	}
	if !r.OK || r.Detail != "showing "+d.id() || len(r.Overviews) != 1 {
		t.Fatalf("reply = %+v", r)
	}
	if o := r.Overviews[0]; o.ID != d.id() || o.Title != "Tour" || o.State != "shown" || o.Anchors != 2 || len(o.Unresolved) != 0 {
		t.Fatalf("wire overview = %+v", o)
	}
}

func TestOverviewAddBackground(t *testing.T) {
	t.Parallel()
	c := overviewAddCmd("o-2", "Tour", tourText)
	c.Background = true
	m, r := applyOverview(t, loadedNavModel(t), c)
	if topDoc(m) != nil || !r.OK || r.Overviews[0].State != "background" || !strings.HasPrefix(r.Detail, "added ") {
		t.Fatalf("top=%v reply=%+v", topDoc(m), r)
	}
}

func TestOverviewAddWhileBusyFallsBackToBackground(t *testing.T) {
	t.Parallel()
	m := loadedNavModel(t)
	m.filterTyping = true
	m, r := applyOverview(t, m, overviewAddCmd("o-3", "Tour", tourText))
	if topDoc(m) != nil || !r.OK || !strings.Contains(r.Detail, "in the background (") {
		t.Fatalf("top=%v reply=%+v", topDoc(m), r)
	}
}

func TestOverviewAddListsUnresolvedAnchors(t *testing.T) {
	t.Parallel()
	text := "[ok](a.txt) [gone](nope.txt:3) [note](note:t999999)"
	m, r := applyOverview(t, loadedNavModel(t), overviewAddCmd("o-4", "Tour", text))
	if got := r.Overviews[0].Unresolved; len(got) != 2 || got[0] != "nope.txt:3" || got[1] != "note:t999999" {
		t.Fatalf("unresolved = %v", got)
	}
	d := topDoc(m)
	if d.ov.anchors[0].missing || !d.ov.anchors[1].missing || !d.ov.anchors[2].missing {
		t.Fatalf("missing flags = %v %v %v", d.ov.anchors[0].missing, d.ov.anchors[1].missing, d.ov.anchors[2].missing)
	}
}

func TestOverviewAddLimits(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, title, text, want string
	}{
		{"empty", "T", "  \n", "an overview needs text"},
		{"too big", "T", strings.Repeat("x", agentdocs.MaxOverviewBytes+1), "the text is over 64 KiB"},
		{"no title", " ", "x", "an overview needs a title"},
		{"two-line title", "a\nb", "x", "the title must be one line of at most 200 characters"},
	} {
		_, r := applyOverview(t, loadedNavModel(t), overviewAddCmd("l-"+tc.name, tc.title, tc.text))
		if r.OK || r.Error != tc.want {
			t.Errorf("%s: reply = %+v, want %q", tc.name, r, tc.want)
		}
	}
	m := loadedNavModel(t)
	for i := 0; i < agentdocs.MaxOverviewsPerRoot; i++ {
		m = m.registerDoc(storeOverview(t, m, "T", "x"))
	}
	if _, r := applyOverview(t, m, overviewAddCmd("l-21", "T", "x")); r.OK || r.Error != "20 overviews are open; remove one first" {
		t.Errorf("21st: reply = %+v", r)
	}
}

func TestOverviewSetKeepsTheSelection(t *testing.T) {
	t.Parallel()
	m, r := applyOverview(t, loadedNavModel(t), overviewAddCmd("s-1", "Tour", "[a](a.txt:1) [b](a.txt:2)"))
	d := topDoc(m)
	rows, _ := m.viewerGeom()
	d.selectAnchor(1, rows)
	set := steer.Command{ID: "s-2", Cmd: "overview_set", FileID: r.Overviews[0].ID, Text: "new [z](a.txt:9) then [b again](a.txt:2)", Wait: true}
	m, r2 := applyOverview(t, m, set)
	if !r2.OK || d.ov.sel != 1 || d.ov.anchors[1].dest != "a.txt:2" || d.title != "Tour" {
		t.Fatalf("reply=%+v sel=%d", r2, d.ov.sel)
	}
	set = steer.Command{ID: "s-3", Cmd: "overview_set", FileID: d.id(), Title: "Tour 2", Text: "[c](a.txt:3)", Wait: true}
	_, r3 := applyOverview(t, m, set)
	if !r3.OK || d.ov.sel != -1 || d.title != "Tour 2" {
		t.Fatalf("reply=%+v sel=%d title=%q", r3, d.ov.sel, d.title)
	}
}

func TestOverviewSetUnknownID(t *testing.T) {
	t.Parallel()
	_, r := applyOverview(t, loadedNavModel(t), steer.Command{ID: "u-1", Cmd: "overview_set", FileID: "f999999", Text: "x", Wait: true})
	if r.OK || r.Error != "no overview f999999" {
		t.Fatalf("reply = %+v", r)
	}
}

func TestOverviewListShowRm(t *testing.T) {
	t.Parallel()
	m, r := applyOverview(t, loadedNavModel(t), overviewAddCmd("x-1", "Tour", tourText))
	id := r.Overviews[0].ID
	m, l := applyOverview(t, m, steer.Command{ID: "x-2", Cmd: "overview_list", Wait: true})
	if !l.OK || len(l.Overviews) != 1 || l.Overviews[0].ID != id || l.Overviews[0].Text != "" {
		t.Fatalf("list = %+v", l)
	}
	m, s := applyOverview(t, m, steer.Command{ID: "x-3", Cmd: "overview_show", FileID: id, Wait: true})
	if !s.OK || len(s.Overviews) != 1 || s.Overviews[0].Text != tourText {
		t.Fatalf("show = %+v", s)
	}
	m, x := applyOverview(t, m, steer.Command{ID: "x-4", Cmd: "overview_rm", FileID: id, Wait: true})
	if !x.OK || x.Detail != "closed "+id || topDoc(m) != nil || m.findOpenFile(id, "") != nil {
		t.Fatalf("rm = %+v", x)
	}
}

func TestOverviewAddNamesTheFileItPushedOut(t *testing.T) {
	t.Parallel()
	m := loadedNavModel(t)
	var first *openFile
	for i := 0; i < maxOpenFiles; i++ {
		d := newOpenFile(fileSource{kind: srcWorktree}, "f"+strconv.Itoa(i)+".txt")
		if i == 0 {
			first = d
		}
		m = m.registerDoc(d)
	}
	_, r := applyOverview(t, m, overviewAddCmd("ev-1", "Tour", tourText))
	if !r.OK || !strings.Contains(r.Detail, "; closed "+first.path+" (100 files open)") {
		t.Fatalf("reply = %+v", r)
	}
}

// set, show and rm from an agent in another worktree are refused, as add is.
func TestOverviewVerbsRefuseAnotherWorktree(t *testing.T) {
	t.Parallel()
	c := overviewAddCmd("w-1", "Tour", tourText)
	c.Background = true
	m, r := applyOverview(t, loadedNavModel(t), c)
	id := r.Overviews[0].ID
	for _, verb := range []string{"overview_set", "overview_show", "overview_rm"} {
		c := steer.Command{ID: "w-" + verb, Cmd: verb, FileID: id, Text: "x", Worktree: "/somewhere/else", Wait: true}
		var r steer.Reply
		m, r = applyOverview(t, m, c)
		if r.OK || !strings.HasPrefix(r.Error, "gg is showing worktree ") {
			t.Errorf("%s: %+v", verb, r)
		}
	}
	if d, _ := m.findOverview(id); d == nil {
		t.Fatal("an rm from another worktree closed the overview")
	}
}
