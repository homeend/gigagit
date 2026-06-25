package tui

import (
	"strings"
	"testing"
)

// r is inert while a load is already in flight: it dispatches no new load
// (loadGen unchanged) and does not disturb the running reload's flags. This
// blocks the double-r restart and the stale-flash when r is pressed during a
// load.
func TestRBlockedWhileLoading(t *testing.T) {
	m := loadedModel(t)
	m.loading = true
	m.softReload = true // a soft reload is already running
	gen := m.loadGen
	updated, cmd := m.Update(keyMsg("r"))
	mm := updated.(Model)
	if mm.loadGen != gen {
		t.Fatalf("r should dispatch no new load while one is in flight: loadGen %d→%d", gen, mm.loadGen)
	}
	if cmd != nil {
		t.Fatal("r should return no command while a load is in flight")
	}
	if !mm.softReload || !mm.loading {
		t.Fatal("r must not disturb the in-flight reload's flags")
	}
}

// Pressing r DURING an in-flight repo switch (reRoot: loading=true,
// softReload=false) must not turn on softReload — otherwise View would
// soft-render the outgoing repo's stale panels for the rest of the switch.
func TestRDuringRepoSwitchStaysHard(t *testing.T) {
	m := loadedModel(t)
	switched, _ := m.reRoot(m.currentWorktree)
	m = switched.(Model) // loading=true, softReload=false
	gen := m.loadGen
	updated, _ := m.Update(keyMsg("r"))
	mm := updated.(Model)
	if mm.softReload {
		t.Fatal("r during an in-flight repo switch must not enable softReload")
	}
	if mm.loadGen != gen {
		t.Fatalf("r during a switch should dispatch no new load: loadGen %d→%d", gen, mm.loadGen)
	}
}

// Pressing r on a loaded model starts a soft reload: loading + softReload set.
func TestRKeyStartsSoftReload(t *testing.T) {
	m := loadedModel(t)
	updated, _ := m.Update(keyMsg("r"))
	mm := updated.(Model)
	if !mm.loading {
		t.Fatal("r should set loading")
	}
	if !mm.softReload {
		t.Fatal("r should set softReload")
	}
}

// When the load completes, dataLoadedMsg clears softReload (and loading).
func TestDataLoadedClearsSoftReload(t *testing.T) {
	m := loadedModel(t)
	m.loading = true
	m.softReload = true
	updated, _ := m.Update(dataLoadedMsg{gen: m.loadGen})
	mm := updated.(Model)
	if mm.loading {
		t.Fatal("dataLoadedMsg should clear loading")
	}
	if mm.softReload {
		t.Fatal("dataLoadedMsg should clear softReload")
	}
}

// A superseded-generation dataLoadedMsg must NOT clear softReload: a newer r
// reload is still in flight (loadGen=5) and must keep soft-rendering until it
// completes. Clearing here would blank the screen mid double-reload.
func TestSupersededLoadKeepsSoftReload(t *testing.T) {
	m := loadedModel(t)
	m.loadGen = 5
	m.loading = true
	m.softReload = true
	updated, _ := m.Update(dataLoadedMsg{gen: 4}) // older generation, dropped
	mm := updated.(Model)
	if !mm.softReload {
		t.Fatal("a superseded dataLoadedMsg must leave softReload set for the newer in-flight load")
	}
	if !mm.loading {
		t.Fatal("a superseded dataLoadedMsg must leave loading set")
	}
}

// reRoot (repo switch) clears softReload even if a soft reload was in flight, so
// the outgoing repo's panels stop soft-rendering immediately.
func TestReRootClearsInFlightSoftReload(t *testing.T) {
	m := loadedModel(t)
	m.softReload = true
	updated, _ := m.reRoot(m.currentWorktree)
	if updated.(Model).softReload {
		t.Fatal("reRoot must clear softReload")
	}
}

// During a soft reload the panels stay on screen (no "gigagit (loading…)").
func TestViewSoftRendersDuringReload(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 100, 30
	m.focus = panelCommits // keep every panel label visible (see TestViewRendersPanelsWithoutPanic)
	m.loading = true
	m.softReload = true
	out := m.View()
	if strings.Contains(out, "gigagit (loading…)") {
		t.Fatalf("soft reload should not blank the screen:\n%s", out)
	}
	if !strings.Contains(out, "Branches") || !strings.Contains(out, "Commits") {
		t.Fatalf("soft reload should keep panels visible:\n%s", out)
	}
}

// A hard reload (loading without softReload — startup / reRoot) still blanks.
func TestViewBlanksForHardReload(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 100, 30
	m.loading = true
	m.softReload = false
	out := m.View()
	if !strings.Contains(out, "gigagit (loading…)") {
		t.Fatalf("hard reload should blank to the loading screen, got:\n%s", out)
	}
}

// reRoot (repo switch) sets loading WITHOUT softReload, so its View blanks.
func TestReRootIsHardReload(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 100, 30
	updated, _ := m.reRoot(m.currentWorktree)
	mm := updated.(Model)
	if mm.softReload {
		t.Fatal("reRoot must not set softReload")
	}
	if !strings.Contains(mm.View(), "gigagit (loading…)") {
		t.Fatal("reRoot should keep the blank loading screen")
	}
}

// Every panel title carries the ⏳ glyph during a soft reload, and the status
// line shows "reloading…".
func TestSoftReloadShowsGlyphAndStatus(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 100, 30
	m.focus = panelCommits
	m.loading = true
	m.softReload = true
	out := m.View()
	if !strings.Contains(out, commitsLoadingGlyph) {
		t.Fatalf("soft reload should show the %q glyph:\n%s", commitsLoadingGlyph, out)
	}
	if !strings.Contains(out, "reloading…") {
		t.Fatalf("soft reload should show a reloading status line:\n%s", out)
	}
}

// Direct panelLabel test: the per-panel glyph is proven in ISOLATION from the
// status line (which also emits ⏳), so a broken panelLabel edit can't pass on
// the status line alone.
func TestPanelLabelShowsGlyphDuringSoftReload(t *testing.T) {
	m := loadedModel(t)
	m.softReload = true
	got := m.panelLabel(panelBranches, "Branches")
	if !strings.Contains(got, commitsLoadingGlyph) {
		t.Fatalf("Branches label should carry the glyph during soft reload: %q", got)
	}
}

// Without a soft reload the Branches title carries no glyph (no false positive).
func TestNoGlyphWhenNotReloading(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 100, 30
	m.focus = panelCommits
	got := m.panelLabel(panelBranches, "Branches")
	if strings.Contains(got, commitsLoadingGlyph) {
		t.Fatalf("Branches label should have no glyph when idle: %q", got)
	}
}
