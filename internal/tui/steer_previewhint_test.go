package tui

import (
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/steer"
)

// previewHintModel: two saved rows — a merge preview (row 0) and a commit pair
// (row 1) — with the user somewhere ELSE, so a reveal has something to move.
func previewHintModel(t *testing.T) Model {
	t.Helper()
	m, _ := savedPairModel(t)
	m = m.activateTab(panelBranches)
	m.sel[panelPreviews] = 0
	return m
}

func pairCmd(m Model, hintID string) steer.Command {
	p := m.previews[1].pair
	return steer.Command{Cmd: "navigate", Target: &steer.Target{State: "pair", A: p.A, B: p.B}, HintKind: "preview", HintID: hintID}
}

// TestPreviewHintRevealsTheRowByID: the landing of a ?preview=<id> link
// leaves the Previews tab showing THAT row, so closing the landed view
// returns the user to the entry the link was copied from.
func TestPreviewHintRevealsTheRowByID(t *testing.T) {
	t.Parallel()
	m := previewHintModel(t)
	nm, _ := m.navigateLanded(pairCmd(m, m.previews[1].id()), "opened")
	if nm.activeLeftTab != panelPreviews {
		t.Fatalf("active left tab = %v, want the Previews tab", nm.activeLeftTab)
	}
	if r, ok := nm.selectedPreview(); !ok || r.id() != m.previews[1].id() {
		t.Fatalf("selected = %+v, want the pair row", r)
	}
	if nm.statusMsg != "" {
		t.Errorf("a found entry needs no notice, got %q", nm.statusMsg)
	}
}

// TestPreviewHintIDWinsOverTheSet makes the two lookups DISAGREE on one
// fixture: the id names the merge row while the address is the pair's. The
// ruling is "by id, else by matching set" — the id answers first.
func TestPreviewHintIDWinsOverTheSet(t *testing.T) {
	t.Parallel()
	m := previewHintModel(t)
	m.sel[panelPreviews] = 1
	nm, _ := m.navigateLanded(pairCmd(m, m.previews[0].id()), "opened")
	if r, ok := nm.selectedPreview(); !ok || r.id() != m.previews[0].id() {
		t.Fatalf("selected = %+v, want the row the ID names (the merge preview)", r)
	}
}

// TestPreviewHintFallsBackToTheMatchingSet: another machine spells the repo
// differently, so its id is foreign here — the entry holding the same two
// commits is the one to reveal.
func TestPreviewHintFallsBackToTheMatchingSet(t *testing.T) {
	t.Parallel()
	m := previewHintModel(t)
	nm, _ := m.navigateLanded(pairCmd(m, "f0re1gn0"), "opened")
	if r, ok := nm.selectedPreview(); !ok || r.id() != m.previews[1].id() || nm.activeLeftTab != panelPreviews {
		t.Fatalf("selected = %+v tab=%v, want the pair row revealed by its set", r, nm.activeLeftTab)
	}
	// …and the same for a merge preview, matched on its two NAMES.
	c := steer.Command{Cmd: "navigate", File: "a.txt", Target: &steer.Target{State: "preview", Source: "feat/x", Target: "main"}, HintKind: "preview", HintID: "f0re1gn0"}
	m.sel[panelPreviews] = 1
	nm, _ = m.navigateLanded(c, "opened")
	if r, ok := nm.selectedPreview(); !ok || r.id() != m.previews[0].id() {
		t.Fatalf("selected = %+v, want the merge row revealed by source+target", r)
	}
}

// TestPreviewHintMissNoticesAndMovesNothing: the hint degrades, it never
// fails — the link already landed; the user is only told the entry is not
// saved here, and their tab and cursor stay put.
func TestPreviewHintMissNoticesAndMovesNothing(t *testing.T) {
	t.Parallel()
	m := previewHintModel(t)
	c := steer.Command{Cmd: "navigate", Target: &steer.Target{State: "pair", A: strings.Repeat("a", 40), B: strings.Repeat("b", 40)}, HintKind: "preview", HintID: "f0re1gn0"}
	nm, _ := m.navigateLanded(c, "opened")
	if nm.activeLeftTab != panelBranches || nm.sel[panelPreviews] != 0 {
		t.Errorf("a miss moved the view: tab=%v sel=%d", nm.activeLeftTab, nm.sel[panelPreviews])
	}
	if !strings.Contains(nm.statusMsg, "f0re1gn0") || !strings.Contains(nm.statusMsg, "not saved here") {
		t.Errorf("notice = %q, want it to name the id and say it is not saved here", nm.statusMsg)
	}
}

// TestPreviewHintRevealKeepsAnOpenViewsFocus: a pair link lands in the files
// view; the reveal works UNDER it (tab + cursor) and must not pull the
// keyboard away from the view that just opened.
func TestPreviewHintRevealKeepsAnOpenViewsFocus(t *testing.T) {
	t.Parallel()
	m := previewHintModel(t)
	m.focus = panelCommits
	m.filesView = &contentPopup{}
	m.filesReturnFocus = panelBranches
	nm, _ := m.navigateLanded(pairCmd(m, m.previews[1].id()), "opened")
	if nm.focus != panelCommits {
		t.Errorf("focus = %v, want it left where the landing put it", nm.focus)
	}
	if nm.activeLeftTab != panelPreviews {
		t.Errorf("active left tab = %v, want Previews underneath the open view", nm.activeLeftTab)
	}
	// Closing the view must hand the keyboard to the revealed row, never to
	// the tab the user came from — that tab is no longer the one shown.
	if nm.filesReturnFocus != panelPreviews {
		t.Errorf("filesReturnFocus = %v, want the Previews panel", nm.filesReturnFocus)
	}
}
