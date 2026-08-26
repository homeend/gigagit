package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/model"
)

// The palette entry opens the browser in its loading state, popping the
// palette first (the list-popup convention).
func TestPaletteOpensRemoteHeadsBrowser(t *testing.T) {
	t.Parallel()
	m := loadedModel(t)
	m, cmd := palettePick(t, m, "Browse remote branches")
	p := layerOf[*remoteHeadsPopup](m)
	if p == nil || !p.loading {
		t.Fatalf("popup = %+v, want open and loading", p)
	}
	if layerOf[*commandPalette](m) != nil {
		t.Fatal("the palette should be popped before the browser opens")
	}
	if cmd == nil {
		t.Fatal("opening must fire the remote-names load")
	}
}

// The Branches-panel menu offers the row; other panels don't.
func TestBrowseRemoteBranchesRowGating(t *testing.T) {
	t.Parallel()
	m := loadedModel(t)
	m.focus = panelBranches
	if _, ok := m.browseRemoteBranchesRow(); !ok {
		t.Fatal("row should be offered on the Branches panel")
	}
	m.focus = panelFiles
	if _, ok := m.browseRemoteBranchesRow(); ok {
		t.Fatal("row must not be offered off the Branches panel")
	}
}

// No remotes configured → the popup closes with a status message.
func TestRemoteHeadsNoRemotesCloses(t *testing.T) {
	t.Parallel()
	m := loadedModel(t)
	m, cmd := m.openRemoteHeadsBrowser()
	m, _ = send(m, cmd()) // real RemoteNames read: the fixture has no remote
	if layerOf[*remoteHeadsPopup](m) != nil {
		t.Fatal("popup should close when there is no remote")
	}
	if m.statusMsg == "" {
		t.Fatal("closing should explain itself in the status line")
	}
}

// One remote skips the chooser and goes straight to the ls-remote phase; the
// heads message then fills the filterable list, and enter opens the
// checkout menu.
func TestRemoteHeadsSingleRemoteFlow(t *testing.T) {
	t.Parallel()
	m := loadedModel(t)
	m, _ = m.openRemoteHeadsBrowser()
	m, cmd := send(m, remoteHeadNamesMsg{names: []string{"origin"}, gen: m.loadGen})
	p := layerOf[*remoteHeadsPopup](m)
	if p == nil || p.remote != "origin" || !p.loading {
		t.Fatalf("popup = %+v, want remote=origin still loading", p)
	}
	if cmd == nil {
		t.Fatal("a single remote must fire the heads load directly")
	}
	heads := []model.RemoteHead{{Name: "hidden/a", Hash: "a"}, {Name: "team/b", Hash: "b"}}
	m, _ = send(m, remoteHeadsMsg{remote: "origin", heads: heads, gen: m.loadGen})
	if p.loading || len(p.visible) != 2 {
		t.Fatalf("after heads: loading=%v visible=%v", p.loading, p.visible)
	}
	m, _ = send(m, keyMsg("/"))
	m = typeRunes(t, m, "team")
	if len(p.visible) != 1 || p.heads[p.visible[0]].Name != "team/b" {
		t.Fatalf("filter: visible=%v", p.visible)
	}
	m, _ = send(m, keyType(tea.KeyEnter)) // leave filter mode (keep filter)
	m, _ = send(m, keyType(tea.KeyEnter)) // open the checkout menu
	if m.actionMenu == nil || len(m.actionMenu.rows) != 3 {
		t.Fatalf("actionMenu = %+v, want 3 rows", m.actionMenu)
	}
}

// >1 remote shows the chooser; picking one starts the heads load.
func TestRemoteHeadsRemoteChooser(t *testing.T) {
	t.Parallel()
	m := loadedModel(t)
	m, _ = m.openRemoteHeadsBrowser()
	m, _ = send(m, remoteHeadNamesMsg{names: []string{"origin", "fork"}, gen: m.loadGen})
	p := layerOf[*remoteHeadsPopup](m)
	if p == nil || p.loading || p.remote != "" || len(p.visible) != 2 {
		t.Fatalf("chooser: %+v", p)
	}
	m, _ = send(m, keyType(tea.KeyDown))
	m, cmd := send(m, keyType(tea.KeyEnter))
	if p.remote != "fork" || !p.loading || cmd == nil {
		t.Fatalf("pick: remote=%q loading=%v cmd=%v", p.remote, p.loading, cmd)
	}
}

// A result from before a repo switch (stale gen) is dropped.
func TestRemoteHeadsStaleGenDropped(t *testing.T) {
	t.Parallel()
	m := loadedModel(t)
	m, _ = m.openRemoteHeadsBrowser()
	m, _ = send(m, remoteHeadNamesMsg{names: []string{"origin"}, gen: m.loadGen - 1})
	p := layerOf[*remoteHeadsPopup](m)
	if p == nil || !p.loading || p.remote != "" {
		t.Fatalf("stale msg must be dropped: %+v", p)
	}
}

// The checkout rows pop the browser and start the op; Cancel keeps browsing.
func TestRemoteHeadsCheckoutRows(t *testing.T) {
	t.Parallel()
	m := loadedModel(t)
	m, _ = m.openRemoteHeadsBrowser()
	rows := m.remoteHeadActionRows("origin", "hidden/a")
	if len(rows) != 3 {
		t.Fatalf("rows = %d, want 3", len(rows))
	}
	// Cancel first: popLayer nils the shared backing-array slot, so a copy
	// held across a checkout row's pop would go stale.
	um, _ := rows[2].run(m)
	m3 := um.(Model)
	if layerOf[*remoteHeadsPopup](m3) == nil {
		t.Fatal("Cancel must keep the browser open")
	}
	if m3.running {
		t.Fatal("Cancel must start nothing")
	}

	um, cmd := rows[0].run(m)
	m2 := um.(Model)
	if layerOf[*remoteHeadsPopup](m2) != nil {
		t.Fatal("checkout must pop the browser")
	}
	if !m2.running {
		t.Fatal("checkout must start the op")
	}
	driveOp(t, m2, cmd) // no such remote in the fixture: finishes with an error
}
