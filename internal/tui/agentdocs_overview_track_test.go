package tui

import (
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/domain"
)

// addOverviewViaSteer adds an overview the way an agent does (shown unless
// the screen is busy) and returns the document it registered.
func addOverviewViaSteer(t *testing.T, m Model, title, text string) (Model, *openFile) {
	t.Helper()
	m, r := applyOverview(t, m, overviewAddCmd("via-steer", title, text))
	if !r.OK || len(r.Overviews) != 1 {
		t.Fatalf("add: %+v", r)
	}
	d, why := m.findOverview(r.Overviews[0].ID)
	if d == nil {
		t.Fatal(why)
	}
	return m, d
}

// An overview the browser closes leaves the TUI; the one on screen closes
// as X would and says so.
func TestAnOverviewRemovedElsewhereClosesInTheTUI(t *testing.T) {
	t.Parallel()
	m := privateDocsModel(t)
	m, d := addOverviewViaSteer(t, m, "Tour", "[a](a.txt:3)") // shown
	if topDoc(m) != d {
		t.Fatal("the fixture overview is not on screen")
	}
	m.docs.RemoveOverview(d.id())
	m, _ = m.onAgentDocsChanged()
	if m.openFiles.find(m.currentWorktree, d.key()) != nil || topDoc(m) == d {
		t.Fatal("the overview is still open in the TUI")
	}
	if !strings.Contains(m.statusMsg, "overview "+d.id()+" was closed in the browser") {
		t.Fatalf("status = %q", m.statusMsg)
	}
}

// A background overview removed elsewhere leaves the list quietly.
func TestABackgroundOverviewRemovedElsewhereLeavesQuietly(t *testing.T) {
	t.Parallel()
	m := privateDocsModel(t)
	c := overviewAddCmd("bg", "Tour", "[a](a.txt)")
	c.Background = true
	m, r := applyOverview(t, m, c)
	d, _ := m.findOverview(r.Overviews[0].ID)
	m.statusMsg = "before"
	m.docs.RemoveOverview(d.id())
	m, _ = m.onAgentDocsChanged()
	if m.openFiles.find(m.currentWorktree, d.key()) != nil || m.statusMsg != "before" {
		t.Fatalf("listed=%v status=%q", m.openFiles.find(m.currentWorktree, d.key()) != nil, m.statusMsg)
	}
}

// set made elsewhere re-lays out, keeping the selected anchor by dest.
func TestAnOverviewSetElsewhereRelaysOutKeepingTheSelection(t *testing.T) {
	t.Parallel()
	m := privateDocsModel(t)
	m, d := addOverviewViaSteer(t, m, "Tour", "[a](a.txt:3) [b](a.txt:5)")
	rows, _ := m.viewerGeom()
	d.selectAnchor(1, rows) // a.txt:5
	if _, err := m.docs.SetOverview(d.id(), "Tour 2", "intro [x](a.txt:1) [b](a.txt:5)"); err != nil {
		t.Fatal(err)
	}
	m, _ = m.onAgentDocsChanged()
	if d.title != "Tour 2" || d.ov.text != "intro [x](a.txt:1) [b](a.txt:5)" || d.ov.sel != 1 || d.ov.anchors[1].dest != "a.txt:5" {
		t.Fatalf("title=%q sel=%d anchors=%+v", d.title, d.ov.sel, d.ov.anchors)
	}
}

// An overview added to the store elsewhere joins the TUI's list in the
// background, under the store's id.
func TestAnOverviewAddedElsewhereJoinsInTheBackground(t *testing.T) {
	t.Parallel()
	m := privateDocsModel(t)
	o, err := m.docs.AddOverview(domain.CheckoutKey(m.currentWorktree), m.currentWorktree, "Away", "[a](a.txt)")
	if err != nil {
		t.Fatal(err)
	}
	m, _ = m.onAgentDocsChanged()
	d, _ := m.findOverview(o.ID)
	if d == nil || d.title != "Away" || topDoc(m) == d || len(d.ov.anchors) != 1 {
		t.Fatalf("doc = %+v", d)
	}
}

// A check made elsewhere (the web's request) paints the TUI's anchors.
func TestMissingFlagsFollowTheStore(t *testing.T) {
	t.Parallel()
	m := privateDocsModel(t)
	m, d := addOverviewViaSteer(t, m, "Tour", "[a](a.txt) [b](a.txt:2)")
	m.docs.SetAnchorMissing(d.id(), 0, true)
	m, _ = m.onAgentDocsChanged()
	if !d.ov.anchors[0].missing || d.ov.anchors[1].missing {
		t.Fatalf("missing = %v %v", d.ov.anchors[0].missing, d.ov.anchors[1].missing)
	}
}

// X in the TUI removes the overview from the store (the page closes it).
func TestXOnAnOverviewRemovesItFromTheStore(t *testing.T) {
	t.Parallel()
	m := privateDocsModel(t)
	m, d := addOverviewViaSteer(t, m, "Tour", "[a](a.txt)")
	m = m.closeDoc(d)
	if _, ok := m.docs.Overview(d.id()); ok {
		t.Fatal("the store still has the overview after X")
	}
}
