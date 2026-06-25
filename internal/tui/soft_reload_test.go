package tui

import "testing"

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
