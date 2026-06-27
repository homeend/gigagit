package tui

import (
	"strings"
	"testing"
)

// r is inert while a source read is already in flight: it dispatches no new
// load and does not disturb the running reload's flags. This blocks the
// double-r restart and the stale-flash when r is pressed during a load.
func TestRBlockedWhileLoading(t *testing.T) {
	m := loadedModel(t)
	m.loading = true
	m.srcInflight[srcStatus] = true // anySourceInflight() must return true
	gen := m.loadGen
	updated, cmd := m.Update(keyMsg("r"))
	mm := updated.(Model)
	if mm.loadGen != gen {
		t.Fatalf("r should dispatch no new load while one is in flight: loadGen %d→%d", gen, mm.loadGen)
	}
	if cmd != nil {
		t.Fatal("r should return no command while a load is in flight")
	}
	if !mm.loading {
		t.Fatal("r must not disturb the in-flight reload's loading flag")
	}
}

// Pressing r DURING an in-flight repo switch (reRoot: loading=true, ready=false)
// must not fire a new load — m.loading must gate it.
func TestRDuringRepoSwitchStaysHard(t *testing.T) {
	m := loadedModel(t)
	switched, _ := m.reRoot(m.currentWorktree)
	m = switched.(Model) // loading=true (reRoot sets it), ready=false
	gen := m.loadGen
	// reRoot sets m.loading=true; the new !m.loading guard in the r handler
	// must block r without any artificial srcInflight injection.
	if !m.loading {
		t.Fatal("reRoot must set m.loading=true (test precondition)")
	}
	updated, cmd := m.Update(keyMsg("r"))
	mm := updated.(Model)
	if mm.ready {
		t.Fatal("r during an in-flight repo switch must not set ready")
	}
	if mm.loadGen != gen {
		t.Fatalf("r during a switch should dispatch no new load: loadGen %d→%d", gen, mm.loadGen)
	}
	if cmd != nil {
		t.Fatal("r during repo switch should return no command")
	}
}

// Pressing r on a loaded model triggers a reload: the legacy m.loading flag
// is set (action guards depend on it). Per-source srcLoading entries carry
// the spinner role previously held by softReload.
func TestRKeyStartsSoftReload(t *testing.T) {
	m := loadedModel(t)
	updated, _ := m.Update(keyMsg("r"))
	mm := updated.(Model)
	if !mm.loading {
		t.Fatal("r should set loading")
	}
}

// When the load completes, dataLoadedMsg sets ready=true and clears loading.
func TestDataLoadedSetsReady(t *testing.T) {
	m := loadedModel(t)
	m.loading = true
	m.ready = false
	updated, _ := m.Update(dataLoadedMsg{gen: m.loadGen})
	mm := updated.(Model)
	if mm.loading {
		t.Fatal("dataLoadedMsg should clear loading")
	}
	if !mm.ready {
		t.Fatal("dataLoadedMsg should set ready=true")
	}
}

// A superseded-generation dataLoadedMsg must NOT set ready: a newer load is
// still in flight (loadGen=5) and must keep blank until it completes.
func TestSupersededLoadDoesNotSetReady(t *testing.T) {
	m := loadedModel(t)
	m.loadGen = 5
	m.loading = true
	m.ready = false
	updated, _ := m.Update(dataLoadedMsg{gen: 4}) // older generation, dropped
	mm := updated.(Model)
	if mm.ready {
		t.Fatal("a superseded dataLoadedMsg must not set ready")
	}
	if !mm.loading {
		t.Fatal("a superseded dataLoadedMsg must leave loading set")
	}
}

// reRoot (repo switch) sets ready=false even if the previous repo had ready,
// so the new repo blanks the screen until its own first data arrives.
func TestReRootResetsReady(t *testing.T) {
	m := loadedModel(t)
	m.ready = true
	updated, _ := m.reRoot(m.currentWorktree)
	if updated.(Model).ready {
		t.Fatal("reRoot must reset ready to false")
	}
}

// During a reload with ready=true the panels stay on screen (no "gigagit (loading…)").
func TestViewKeepsPanelsDuringReload(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 100, 30
	m.focus = panelCommits // keep every panel label visible (see TestViewRendersPanelsWithoutPanic)
	m.loading = true
	m.ready = true // data already arrived; keep panels visible
	out := m.View()
	if strings.Contains(out, "gigagit (loading…)") {
		t.Fatalf("reload with ready=true should not blank the screen:\n%s", out)
	}
	if !strings.Contains(out, "Branches") || !strings.Contains(out, "Commits") {
		t.Fatalf("reload with ready=true should keep panels visible:\n%s", out)
	}
}

// A hard reload (loading without ready — startup / reRoot) still blanks.
func TestViewBlanksForHardReload(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 100, 30
	m.loading = true
	m.ready = false
	out := m.View()
	if !strings.Contains(out, "gigagit (loading…)") {
		t.Fatalf("hard reload should blank to the loading screen, got:\n%s", out)
	}
}

// reRoot (repo switch) sets loading=true AND ready=false, so its View blanks.
func TestReRootIsHardReload(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 100, 30
	updated, _ := m.reRoot(m.currentWorktree)
	mm := updated.(Model)
	if mm.ready {
		t.Fatal("reRoot must not set ready (it resets it to false)")
	}
	if !strings.Contains(mm.View(), "gigagit (loading…)") {
		t.Fatal("reRoot should keep the blank loading screen")
	}
}

// Every panel whose source is mid manual-refresh carries the ⏳ glyph, and
// the status line shows "reloading…".
func TestSourceLoadingShowsGlyphAndStatus(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 100, 30
	m.focus = panelCommits
	m.loading = true
	m.ready = true // don't blank; we want to see the rendered panels
	// Mark all sources as manually loading so every panel shows the glyph.
	for s := sourceKey(0); s < srcCount; s++ {
		m.srcLoading[s] = true
	}
	out := m.View()
	if !strings.Contains(out, commitsLoadingGlyph) {
		t.Fatalf("source reload should show the %q glyph:\n%s", commitsLoadingGlyph, out)
	}
	if !strings.Contains(out, "reloading…") {
		t.Fatalf("source reload should show a reloading status line:\n%s", out)
	}
}

// Direct panelLabel test: the per-panel glyph is proven in ISOLATION from the
// status line (which also emits ⏳), so a broken panelLabel edit can't pass on
// the status line alone.
func TestPanelLabelShowsGlyphWhenSourceLoading(t *testing.T) {
	m := loadedModel(t)
	m.srcLoading[srcBranches] = true
	got := m.panelLabel(panelBranches, "Branches")
	if !strings.Contains(got, commitsLoadingGlyph) {
		t.Fatalf("Branches label should carry the glyph when srcBranches is loading: %q", got)
	}
}

// Without any source loading the Branches title carries no glyph (no false positive).
func TestNoGlyphWhenNotReloading(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 100, 30
	m.focus = panelCommits
	got := m.panelLabel(panelBranches, "Branches")
	if strings.Contains(got, commitsLoadingGlyph) {
		t.Fatalf("Branches label should have no glyph when idle: %q", got)
	}
}
