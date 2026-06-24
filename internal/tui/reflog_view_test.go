package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/git"
	"github.com/homeend/gigagit/internal/gitexec"
	"github.com/homeend/gigagit/internal/model"
)

func reflogTestModel() Model {
	return Model{
		svc:       domain.New(&git.Repo{Runner: gitexec.NewFakeRunner()}),
		sel:       map[panel]int{},
		width:     120,
		height:    40,
		sortModes: map[panel]sortMode{},
		reflog: []model.ReflogEntry{
			{Selector: "HEAD@{0}", Hash: "1111111111111111111111111111111111111111", ShortHash: "1111111", Subject: "checkout: moving from main to feature", Rel: "1 minute ago"},
			{Selector: "HEAD@{1}", Hash: "2222222222222222222222222222222222222222", ShortHash: "2222222", Subject: "commit: second", Rel: "2 minutes ago"},
		},
	}
}

func TestReflogListLenAndRows(t *testing.T) {
	m := reflogTestModel()
	if m.panelLen(panelReflog) != 2 {
		t.Fatalf("panelLen(reflog) = %d, want 2", m.panelLen(panelReflog))
	}
	rows := m.reflogRows()
	if len(rows) != 2 {
		t.Fatalf("reflogRows = %d, want 2", len(rows))
	}
	if !contains(rows[0], "1111111") || !contains(rows[0], "checkout") {
		t.Fatalf("row 0 = %q, want short hash + subject", rows[0])
	}
}

func TestReflogTabRendersInAssembledLeftColumn(t *testing.T) {
	// Render-path coverage: the panel-logic tests run on synthetic data and
	// never prove the Reflog tab draws in the assembled left column. Toggle the
	// bottom slot to Reflog and assert a reflog row + the bracketed label survive
	// the full render.
	m := maxModel() // width 120, height 40 — bodyH >= 12 so the bottom slot shows
	m.reflog = reflogTestModel().reflog
	m.activeBottomTab = panelReflog

	out := ansi.Strip(m.renderInterface())
	if !strings.Contains(out, "[Reflog") {
		t.Fatalf("assembled render must show the active [Reflog] tab label:\n%s", out)
	}
	if !strings.Contains(out, "checkout: moving") {
		t.Fatalf("assembled render must show a reflog row subject:\n%s", out)
	}
	if strings.Contains(out, "[Staged") {
		t.Fatalf("Staged must not be the active bracketed tab when Reflog is active:\n%s", out)
	}
}

func TestReflogResetRowAnchorsOnCursor(t *testing.T) {
	m := reflogTestModel()
	m.focus = panelReflog
	m.sel[panelReflog] = 1 // second entry
	r, ok := m.reflogResetRow()
	if !ok {
		t.Fatal("reflog . menu must offer Reset to this entry")
	}
	if r.id != "reflog-reset" {
		t.Fatalf("row id = %q, want reflog-reset", r.id)
	}
	// Not offered off the reflog panel.
	m.focus = panelCommits
	if _, ok := m.reflogResetRow(); ok {
		t.Fatal("reset row must not appear off the reflog panel")
	}
}

func TestReflogCheckoutRowOpensModal(t *testing.T) {
	m := reflogTestModel()
	m.focus = panelReflog
	m.sel[panelReflog] = 1
	r, ok := m.reflogCheckoutRow()
	if !ok || r.id != "reflog-checkout" {
		t.Fatalf("reflog . menu must offer Check out this entry…, got ok=%v id=%q", ok, r.id)
	}
	nm, _ := r.run(m)
	m = nm.(Model)
	if m.modal == nil {
		t.Fatal("Check out must open a decision modal")
	}
	opts := m.modal.req.Options
	if len(opts) == 0 || opts[len(opts)-1] != "Cancel" {
		t.Fatalf("modal must end with Cancel (never-trap), got %v", opts)
	}
}

func TestReflogCheckoutDetachedStartsOp(t *testing.T) {
	m := reflogTestModel()
	m.focus = panelReflog
	m.sel[panelReflog] = 1
	r, _ := m.reflogCheckoutRow()
	nm, _ := r.run(m)
	m = nm.(Model)
	nm, cmd := m.modal.onResolve(m, "Detached")
	m = nm.(Model)
	if cmd == nil {
		t.Fatal("Detached must start the checkout op")
	}
}

