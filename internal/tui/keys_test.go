package tui

import "testing"

func TestPullKeyStartsOperation(t *testing.T) {
	m := loadedModel(t)
	updated, cmd := m.Update(keyMsg("p"))
	mm := updated.(Model)
	if !mm.running {
		t.Fatal("pressing p should start an operation (running=true)")
	}
	if cmd == nil {
		t.Fatal("expected a waitForOp command")
	}
	driveOp(t, mm, cmd) // drain so the goroutine doesn't leak
}

func TestKeysIgnoredWhileRunning(t *testing.T) {
	m := loadedModel(t)
	m.running = true // pretend an op is in flight
	updated, _ := m.Update(keyMsg("u"))
	mm := updated.(Model)
	if mm.opMsgs != nil {
		t.Fatal("operation keys must be ignored while another op is running")
	}
}

func TestZCyclesFocusedPanelMode(t *testing.T) {
	m := New(nil)
	m.focus = panelCommits
	if m.dispModes[panelCommits] != modeCutoff {
		t.Fatalf("default mode = %v, want modeCutoff", m.dispModes[panelCommits])
	}
	u, _ := m.Update(keyMsg("z"))
	if got := u.(Model).dispModes[panelCommits]; got != modeWrap {
		t.Errorf("after z, mode = %v, want modeWrap", got)
	}
	// w still opens the worktree popup path, not a mode cycle.
	if got := u.(Model).dispModes[panelCommits]; got == modeCutoff {
		t.Errorf("z did not change the mode")
	}
}

func TestZIsFilterTextWhileTyping(t *testing.T) {
	m := New(nil)
	m.focus = panelBranches
	m.filterTyping = true
	m.filterPanel = panelBranches
	u, _ := m.Update(keyMsg("z"))
	mm := u.(Model)
	if mm.filterQuery != "z" {
		t.Errorf("filterQuery = %q, want \"z\" (z is query text while filtering)", mm.filterQuery)
	}
	if mm.dispModes[panelBranches] != modeCutoff {
		t.Error("z must not cycle the display mode while filter-typing")
	}
}