func TestReflogCheckoutCreateBranchOpensPopup(t *testing.T) {
	m := reflogTestModel()
	m.focus = panelReflog
	m.sel[panelReflog] = 1
	r, _ := m.reflogCheckoutRow()
	nm, _ := r.run(m)
	m = nm.(Model)
	nm, _ = m.modal.onResolve(m, "Create branch…")
	m = nm.(Model)
	p := layerOf[*reflogCheckoutPopup](m)
	if p == nil {
		t.Fatal("Create branch… must push the reflog checkout popup")
	}
	// Anchored on the cursor entry (sel=1), not entry 0 — guards the
	// display-vs-backing trap (would pass at sel=0 without this).
	if p.ref != "2222222222222222222222222222222222222222" {
		t.Fatalf("popup must carry the cursor entry's hash, got %q", p.ref)
	}
}

func TestReflogEnterOpensCommitFilesView(t *testing.T) {
	m := reflogTestModel()
	m.focus = panelReflog
	m.sel[panelReflog] = 1 // anchor on the SECOND row, not the default 0
	nm, cmd := m.Update(keyMsg("enter"))
	m = nm.(Model)
	if m.filesView == nil {
		t.Fatal("enter on a reflog row must open the files view")
	}
	if m.filesHash != "2222222222222222222222222222222222222222" {
		t.Fatalf("filesHash = %q, want the SECOND entry's hash (cursor-anchored)", m.filesHash)
	}
	if cmd == nil {
		t.Fatal("expected a files-load command")
	}
}

func TestReflogFilesViewOpensTreeFocusedOnCommits(t *testing.T) {
	// The reflog files-view's right side is the Commits feed (not the reflog you
	// came from), so it opens TREE-focused (up/down walks the entry's files, not
	// the feed) and focus is panelCommits (the commit-list side / tooltip anchor),
	// never panelReflog — which would mis-place the reveal tooltip over the tree.
	m := reflogTestModel()
	m.focus = panelReflog
	m.sel[panelReflog] = 1
	nm, _ := m.Update(keyMsg("enter"))
	m = nm.(Model)
	if !m.filesTreeFocused {
		t.Fatal("reflog files-view must open tree-focused so up/down walks the files")
	}
	if m.focus != panelCommits {
		t.Fatalf("focus = %v, want panelCommits (the files-view commit-list side)", m.focus)
	}
}

func TestReflogMenuCopyAndBookmark(t *testing.T) {
	m := reflogTestModel()
	m.focus = panelReflog
	m.sel[panelReflog] = 0
	rows := m.contextCopyRows()
	var sawCopy bool
	for _, r := range rows {
		if r.id == "copy-reflog-sha" {
			sawCopy = true
			if r.copyText != "1111111111111111111111111111111111111111" {
				t.Fatalf("copy text = %q, want the cursor row's full hash", r.copyText)
			}
		}
	}
	if !sawCopy {
		t.Fatal("reflog . menu must offer Copy SHA")
	}
	if _, ok := m.reflogBookmarkRow(); !ok {
		t.Fatal("reflog . menu must offer Bookmark this commit")
	}
}

func TestReflogMenuNoCommitLeak(t *testing.T) {
	m := reflogTestModel()
	m.focus = panelReflog
	m.sel[panelReflog] = 0
	// The commit-panel bookmark row is anchored on panelCommits and must NOT
	// fire while focus is on the reflog panel.
	if _, ok := m.commitBookmarkRow(); ok {
		t.Fatal("commit bookmark row leaked into the reflog panel")
	}
}

func TestBottomTabTogglesStagedReflog(t *testing.T) {
	m := reflogTestModel()
	m.focus = panelStaged
	nm, _ := m.Update(keyMsg("ctrl+right"))
	m = nm.(Model)
	if m.focus != panelReflog || m.bottomTab() != panelReflog {
		t.Fatalf("after ctrl+right: focus=%v bottomTab=%v, want panelReflog", m.focus, m.bottomTab())
	}
	nm, _ = m.Update(keyMsg("ctrl+left"))
	m = nm.(Model)
	if m.focus != panelStaged || m.bottomTab() != panelStaged {
		t.Fatalf("after ctrl+left: focus=%v bottomTab=%v, want panelStaged", m.focus, m.bottomTab())
	}
}
